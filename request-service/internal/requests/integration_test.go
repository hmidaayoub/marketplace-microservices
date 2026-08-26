package requests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/auth"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/clients"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/db"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/events"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/middleware"
)

const (
	testSecret         = "dev-secret-key-change-in-production-min-256-bits"
	testInternalAPIKey = "test-internal-api-key"
)

var testPool *pgxpool.Pool

// TestMain runs the suite against a real PostgreSQL with the real migrations applied,
// for the same reason the Java modules do: an in-memory stand-in would not execute
// gen_random_uuid(), the CHECK constraints or the unique index that the code relies on.
//
// DATABASE_URL short-circuits the container so the suite can also run against a
// Postgres that is already up.
func TestMain(m *testing.M) {
	ctx := context.Background()

	databaseURL := os.Getenv("DATABASE_URL")
	var terminate func()

	if databaseURL == "" {
		container, err := tcpostgres.Run(ctx, "postgres:15-alpine",
			tcpostgres.WithDatabase("request_db"),
			tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("postgres"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).WithStartupTimeout(60*time.Second)),
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "starting postgres: %v\n", err)
			os.Exit(1)
		}
		terminate = func() { _ = testcontainers.TerminateContainer(container) }

		databaseURL, err = container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			terminate()
			fmt.Fprintf(os.Stderr, "connection string: %v\n", err)
			os.Exit(1)
		}
	}

	if err := db.Migrate(databaseURL); err != nil {
		fmt.Fprintf(os.Stderr, "migrating: %v\n", err)
		if terminate != nil {
			terminate()
		}
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connecting: %v\n", err)
		if terminate != nil {
			terminate()
		}
		os.Exit(1)
	}
	testPool = pool

	code := m.Run()

	pool.Close()
	if terminate != nil {
		terminate()
	}
	os.Exit(code)
}

// --- harness ---------------------------------------------------------------------

// recordingNotifier stands in for the broker. The tests assert on what would have been
// published rather than starting a RabbitMQ, which keeps the suite fast; the AMQP path
// itself is exercised end-to-end against a real broker in the running stack.
type recordingNotifier struct {
	mu        sync.Mutex
	published []publishedEvent
}

type publishedEvent struct {
	routingKey    string
	notifications []events.Notification
}

func (n *recordingNotifier) PublishOrLog(_ context.Context, routingKey string, ns ...events.Notification) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.published = append(n.published, publishedEvent{routingKey: routingKey, notifications: ns})
}

func (n *recordingNotifier) events() []publishedEvent {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]publishedEvent(nil), n.published...)
}

type harness struct {
	t      *testing.T
	router http.Handler

	notifier *recordingNotifier

	// userID -> customerID, as customer-service would resolve it.
	profiles map[uuid.UUID]uuid.UUID

	// records whether the stub was called with the right internal key
	sawAPIKey string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	if _, err := testPool.Exec(context.Background(), "TRUNCATE purchase_request CASCADE"); err != nil {
		t.Fatalf("cleaning tables: %v", err)
	}

	h := &harness{t: t, profiles: map[uuid.UUID]uuid.UUID{}, notifier: &recordingNotifier{}}

	// Stands in for customer-service's /internal/customers/by-user/{userId}.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.sawAPIKey = r.Header.Get(middleware.InternalAPIKeyHeader)

		raw := strings.TrimPrefix(r.URL.Path, "/internal/customers/by-user/")
		userID, err := uuid.Parse(raw)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		customerID, ok := h.profiles[userID]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"customerId": customerID.String()})
	}))
	t.Cleanup(stub.Close)

	h.router = NewRouter(RouterConfig{
		Handler: NewHandler(
			NewService(testPool),
			newCustomerClient(stub.URL),
			h.notifier,
		),
		Verifier:       auth.NewVerifier([]byte(testSecret)),
		InternalAPIKey: testInternalAPIKey,
		Ready:          func() error { return nil },
	})

	return h
}

// newCustomer builds a customer with a profile and returns their userId and token.
func (h *harness) newCustomer() (uuid.UUID, string) {
	userID := uuid.New()
	h.profiles[userID] = uuid.New()
	return userID, h.token(userID, auth.RoleCustomer)
}

func (h *harness) token(userID uuid.UUID, role string) string {
	h.t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.MapClaims{
		"sub":   userID.String(),
		"role":  role,
		"email": "user@test.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(testSecret))
	if err != nil {
		h.t.Fatalf("signing test token: %v", err)
	}
	return signed
}

type response struct {
	code int
	body map[string]any
	list []map[string]any
	raw  string
}

func (h *harness) do(method, path, token, body string) response {
	h.t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)

	out := response{code: rec.Code, raw: rec.Body.String()}
	trimmed := strings.TrimSpace(out.raw)
	if strings.HasPrefix(trimmed, "{") {
		_ = json.Unmarshal([]byte(trimmed), &out.body)
	} else if strings.HasPrefix(trimmed, "[") {
		_ = json.Unmarshal([]byte(trimmed), &out.list)
	}
	return out
}

func (h *harness) doInternal(method, path, apiKey string) response {
	h.t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(""))
	if apiKey != "" {
		req.Header.Set(middleware.InternalAPIKeyHeader, apiKey)
	}

	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)

	out := response{code: rec.Code, raw: rec.Body.String()}
	_ = json.Unmarshal([]byte(strings.TrimSpace(out.raw)), &out.body)
	return out
}

func (h *harness) createRequest(token, itemName string, quantity int) string {
	h.t.Helper()
	res := h.do(http.MethodPost, "/api/requests", token,
		fmt.Sprintf(`{"itemName":%q,"description":"d","category":"c","quantity":%d}`, itemName, quantity))
	if res.code != http.StatusCreated {
		h.t.Fatalf("creating request: status %d body %s", res.code, res.raw)
	}
	return res.body["requestId"].(string)
}

func num(t *testing.T, body map[string]any, field string) int {
	t.Helper()
	v, ok := body[field].(float64)
	if !ok {
		t.Fatalf("field %q missing or not a number in %v", field, body)
	}
	return int(v)
}

func newCustomerClient(baseURL string) customerResolver {
	return clients.NewCustomer(baseURL, testInternalAPIKey, &http.Client{Timeout: 5 * time.Second})
}
