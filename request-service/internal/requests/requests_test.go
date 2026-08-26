package requests

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/auth"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/events"
)

// --- creating (R1, R3, R4) --------------------------------------------------------

func TestCreateRequest_enrollsCreatorAndCountsDemand(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	res := h.do(http.MethodPost, "/api/requests", token,
		`{"itemName":"Espresso Machine","description":"bar grade","category":"kitchen","quantity":3}`)

	if res.code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", res.code, res.raw)
	}
	if got := res.body["itemName"]; got != "Espresso Machine" {
		t.Errorf("itemName = %v", got)
	}
	if got := res.body["status"]; got != StatusOpen {
		t.Errorf("status = %v, want OPEN", got)
	}
	// R1 and R3 together: the creator wants the item too, so demand starts at one.
	if got := num(t, res.body, "totalCustomers"); got != 1 {
		t.Errorf("totalCustomers = %d, want 1", got)
	}
	if got := num(t, res.body, "totalQuantity"); got != 3 {
		t.Errorf("totalQuantity = %d, want 3", got)
	}
}

func TestCreateRequest_rejectsUnauthenticated(t *testing.T) {
	h := newHarness(t)

	res := h.do(http.MethodPost, "/api/requests", "", `{"itemName":"x","quantity":1}`)

	if res.code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.code)
	}
}

// A seller browses demand but must not manufacture it.
func TestCreateRequest_rejectsSeller(t *testing.T) {
	h := newHarness(t)
	sellerUserID := uuid.New()
	h.profiles[sellerUserID] = uuid.New()

	res := h.do(http.MethodPost, "/api/requests", h.token(sellerUserID, auth.RoleSeller),
		`{"itemName":"x","quantity":1}`)

	if res.code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body %s", res.code, res.raw)
	}
}

func TestCreateRequest_rejectsInvalidBody(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	for name, body := range map[string]string{
		"blank item name":   `{"itemName":"   ","quantity":1}`,
		"zero quantity":     `{"itemName":"x","quantity":0}`,
		"negative quantity": `{"itemName":"x","quantity":-5}`,
		"malformed json":    `{"itemName":`,
	} {
		t.Run(name, func(t *testing.T) {
			res := h.do(http.MethodPost, "/api/requests", token, body)
			if res.code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", res.code, res.raw)
			}
		})
	}
}

// Identity comes from the token subject, so a body field claiming another customer is
// not merely ignored - it is rejected outright.
func TestCreateRequest_rejectsCustomerIdInBody(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	res := h.do(http.MethodPost, "/api/requests", token,
		`{"itemName":"x","quantity":1,"customerId":"`+uuid.New().String()+`"}`)

	if res.code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", res.code, res.raw)
	}
}

// An authenticated user who never created a customer profile has no customerId to
// record, so participation is refused rather than invented.
func TestCreateRequest_rejectsUserWithoutCustomerProfile(t *testing.T) {
	h := newHarness(t)
	orphan := uuid.New() // deliberately not added to h.profiles

	res := h.do(http.MethodPost, "/api/requests", h.token(orphan, auth.RoleCustomer),
		`{"itemName":"x","quantity":1}`)

	if res.code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body %s", res.code, res.raw)
	}
}

func TestCreateRequest_sendsInternalApiKeyToCustomerService(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	h.createRequest(token, "Keyed", 1)

	if h.sawAPIKey != testInternalAPIKey {
		t.Fatalf("customer-service saw api key %q, want %q", h.sawAPIKey, testInternalAPIKey)
	}
}

// --- joining (R2, R3, R4) ---------------------------------------------------------

func TestJoinRequest_aggregatesDemandAcrossCustomers(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	_, joiner := h.newCustomer()

	requestID := h.createRequest(creator, "Standing Desk", 2)

	res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":5}`)

	if res.code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", res.code, res.raw)
	}
	if got := num(t, res.body, "totalCustomers"); got != 2 {
		t.Errorf("totalCustomers = %d, want 2", got)
	}
	if got := num(t, res.body, "totalQuantity"); got != 7 {
		t.Errorf("totalQuantity = %d, want 7 (2 + 5)", got)
	}
}

func TestJoinRequest_rejectsSecondJoinBySameCustomer(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	_, joiner := h.newCustomer()
	requestID := h.createRequest(creator, "Monitor", 1)

	if res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":2}`); res.code != http.StatusCreated {
		t.Fatalf("first join: status %d", res.code)
	}

	res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":9}`)

	if res.code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", res.code, res.raw)
	}
}

// The creator is already a participant, so joining their own request is the same
// conflict rather than a second line.
func TestJoinRequest_rejectsCreatorJoiningAgain(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	requestID := h.createRequest(creator, "Chair", 1)

	res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", creator, `{"quantity":1}`)

	if res.code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", res.code, res.raw)
	}
}

func TestJoinRequest_returns404ForUnknownRequest(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	res := h.do(http.MethodPost, "/api/requests/"+uuid.New().String()+"/participants", token, `{"quantity":1}`)

	if res.code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", res.code, res.raw)
	}
}

// Concurrency is the reason the mutation path locks the request row. Without the lock
// each transaction recomputes totals from a snapshot that misses the others' inserts,
// and the stored demand ends up lower than the participants imply (R4).
func TestJoinRequest_concurrentJoinsProduceCorrectTotals(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	requestID := h.createRequest(creator, "Bulk Coffee", 1)

	const joiners = 8
	tokens := make([]string, joiners)
	for i := range tokens {
		_, tokens[i] = h.newCustomer()
	}

	var wg sync.WaitGroup
	for i, token := range tokens {
		wg.Add(1)
		go func(i int, token string) {
			defer wg.Done()
			h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", token,
				fmt.Sprintf(`{"quantity":%d}`, i+1))
		}(i, token)
	}
	wg.Wait()

	res := h.do(http.MethodGet, "/api/requests/"+requestID, creator, "")

	// creator (1) + joiners 1..8 => 9 customers, 1 + 36 = 37 units
	if got := num(t, res.body, "totalCustomers"); got != joiners+1 {
		t.Errorf("totalCustomers = %d, want %d", got, joiners+1)
	}
	if got := num(t, res.body, "totalQuantity"); got != 37 {
		t.Errorf("totalQuantity = %d, want 37", got)
	}
}

// --- changing and leaving ---------------------------------------------------------

func TestUpdateQuantity_recalculatesDemand(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	requestID := h.createRequest(creator, "Keyboard", 2)

	res := h.do(http.MethodPut, "/api/requests/"+requestID+"/participants/me", creator, `{"quantity":10}`)

	if res.code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", res.code, res.raw)
	}
	if got := num(t, res.body, "totalQuantity"); got != 10 {
		t.Errorf("totalQuantity = %d, want 10", got)
	}
	if got := num(t, res.body, "totalCustomers"); got != 1 {
		t.Errorf("totalCustomers = %d, want 1", got)
	}
}

func TestUpdateQuantity_returns404WhenNotAParticipant(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	_, outsider := h.newCustomer()
	requestID := h.createRequest(creator, "Lamp", 1)

	res := h.do(http.MethodPut, "/api/requests/"+requestID+"/participants/me", outsider, `{"quantity":4}`)

	if res.code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", res.code, res.raw)
	}
}

func TestLeaveRequest_removesParticipantAndDemand(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	_, joiner := h.newCustomer()
	requestID := h.createRequest(creator, "Rug", 2)

	if res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":6}`); res.code != http.StatusCreated {
		t.Fatalf("join: status %d", res.code)
	}

	res := h.do(http.MethodDelete, "/api/requests/"+requestID+"/participants/me", joiner, "")
	if res.code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body %s", res.code, res.raw)
	}

	after := h.do(http.MethodGet, "/api/requests/"+requestID, creator, "")
	if got := num(t, after.body, "totalCustomers"); got != 1 {
		t.Errorf("totalCustomers = %d, want 1", got)
	}
	if got := num(t, after.body, "totalQuantity"); got != 2 {
		t.Errorf("totalQuantity = %d, want 2", got)
	}
}

func TestLeaveRequest_returns404WhenNotAParticipant(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	_, outsider := h.newCustomer()
	requestID := h.createRequest(creator, "Shelf", 1)

	res := h.do(http.MethodDelete, "/api/requests/"+requestID+"/participants/me", outsider, "")

	if res.code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", res.code, res.raw)
	}
}

// --- reading ----------------------------------------------------------------------

// A seller needs the aggregated demand to price an offer, but must not learn which
// customers are behind it. Customer identity leaves only through the internal API.
func TestGetRequest_neverExposesParticipants(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	sellerUserID := uuid.New()
	requestID := h.createRequest(creator, "Projector", 4)

	res := h.do(http.MethodGet, "/api/requests/"+requestID, h.token(sellerUserID, auth.RoleSeller), "")

	if res.code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", res.code, res.raw)
	}
	if got := num(t, res.body, "totalQuantity"); got != 4 {
		t.Errorf("totalQuantity = %d, want 4", got)
	}
	for _, forbidden := range []string{"customerIds", "customerId", "participants"} {
		if _, present := res.body[forbidden]; present {
			t.Errorf("public request detail must not carry %q: %s", forbidden, res.raw)
		}
	}
}

func TestListRequests_filtersByItemNameAndStatus(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	h.createRequest(creator, "Ceramic Mug", 1)

	_, other := h.newCustomer()
	h.createRequest(other, "Steel Bottle", 1)

	res := h.do(http.MethodGet, "/api/requests?q=mug&status=OPEN", creator, "")

	if res.code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", res.code, res.raw)
	}
	if len(res.list) != 1 {
		t.Fatalf("got %d results, want 1: %s", len(res.list), res.raw)
	}
	if got := res.list[0]["itemName"]; got != "Ceramic Mug" {
		t.Errorf("itemName = %v", got)
	}
}

func TestListRequests_rejectsOversizedPage(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	res := h.do(http.MethodGet, "/api/requests?limit=5000", token, "")

	if res.code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", res.code, res.raw)
	}
}

func TestMyRequests_returnsJoinedAndCreated(t *testing.T) {
	h := newHarness(t)
	_, alice := h.newCustomer()
	_, bob := h.newCustomer()

	h.createRequest(alice, "Alice Item", 1)
	bobsRequest := h.createRequest(bob, "Bob Item", 1)

	if res := h.do(http.MethodPost, "/api/requests/"+bobsRequest+"/participants", alice, `{"quantity":2}`); res.code != http.StatusCreated {
		t.Fatalf("join: status %d", res.code)
	}

	res := h.do(http.MethodGet, "/api/requests/me", alice, "")

	if res.code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", res.code, res.raw)
	}
	if len(res.list) != 2 {
		t.Fatalf("got %d requests, want 2 (one created, one joined): %s", len(res.list), res.raw)
	}
}

// Issue #28 in the Java services: a non-UUID path variable must be a 400, not a 500.
func TestMalformedRequestId_returns400(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	for _, path := range []string{
		"/api/requests/not-a-uuid",
		"/api/requests/not-a-uuid/participants",
	} {
		res := h.do(http.MethodGet, path, token, "")
		if res.code != http.StatusBadRequest && res.code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s = %d, want 400", path, res.code)
		}
	}

	res := h.do(http.MethodPost, "/api/requests/not-a-uuid/participants", token, `{"quantity":1}`)
	if res.code != http.StatusBadRequest {
		t.Errorf("POST participants = %d, want 400; body %s", res.code, res.raw)
	}
}

// --- internal API -----------------------------------------------------------------

func TestInternalEndpoints_requireApiKey(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	requestID := h.createRequest(creator, "Internal", 3)

	for _, path := range []string{
		"/internal/requests/" + requestID,
		"/internal/requests/" + requestID + "/demand",
		"/internal/requests/" + requestID + "/participants",
	} {
		t.Run("missing key "+path, func(t *testing.T) {
			if res := h.doInternal(http.MethodGet, path, ""); res.code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", res.code)
			}
		})
		t.Run("wrong key "+path, func(t *testing.T) {
			if res := h.doInternal(http.MethodGet, path, "not-the-key"); res.code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", res.code)
			}
		})
	}
}

// A user JWT must not open the internal API: the two are separate call styles.
func TestInternalEndpoints_rejectUserJwt(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	requestID := h.createRequest(creator, "Internal", 1)

	res := h.do(http.MethodGet, "/internal/requests/"+requestID, creator, "")

	if res.code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body %s", res.code, res.raw)
	}
}

func TestInternalDemand_returnsTotals(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	_, joiner := h.newCustomer()
	requestID := h.createRequest(creator, "Demand", 4)

	if res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":6}`); res.code != http.StatusCreated {
		t.Fatalf("join: status %d", res.code)
	}

	res := h.doInternal(http.MethodGet, "/internal/requests/"+requestID+"/demand", testInternalAPIKey)

	if res.code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", res.code, res.raw)
	}
	if got := num(t, res.body, "totalCustomers"); got != 2 {
		t.Errorf("totalCustomers = %d, want 2", got)
	}
	if got := num(t, res.body, "totalQuantity"); got != 10 {
		t.Errorf("totalQuantity = %d, want 10", got)
	}
}

// This is the route Admin/Contact uses to work out whose phone numbers a granted
// seller may reach, so it is the one place customerIds are returned.
func TestInternalParticipants_returnsCustomerIds(t *testing.T) {
	h := newHarness(t)
	creatorUserID, creator := h.newCustomer()
	joinerUserID, joiner := h.newCustomer()
	requestID := h.createRequest(creator, "Participants", 1)

	if res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":1}`); res.code != http.StatusCreated {
		t.Fatalf("join: status %d", res.code)
	}

	res := h.doInternal(http.MethodGet, "/internal/requests/"+requestID+"/participants", testInternalAPIKey)

	if res.code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", res.code, res.raw)
	}
	raw, ok := res.body["customerIds"].([]any)
	if !ok {
		t.Fatalf("customerIds missing: %s", res.raw)
	}

	got := map[string]bool{}
	for _, v := range raw {
		got[v.(string)] = true
	}
	for _, userID := range []uuid.UUID{creatorUserID, joinerUserID} {
		want := h.profiles[userID].String()
		if !got[want] {
			t.Errorf("customerIds %v is missing %s", raw, want)
		}
	}

	// R10: no phone number may appear anywhere in this service's output.
	if containsAny(res.raw, "phone", "phoneNumber") {
		t.Errorf("participants response must never carry phone data: %s", res.raw)
	}
}

func TestInternalGet_returns404ForUnknownRequest(t *testing.T) {
	h := newHarness(t)

	res := h.doInternal(http.MethodGet, "/internal/requests/"+uuid.New().String(), testInternalAPIKey)

	if res.code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", res.code, res.raw)
	}
}

func TestInternalGet_returns400ForMalformedId(t *testing.T) {
	h := newHarness(t)

	res := h.doInternal(http.MethodGet, "/internal/requests/not-a-uuid", testInternalAPIKey)

	if res.code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", res.code, res.raw)
	}
}

// --- health -----------------------------------------------------------------------

func TestHealth_isReachableWithoutCredentials(t *testing.T) {
	h := newHarness(t)

	for _, path := range []string{"/health", "/actuator/health"} {
		res := h.do(http.MethodGet, path, "", "")
		if res.code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, res.code)
		}
		if got := res.body["status"]; got != "UP" {
			t.Errorf("GET %s status = %v, want UP", path, got)
		}
	}
}

func containsAny(haystack string, needles ...string) bool {
	lower := strings.ToLower(haystack)
	for _, n := range needles {
		if strings.Contains(lower, strings.ToLower(n)) {
			return true
		}
	}
	return false
}

// --- notification events (spec flow 1 step 8, docs/events.md) -------------------------

func TestCreatingARequestEmitsRequestJoinedToTheCreator(t *testing.T) {
	h := newHarness(t)
	userID, token := h.newCustomer()

	h.createRequest(token, "Espresso Machine", 3)

	published := h.notifier.events()
	if len(published) != 1 {
		t.Fatalf("published %d events, want 1: %+v", len(published), published)
	}
	if published[0].routingKey != events.KeyRequestJoined {
		t.Errorf("routing key = %q, want %q", published[0].routingKey, events.KeyRequestJoined)
	}

	notifications := published[0].notifications
	if len(notifications) != 1 {
		t.Fatalf("carried %d notifications, want 1", len(notifications))
	}
	// Addressed by the token subject, not the customerId the request records:
	// notification-service never resolves an identity.
	if notifications[0].UserID != userID {
		t.Errorf("recipient = %s, want the token subject %s", notifications[0].UserID, userID)
	}
	if notifications[0].Type != "REQUEST_JOINED" {
		t.Errorf("type = %q, want REQUEST_JOINED", notifications[0].Type)
	}
	if !strings.Contains(notifications[0].Message, "Espresso Machine") {
		t.Errorf("message does not name the item: %q", notifications[0].Message)
	}
}

func TestJoiningARequestEmitsRequestJoinedToTheJoiner(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	joinerID, joiner := h.newCustomer()

	requestID := h.createRequest(creator, "Espresso Machine", 3)
	if res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":5}`); res.code != http.StatusCreated {
		t.Fatalf("join: status %d body %s", res.code, res.raw)
	}

	published := h.notifier.events()
	if len(published) != 2 {
		t.Fatalf("published %d events, want 2 (create then join)", len(published))
	}

	joined := published[1].notifications[0]
	if joined.UserID != joinerID {
		t.Errorf("recipient = %s, want the joiner %s", joined.UserID, joinerID)
	}
	// The joiner is told the demand they just became part of.
	if !strings.Contains(joined.Message, "2 customers") || !strings.Contains(joined.Message, "8 in total") {
		t.Errorf("message does not carry the recomputed demand: %q", joined.Message)
	}
}

// The event must never be able to fail the operation that produced it - a request that
// was genuinely joined stays joined whether or not the message went out.
func TestAFailedJoinEmitsNothing(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	_, joiner := h.newCustomer()

	requestID := h.createRequest(creator, "Espresso Machine", 3)
	before := len(h.notifier.events())

	// A second join by the same customer is a 409.
	h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", creator, `{"quantity":1}`)
	// An unknown request is a 404.
	h.do(http.MethodPost, "/api/requests/"+uuid.New().String()+"/participants", joiner, `{"quantity":1}`)

	if after := len(h.notifier.events()); after != before {
		t.Errorf("failed operations published %d events, want none", after-before)
	}
}

func TestReadsAndLeavesEmitNothing(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()
	requestID := h.createRequest(token, "Espresso Machine", 3)
	before := len(h.notifier.events())

	h.do(http.MethodGet, "/api/requests", token, "")
	h.do(http.MethodGet, "/api/requests/"+requestID, token, "")
	h.do(http.MethodDelete, "/api/requests/"+requestID+"/participants/me", token, "")

	if after := len(h.notifier.events()); after != before {
		t.Errorf("published %d unexpected events", after-before)
	}
}
