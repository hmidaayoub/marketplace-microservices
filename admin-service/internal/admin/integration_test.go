package admin

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

	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/auth"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/clients"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/db"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/events"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/middleware"
)

const (
	testSecret         = "dev-secret-key-change-in-production-min-256-bits"
	testInternalAPIKey = "test-internal-api-key"
)

var testPool *pgxpool.Pool

// TestMain runs the suite against a real PostgreSQL with the real migrations applied,
// for the same reason the other modules do: an in-memory stand-in would not execute
// gen_random_uuid(), the CHECK constraints, or the unique indexes that make a double
// decision and a duplicate grant impossible.
//
// DATABASE_URL short-circuits the container so the suite can also run against a
// Postgres that is already up.
func TestMain(m *testing.M) {
	ctx := context.Background()

	databaseURL := os.Getenv("DATABASE_URL")
	var terminate func()

	if databaseURL == "" {
		container, err := tcpostgres.Run(ctx, "postgres:15-alpine",
			tcpostgres.WithDatabase("admin_contact_db"),
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

// platform stands in for the five services Admin/Contact calls. One stub serves them
// all because their internal paths do not overlap, and routing by prefix here keeps
// the test state in a single place that assertions can read back.
type platform struct {
	mu sync.Mutex

	offers      map[uuid.UUID]*clients.Offer
	participant map[uuid.UUID][]uuid.UUID // requestID -> customerIDs
	sellerOf    map[uuid.UUID]uuid.UUID   // userID -> sellerID
	userOf      map[uuid.UUID]uuid.UUID   // customerID -> userID
	phones      map[uuid.UUID]string      // userID -> phone

	// failStatusPatch makes offer-service reject the decision relay, which is how the
	// rollback behaviour is exercised.
	failStatusPatch bool

	// keys records the internal key each dependency was called with.
	keys map[string]string

	// phoneCalls counts how many times a phone number was actually fetched, so a test
	// can assert that an unauthorised seller never reaches auth-service at all.
	phoneCalls int
}

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

func (n *recordingNotifier) keys() []string {
	out := []string{}
	for _, e := range n.events() {
		out = append(out, e.routingKey)
	}
	return out
}

type harness struct {
	t        *testing.T
	router   http.Handler
	p        *platform
	notifier *recordingNotifier
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	for _, table := range []string{"contact_access", "offer_decision"} {
		if _, err := testPool.Exec(context.Background(), "TRUNCATE "+table+" CASCADE"); err != nil {
			t.Fatalf("cleaning %s: %v", table, err)
		}
	}

	p := &platform{
		offers:      map[uuid.UUID]*clients.Offer{},
		participant: map[uuid.UUID][]uuid.UUID{},
		sellerOf:    map[uuid.UUID]uuid.UUID{},
		userOf:      map[uuid.UUID]uuid.UUID{},
		phones:      map[uuid.UUID]string{},
		keys:        map[string]string{},
	}

	stub := httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(stub.Close)

	httpClient := &http.Client{Timeout: 5 * time.Second}
	service := NewService(testPool, Deps{
		Offers:    clients.NewOffer(stub.URL, testInternalAPIKey, httpClient),
		Requests:  clients.NewRequest(stub.URL, testInternalAPIKey, httpClient),
		Sellers:   clients.NewSeller(stub.URL, testInternalAPIKey, httpClient),
		Customers: clients.NewCustomer(stub.URL, testInternalAPIKey, httpClient),
		Auth:      clients.NewAuth(stub.URL, testInternalAPIKey, httpClient),
	})

	notifier := &recordingNotifier{}

	return &harness{
		t:        t,
		p:        p,
		notifier: notifier,
		router: NewRouter(RouterConfig{
			Handler:        NewHandler(service, notifier),
			Verifier:       auth.NewVerifier([]byte(testSecret)),
			InternalAPIKey: testInternalAPIKey,
			Ready:          func() error { return nil },
		}),
	}
}

func (p *platform) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()

	path := r.URL.Path
	writeJSON := func(status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}

	switch {
	case path == "/internal/offers/pending":
		p.keys["offer"] = r.Header.Get(middleware.InternalAPIKeyHeader)
		pending := []clients.Offer{}
		for _, offer := range p.offers {
			if offer.Status == statusPending {
				pending = append(pending, *offer)
			}
		}
		writeJSON(http.StatusOK, pending)

	case strings.HasSuffix(path, "/status") && strings.HasPrefix(path, "/internal/offers/"):
		p.keys["offer"] = r.Header.Get(middleware.InternalAPIKeyHeader)
		if p.failStatusPatch {
			writeJSON(http.StatusServiceUnavailable, nil)
			return
		}
		id, err := uuid.Parse(strings.TrimSuffix(strings.TrimPrefix(path, "/internal/offers/"), "/status"))
		if err != nil {
			writeJSON(http.StatusBadRequest, nil)
			return
		}
		offer, ok := p.offers[id]
		if !ok {
			writeJSON(http.StatusNotFound, nil)
			return
		}
		if offer.Status != statusPending {
			// Matches offer-service: only a PENDING offer can be decided.
			writeJSON(http.StatusConflict, nil)
			return
		}
		var body struct {
			Status string `json:"status"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		offer.Status = body.Status
		writeJSON(http.StatusOK, offer)

	case strings.HasPrefix(path, "/internal/offers/"):
		p.keys["offer"] = r.Header.Get(middleware.InternalAPIKeyHeader)
		id, err := uuid.Parse(strings.TrimPrefix(path, "/internal/offers/"))
		if err != nil {
			writeJSON(http.StatusBadRequest, nil)
			return
		}
		offer, ok := p.offers[id]
		if !ok {
			writeJSON(http.StatusNotFound, nil)
			return
		}
		writeJSON(http.StatusOK, offer)

	case strings.HasPrefix(path, "/internal/requests/") && strings.HasSuffix(path, "/participants"):
		p.keys["request"] = r.Header.Get(middleware.InternalAPIKeyHeader)
		raw := strings.TrimSuffix(strings.TrimPrefix(path, "/internal/requests/"), "/participants")
		id, err := uuid.Parse(raw)
		if err != nil {
			writeJSON(http.StatusBadRequest, nil)
			return
		}
		customerIDs, ok := p.participant[id]
		if !ok {
			writeJSON(http.StatusNotFound, nil)
			return
		}
		writeJSON(http.StatusOK, map[string]any{"requestId": id, "customerIds": customerIDs})

	case strings.HasPrefix(path, "/internal/sellers/") && !strings.Contains(path, "/by-user/"):
		p.keys["seller"] = r.Header.Get(middleware.InternalAPIKeyHeader)
		id, err := uuid.Parse(strings.TrimPrefix(path, "/internal/sellers/"))
		if err != nil {
			writeJSON(http.StatusBadRequest, nil)
			return
		}
		for userID, sellerID := range p.sellerOf {
			if sellerID == id {
				writeJSON(http.StatusOK, map[string]any{"sellerId": id, "userId": userID})
				return
			}
		}
		writeJSON(http.StatusNotFound, nil)

	case strings.HasPrefix(path, "/internal/sellers/by-user/"):
		p.keys["seller"] = r.Header.Get(middleware.InternalAPIKeyHeader)
		id, err := uuid.Parse(strings.TrimPrefix(path, "/internal/sellers/by-user/"))
		if err != nil {
			writeJSON(http.StatusBadRequest, nil)
			return
		}
		sellerID, ok := p.sellerOf[id]
		if !ok {
			writeJSON(http.StatusNotFound, nil)
			return
		}
		writeJSON(http.StatusOK, map[string]any{"sellerId": sellerID})

	case strings.HasPrefix(path, "/internal/customers/"):
		p.keys["customer"] = r.Header.Get(middleware.InternalAPIKeyHeader)
		id, err := uuid.Parse(strings.TrimPrefix(path, "/internal/customers/"))
		if err != nil {
			writeJSON(http.StatusBadRequest, nil)
			return
		}
		userID, ok := p.userOf[id]
		if !ok {
			writeJSON(http.StatusNotFound, nil)
			return
		}
		writeJSON(http.StatusOK, map[string]any{"customerId": id, "userId": userID})

	case strings.HasPrefix(path, "/internal/users/") && strings.HasSuffix(path, "/phone"):
		p.keys["auth"] = r.Header.Get(middleware.InternalAPIKeyHeader)
		p.phoneCalls++
		raw := strings.TrimSuffix(strings.TrimPrefix(path, "/internal/users/"), "/phone")
		id, err := uuid.Parse(raw)
		if err != nil {
			writeJSON(http.StatusBadRequest, nil)
			return
		}
		phone, ok := p.phones[id]
		if !ok {
			writeJSON(http.StatusNotFound, nil)
			return
		}
		writeJSON(http.StatusOK, map[string]any{"phoneNumber": phone})

	default:
		writeJSON(http.StatusNotFound, nil)
	}
}

// --- fixtures ----------------------------------------------------------------------

// newSeller registers a seller profile and returns their userId, sellerId and token.
func (h *harness) newSeller() (uuid.UUID, uuid.UUID, string) {
	h.t.Helper()
	userID, sellerID := uuid.New(), uuid.New()
	h.p.mu.Lock()
	h.p.sellerOf[userID] = sellerID
	h.p.mu.Unlock()
	return userID, sellerID, h.token(userID, auth.RoleSeller)
}

// newCustomer registers a customer with a phone number in auth-service and returns the
// customerId that request-service would record for them.
func (h *harness) newCustomer(phone string) uuid.UUID {
	h.t.Helper()
	userID, customerID := uuid.New(), uuid.New()
	h.p.mu.Lock()
	h.p.userOf[customerID] = userID
	h.p.phones[userID] = phone
	h.p.mu.Unlock()
	return customerID
}

// newOffer puts a PENDING offer from sellerID against a request joined by customerIDs.
func (h *harness) newOffer(sellerID uuid.UUID, customerIDs ...uuid.UUID) (uuid.UUID, uuid.UUID) {
	h.t.Helper()
	offerID, requestID := uuid.New(), uuid.New()
	h.p.mu.Lock()
	h.p.offers[offerID] = &clients.Offer{
		OfferID: offerID, RequestID: requestID, SellerID: sellerID,
		AvailableQuantity: 10, PricePerUnit: "9.99", Currency: "EUR", Status: statusPending,
	}
	h.p.participant[requestID] = customerIDs
	h.p.mu.Unlock()
	return offerID, requestID
}

func (h *harness) offerStatus(offerID uuid.UUID) string {
	h.t.Helper()
	h.p.mu.Lock()
	defer h.p.mu.Unlock()
	offer, ok := h.p.offers[offerID]
	if !ok {
		return ""
	}
	return offer.Status
}

func (h *harness) adminToken() (uuid.UUID, string) {
	userID := uuid.New()
	return userID, h.token(userID, auth.RoleAdmin)
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

// --- request helpers ---------------------------------------------------------------

type response struct {
	code int
	body map[string]any
	list []map[string]any
	raw  string
}

func (h *harness) do(method, path, token, body string) response {
	h.t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
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

// approve is the happy path most tests start from.
func (h *harness) approve(token string, offerID uuid.UUID) response {
	h.t.Helper()
	return h.do(http.MethodPost, "/api/admin/offers/"+offerID.String()+"/approve", token, `{"reason":"looks good"}`)
}

func num(t *testing.T, body map[string]any, field string) int {
	t.Helper()
	v, ok := body[field].(float64)
	if !ok {
		t.Fatalf("field %q missing or not a number in %v", field, body)
	}
	return int(v)
}

func str(t *testing.T, body map[string]any, field string) string {
	t.Helper()
	v, ok := body[field].(string)
	if !ok {
		t.Fatalf("field %q missing or not a string in %v", field, body)
	}
	return v
}

func countRows(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return n
}
