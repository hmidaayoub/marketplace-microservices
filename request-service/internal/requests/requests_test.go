package requests

import (
	"fmt"
	"net/http"
	"net/url"
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

// --- one item, one request --------------------------------------------------------

// Demand only pools if it lands in one place, so there is no second request to create.
// The customer is told so and handed the one that exists - joining it is their own act,
// not something done to them because a name matched.
func TestCreateRequest_refusesASecondRequestForTheSameItem(t *testing.T) {
	h := newHarness(t)
	_, first := h.newCustomer()
	_, second := h.newCustomer()

	requestID := h.createRequest(first, "Espresso Machine", 3)

	res := h.do(http.MethodPost, "/api/requests", second,
		`{"itemName":"Espresso Machine","description":"mine","category":"other","quantity":5}`)

	if res.code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", res.code, res.raw)
	}

	existing, ok := res.body["existing"].(map[string]any)
	if !ok {
		t.Fatalf("no existing request to join in the refusal: %s", res.raw)
	}
	if existing["requestId"] != requestID {
		t.Errorf("existing = %v, want %s", existing["requestId"], requestID)
	}

	// Nothing was created, and nothing was joined behind the customer's back.
	list := h.do(http.MethodGet, "/api/requests", "", "")
	if len(list.list) != 1 {
		t.Errorf("browse returns %d requests, want 1: %s", len(list.list), list.raw)
	}
	if got := num(t, list.list[0], "totalCustomers"); got != 1 {
		t.Errorf("totalCustomers = %d, want 1 - the caller must not have been enrolled", got)
	}
}

// Nobody types an item name the same way twice, and none of these spellings is a
// different product: case, spacing and punctuation all normalize away.
func TestCreateRequest_refusesEverySpellingOfTheSameName(t *testing.T) {
	h := newHarness(t)
	_, first := h.newCustomer()
	requestID := h.createRequest(first, "Espresso Machine", 3)

	for _, name := range []string{
		"  espresso MACHINE ",
		"Espresso-Machine!",
		// Condition, packaging and the model year say nothing about which product it
		// is, so they leave the key entirely and this is the same name, not a near one.
		"Espresso Machine (brand new)",
		"Original Espresso Machine 2024",
		"the espresso machine, sealed",
	} {
		_, other := h.newCustomer()
		res := h.do(http.MethodPost, "/api/requests", other,
			fmt.Sprintf(`{"itemName":%q,"quantity":1}`, name))

		if res.code != http.StatusConflict {
			t.Fatalf("%q: status = %d, want 409; body %s", name, res.code, res.raw)
		}
		if got := res.body["existing"].(map[string]any)["requestId"]; got != requestID {
			t.Errorf("%q: existing = %v, want %s", name, got, requestID)
		}
	}
}

// An abbreviation shares almost no trigrams with what it abbreviates - "ps5" against
// "playstation 5" scores .06 - so no similarity threshold would ever reach this.
// Resolving the word does, which makes it the same name and refuses it as one.
func TestCreateRequest_refusesThroughAnAbbreviation(t *testing.T) {
	h := newHarness(t)
	_, first := h.newCustomer()
	_, second := h.newCustomer()

	requestID := h.createRequest(first, "PlayStation 5", 2)
	res := h.do(http.MethodPost, "/api/requests", second, `{"itemName":"PS5","quantity":1}`)

	if res.code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", res.code, res.raw)
	}
	if got := res.body["existing"].(map[string]any)["requestId"]; got != requestID {
		t.Errorf("existing = %v, want %s", got, requestID)
	}
}

// Asking twice for the same item is the same conflict as joining twice - and it says so
// rather than offering them a request they are already in.
func TestCreateRequest_refusesAnItemTheCallerHasAlreadyAskedFor(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()
	h.createRequest(token, "Espresso Machine", 3)

	res := h.do(http.MethodPost, "/api/requests", token,
		`{"itemName":"Espresso Machine","quantity":5}`)

	if res.code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", res.code, res.raw)
	}
	if _, offered := res.body["existing"]; offered {
		t.Error("offered a request to join that the caller is already in")
	}
}

// A name made of nothing but filler still has to key to something of its own, or every
// such name would collide with every other.
func TestCreateRequest_keepsANameThatIsEntirelyFiller(t *testing.T) {
	h := newHarness(t)
	_, first := h.newCustomer()
	_, second := h.newCustomer()

	h.createRequest(first, "New", 1)
	res := h.do(http.MethodPost, "/api/requests", second, `{"itemName":"2024","quantity":1}`)

	if res.code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: two names that merely emptied out are not one product; body %s",
			res.code, res.raw)
	}
}

// Only open demand pools. A closed request is not something a customer can join, so
// naming it again opens a fresh one rather than failing.
func TestCreateRequest_opensAFreshRequestWhenTheOnlyMatchIsClosed(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	_, other := h.newCustomer()

	closedID := h.createRequest(owner, "Espresso Machine", 3)
	if res := h.do(http.MethodPost, "/api/requests/"+closedID+"/close", owner, ""); res.code != http.StatusOK {
		t.Fatalf("closing: status %d body %s", res.code, res.raw)
	}

	res := h.do(http.MethodPost, "/api/requests", other,
		`{"itemName":"Espresso Machine","quantity":1}`)

	if res.code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", res.code, res.raw)
	}
	if got := res.body["requestId"]; got == closedID {
		t.Error("matched the closed request instead of opening a new one")
	}
}

// The lookup alone would let two simultaneous creates both find nothing and both open a
// request, splitting the demand the refusal exists to protect. The name is locked for
// the length of the transaction so the losers of the race are refused rather than served.
func TestCreateRequest_concurrentCreatesOfTheSameItemProduceOneRequest(t *testing.T) {
	h := newHarness(t)

	const customers = 8
	tokens := make([]string, customers)
	for i := range tokens {
		_, tokens[i] = h.newCustomer()
	}

	var wg sync.WaitGroup
	for i, token := range tokens {
		wg.Add(1)
		go func(i int, token string) {
			defer wg.Done()
			h.do(http.MethodPost, "/api/requests", token,
				fmt.Sprintf(`{"itemName":"Bulk Coffee","quantity":%d}`, i+1))
		}(i, token)
	}
	wg.Wait()

	list := h.do(http.MethodGet, "/api/requests", "", "")
	if len(list.list) != 1 {
		t.Fatalf("browse returns %d requests, want 1: %s", len(list.list), list.raw)
	}
	// Exactly one create won; the other seven were refused and told to join it.
	if got := num(t, list.list[0], "totalCustomers"); got != 1 {
		t.Errorf("totalCustomers = %d, want 1", got)
	}
}

// --- names that are merely close ---------------------------------------------------
//
// None of these is refused. Whether a close spelling is the same product is a judgement
// about products, and the service has only the spelling to go on, so it offers the
// matches through /api/requests/similar and the customer decides. What these fix is
// that the customer is never surprised: the request they might have meant is in front
// of them before they submit.

func TestCreateRequest_allowsANameThatMerelyLooksLikeOpenDemand(t *testing.T) {
	h := newHarness(t)
	_, first := h.newCustomer()
	h.createRequest(first, "Espresso Machine", 3)

	for _, name := range []string{
		"Espreso Machine",             // a typo
		"Espresso Machine Pro Deluxe", // the same words and more
		"Espresso Machine Stand",      // a different product named after it
	} {
		_, other := h.newCustomer()
		res := h.do(http.MethodPost, "/api/requests", other,
			fmt.Sprintf(`{"itemName":%q,"quantity":1}`, name))

		if res.code != http.StatusCreated {
			t.Errorf("%q: status = %d, want 201 - a close name is a suggestion, not a refusal; body %s",
				name, res.code, res.raw)
		}
	}
}

// ...but every one of them is offered to the customer first.
func TestSimilarRequests_offersTheNamesACreateWillNotRefuse(t *testing.T) {
	h := newHarness(t)
	_, first := h.newCustomer()
	requestID := h.createRequest(first, "Espresso Machine", 3)

	for _, typed := range []string{"Espreso Machine", "Espresso Machine Pro Deluxe", "espresso machine"} {
		res := h.do(http.MethodGet, "/api/requests/similar?itemName="+url.QueryEscape(typed), "", "")

		if res.code != http.StatusOK {
			t.Fatalf("%q: status = %d, want 200; body %s", typed, res.code, res.raw)
		}
		if len(res.list) == 0 {
			t.Fatalf("%q: nothing suggested, so the customer meets the request only by accident", typed)
		}
		if got := res.list[0]["requestId"]; got != requestID {
			t.Errorf("%q: first suggestion = %v, want %s", typed, got, requestID)
		}
	}
}

// The suggestion carries whether it is the same name outright, so the form can say "join
// this, you cannot open another" before the customer submits and learns it from a 409.
func TestSimilarRequests_marksTheSuggestionThatIsTheSameName(t *testing.T) {
	h := newHarness(t)
	_, first := h.newCustomer()
	h.createRequest(first, "Espresso Machine", 3)

	exact := h.do(http.MethodGet, "/api/requests/similar?itemName="+url.QueryEscape("Espresso-Machine!"), "", "")
	if len(exact.list) != 1 || exact.list[0]["exact"] != true {
		t.Errorf("exact = %v, want true for the same name spelled differently: %s",
			exact.list[0]["exact"], exact.raw)
	}

	near := h.do(http.MethodGet, "/api/requests/similar?itemName="+url.QueryEscape("Espresso Machine Pro"), "", "")
	if len(near.list) != 1 {
		t.Fatalf("returned %d matches, want 1: %s", len(near.list), near.raw)
	}
	if got := near.list[0]["exact"]; got != nil {
		t.Errorf("exact = %v, want it absent - this one may still be created", got)
	}
}

// Names far enough apart are two products, and nobody should hear about them.
func TestCreateRequest_leavesAnUnrelatedNameAlone(t *testing.T) {
	h := newHarness(t)
	_, first := h.newCustomer()
	_, second := h.newCustomer()
	h.createRequest(first, "Espresso Machine", 3)

	res := h.do(http.MethodPost, "/api/requests", second, `{"itemName":"Standing Desk","quantity":1}`)

	if res.code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", res.code, res.raw)
	}

	suggestions := h.do(http.MethodGet, "/api/requests/similar?itemName=Standing+Desk", "", "")
	for _, got := range suggestions.list {
		if got["itemName"] == "Espresso Machine" {
			t.Error("suggested an espresso machine to somebody asking for a desk")
		}
	}
}

// Model qualifiers are not noise: they are the whole difference between two products,
// and stripping one would refuse demand that was never for the same thing.
func TestCreateRequest_keepsModelQualifiersApart(t *testing.T) {
	h := newHarness(t)
	_, first := h.newCustomer()
	_, second := h.newCustomer()

	requestID := h.createRequest(first, "iPhone 15", 1)

	res := h.do(http.MethodPost, "/api/requests", second, `{"itemName":"iPhone 15 Pro","quantity":1}`)

	if res.code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", res.code, res.raw)
	}
	if got := res.body["requestId"]; got == requestID {
		t.Error("a Pro is a different product")
	}
}

// --- the suggestion endpoint ------------------------------------------------------

func TestSimilarRequests_ranksNearMatchesAndNeedsNoToken(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	_, other := h.newCustomer()

	espresso := h.createRequest(creator, "Espresso Machine", 3)
	h.createRequest(other, "Standing Desk", 1)

	res := h.do(http.MethodGet, "/api/requests/similar?itemName=espreso+machin", "", "")

	if res.code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", res.code, res.raw)
	}
	if len(res.list) != 1 {
		t.Fatalf("returned %d matches, want only the espresso machine: %s", len(res.list), res.raw)
	}
	if res.list[0]["requestId"] != espresso {
		t.Errorf("requestId = %v, want %s", res.list[0]["requestId"], espresso)
	}
	if _, ok := res.list[0]["similarity"].(float64); !ok {
		t.Errorf("no similarity score to rank by: %v", res.list[0])
	}
	// Same projection browsing serves: totals, never participants.
	if _, leaked := res.list[0]["customerIds"]; leaked {
		t.Error("the suggestion carries participant identity")
	}
}

func TestSimilarRequests_requiresAnItemName(t *testing.T) {
	h := newHarness(t)

	for _, path := range []string{"/api/requests/similar", "/api/requests/similar?itemName=++"} {
		res := h.do(http.MethodGet, path, "", "")
		if res.code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body %s", path, res.code, res.raw)
		}
	}
}

func TestSimilarRequests_returnsAnEmptyListWhenNothingIsClose(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	h.createRequest(creator, "Espresso Machine", 3)

	res := h.do(http.MethodGet, "/api/requests/similar?itemName=bicycle", "", "")

	if res.code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", res.code, res.raw)
	}
	if len(res.list) != 0 {
		t.Errorf("returned %d matches, want none: %s", len(res.list), res.raw)
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

// Browsing is the one thing a visitor may do before signing up: they have to be able
// to see what the platform is before being asked to join it.
func TestBrowsingDemand_needsNoToken(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	requestID := h.createRequest(creator, "Espresso Machine", 3)

	list := h.do(http.MethodGet, "/api/requests?q=espresso", "", "")
	if list.code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body %s", list.code, list.raw)
	}
	if len(list.list) != 1 {
		t.Fatalf("got %d results, want 1: %s", len(list.list), list.raw)
	}

	one := h.do(http.MethodGet, "/api/requests/"+requestID, "", "")
	if one.code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200; body %s", one.code, one.raw)
	}
	// Opening the read must not have widened the projection: an anonymous caller sees
	// exactly what an authenticated seller sees, which is totals and no participants.
	for _, forbidden := range []string{"customerIds", "customerId", "participants"} {
		if _, present := one.body[forbidden]; present {
			t.Errorf("anonymous request detail must not carry %q: %s", forbidden, one.raw)
		}
	}
}

// The reads opened; nothing else did.
func TestWritingDemand_stillRejectsUnauthenticated(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	requestID := h.createRequest(creator, "Blender", 2)

	for _, call := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/requests", `{"itemName":"x","quantity":1}`},
		{http.MethodPost, "/api/requests/" + requestID + "/participants", `{"quantity":1}`},
		{http.MethodPut, "/api/requests/" + requestID + "/participants/me", `{"quantity":2}`},
		{http.MethodDelete, "/api/requests/" + requestID + "/participants/me", ""},
		{http.MethodPost, "/api/requests/" + requestID + "/close", ""},
	} {
		t.Run(call.method+" "+call.path, func(t *testing.T) {
			if res := h.do(call.method, call.path, "", call.body); res.code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401; body %s", res.code, res.raw)
			}
		})
	}
}

// /me is a static segment sharing a position with {requestId}. If it ever stopped
// winning that match it would fall through to the public read and answer 400 for a
// malformed id instead of 401 - a private list quietly turned into a public one.
func TestMyRequests_isNotReachableWithoutAToken(t *testing.T) {
	h := newHarness(t)

	res := h.do(http.MethodGet, "/api/requests/me", "", "")

	if res.code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body %s", res.code, res.raw)
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

func TestCreatingARequestWritesRequestJoinedToTheOutbox(t *testing.T) {
	h := newHarness(t)
	userID, token := h.newCustomer()

	h.createRequest(token, "Espresso Machine", 3)

	outbox := h.outbox()
	if len(outbox) != 1 {
		t.Fatalf("outbox holds %d events, want 1: %+v", len(outbox), outbox)
	}
	if outbox[0].routingKey != events.KeyRequestJoined {
		t.Errorf("routing key = %q, want %q", outbox[0].routingKey, events.KeyRequestJoined)
	}
	// Written by the transaction, not yet sent: the relay is what publishes.
	if outbox[0].publishedAt != nil {
		t.Errorf("row is already marked published; the relay should own that")
	}

	notifications := outbox[0].notifications
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

func TestJoiningARequestWritesRequestJoinedToTheJoiner(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	joinerID, joiner := h.newCustomer()

	requestID := h.createRequest(creator, "Espresso Machine", 3)
	if res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":5}`); res.code != http.StatusCreated {
		t.Fatalf("join: status %d body %s", res.code, res.raw)
	}

	outbox := h.outbox()
	if len(outbox) != 2 {
		t.Fatalf("outbox holds %d events, want 2 (create then join)", len(outbox))
	}

	joined := outbox[1].notifications[0]
	if joined.UserID != joinerID {
		t.Errorf("recipient = %s, want the joiner %s", joined.UserID, joinerID)
	}
	// The joiner is told the demand they just became part of, which means the event is
	// written after the totals are recomputed - in the same transaction.
	if !strings.Contains(joined.Message, "2 customers") || !strings.Contains(joined.Message, "8 in total") {
		t.Errorf("message does not carry the recomputed demand: %q", joined.Message)
	}
}

// The whole point of the outbox: the event and the change that caused it are one write.
func TestAFailedJoinWritesNoEvent(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	_, joiner := h.newCustomer()

	requestID := h.createRequest(creator, "Espresso Machine", 3)
	before := len(h.outbox())

	// A second join by the same customer is a 409.
	h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", creator, `{"quantity":1}`)
	// An unknown request is a 404.
	h.do(http.MethodPost, "/api/requests/"+uuid.New().String()+"/participants", joiner, `{"quantity":1}`)

	if after := len(h.outbox()); after != before {
		t.Errorf("failed operations wrote %d events, want none", after-before)
	}
}

func TestReadsAndLeavesWriteNoEvent(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()
	requestID := h.createRequest(token, "Espresso Machine", 3)
	before := len(h.outbox())

	h.do(http.MethodGet, "/api/requests", token, "")
	h.do(http.MethodGet, "/api/requests/"+requestID, token, "")
	h.do(http.MethodDelete, "/api/requests/"+requestID+"/participants/me", token, "")

	if after := len(h.outbox()); after != before {
		t.Errorf("wrote %d unexpected events", after-before)
	}
}

// Every event carries its own id, so the consumer can tell two events apart and a
// redelivery of one from a fresh occurrence of another.
func TestEachOutboxEventHasADistinctId(t *testing.T) {
	h := newHarness(t)
	_, creator := h.newCustomer()
	_, joiner := h.newCustomer()

	requestID := h.createRequest(creator, "Espresso Machine", 3)
	h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":5}`)

	outbox := h.outbox()
	seen := map[uuid.UUID]bool{}
	for _, e := range outbox {
		if e.eventID == uuid.Nil {
			t.Fatalf("event has no id: %+v", e)
		}
		if seen[e.eventID] {
			t.Errorf("duplicate event id %s", e.eventID)
		}
		seen[e.eventID] = true
	}
}

// The relay is nudged on commit so a notification is not held for the poll interval.
func TestCommittingAnEventWakesTheRelay(t *testing.T) {
	h := newHarness(t)
	_, token := h.newCustomer()

	h.createRequest(token, "Espresso Machine", 3)

	h.waker.mu.Lock()
	defer h.waker.mu.Unlock()
	if h.waker.wakes == 0 {
		t.Error("the relay was never woken, so the event waits for the next tick")
	}
}

// --- closing a request (spec section 18: REQUEST_CLOSED) --------------------------------

func TestClosingARequestNotifiesEveryParticipant(t *testing.T) {
	h := newHarness(t)
	ownerID, owner := h.newCustomer()
	joinerID, joiner := h.newCustomer()

	requestID := h.createRequest(owner, "Espresso Machine", 3)
	if res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":5}`); res.code != http.StatusCreated {
		t.Fatalf("join: status %d body %s", res.code, res.raw)
	}
	before := len(h.outbox())

	res := h.do(http.MethodPost, "/api/requests/"+requestID+"/close", owner, "")
	if res.code != http.StatusOK {
		t.Fatalf("close: status %d body %s", res.code, res.raw)
	}
	if got := res.body["status"]; got != StatusClosed {
		t.Errorf("status = %v, want CLOSED", got)
	}

	outbox := h.outbox()
	if len(outbox) != before+1 {
		t.Fatalf("close wrote %d events, want 1", len(outbox)-before)
	}
	closed := outbox[len(outbox)-1]
	if closed.routingKey != events.KeyRequestClosed {
		t.Errorf("routing key = %q, want %q", closed.routingKey, events.KeyRequestClosed)
	}

	// One event carrying every participant, so the fan-out is one transaction: all of
	// them are told, or none is.
	recipients := map[uuid.UUID]bool{}
	for _, n := range closed.notifications {
		recipients[n.UserID] = true
		if n.Type != "REQUEST_CLOSED" {
			t.Errorf("type = %q, want REQUEST_CLOSED", n.Type)
		}
		if !strings.Contains(n.Message, "Espresso Machine") {
			t.Errorf("message does not name the item: %q", n.Message)
		}
	}
	if !recipients[ownerID] || !recipients[joinerID] || len(recipients) != 2 {
		t.Errorf("recipients = %v, want both the owner and the joiner", recipients)
	}
}

// Closing withdraws demand other people joined, so it is the creator's to do.
func TestOnlyTheCreatorMayCloseARequest(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	_, joiner := h.newCustomer()

	requestID := h.createRequest(owner, "Espresso Machine", 3)
	h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":5}`)
	before := len(h.outbox())

	res := h.do(http.MethodPost, "/api/requests/"+requestID+"/close", joiner, "")
	if res.code != http.StatusForbidden {
		t.Fatalf("participant closing: status %d, want 403; body %s", res.code, res.raw)
	}
	// 403 rather than 404: they can already read the request, they just did not create it.
	if after := len(h.outbox()); after != before {
		t.Errorf("a refused close wrote %d events, want none", after-before)
	}

	// And it really is still open.
	got := h.do(http.MethodGet, "/api/requests/"+requestID, joiner, "")
	if got.body["status"] != StatusOpen {
		t.Errorf("status = %v, want it left OPEN", got.body["status"])
	}
}

func TestClosingTwiceIsAConflict(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)

	if res := h.do(http.MethodPost, "/api/requests/"+requestID+"/close", owner, ""); res.code != http.StatusOK {
		t.Fatalf("first close: status %d body %s", res.code, res.raw)
	}
	before := len(h.outbox())

	if res := h.do(http.MethodPost, "/api/requests/"+requestID+"/close", owner, ""); res.code != http.StatusConflict {
		t.Fatalf("second close: status %d, want 409; body %s", res.code, res.raw)
	}
	if after := len(h.outbox()); after != before {
		t.Error("the second close announced itself again")
	}
}

// Once closed, the demand stops moving - which is what the participants were told.
func TestAClosedRequestAcceptsNoMoreParticipants(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	_, joiner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)

	h.do(http.MethodPost, "/api/requests/"+requestID+"/close", owner, "")

	res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":1}`)
	if res.code != http.StatusConflict {
		t.Errorf("joining a closed request: status %d, want 409", res.code)
	}
}

func TestClosingRequiresACustomerAndAValidId(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)

	if res := h.do(http.MethodPost, "/api/requests/"+requestID+"/close", "", ""); res.code != http.StatusUnauthorized {
		t.Errorf("no token: status %d, want 401", res.code)
	}
	if res := h.do(http.MethodPost, "/api/requests/"+requestID+"/close", h.token(uuid.New(), auth.RoleSeller), ""); res.code != http.StatusForbidden {
		t.Errorf("as a seller: status %d, want 403", res.code)
	}
	if res := h.do(http.MethodPost, "/api/requests/not-a-uuid/close", owner, ""); res.code != http.StatusBadRequest {
		t.Errorf("malformed id: status %d, want 400", res.code)
	}
	if res := h.do(http.MethodPost, "/api/requests/"+uuid.New().String()+"/close", owner, ""); res.code != http.StatusNotFound {
		t.Errorf("unknown request: status %d, want 404", res.code)
	}
}

// --- the internal status API -----------------------------------------------------------

// Offer-service already refuses offers against an OFFER_APPROVED request; until
// something made that transition, the guard could never fire.
func TestInternalStatusMovesARequestForward(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)
	before := len(h.outbox())

	res := h.doInternalWithBody(http.MethodPatch, "/internal/requests/"+requestID+"/status",
		testInternalAPIKey, `{"status":"OFFER_APPROVED"}`)
	if res.code != http.StatusOK {
		t.Fatalf("status %d body %s", res.code, res.raw)
	}
	if got := res.body["status"]; got != StatusOfferApproved {
		t.Errorf("status = %v, want OFFER_APPROVED", got)
	}

	// No notification: the seller is told by Admin/Contact, and the customers have not
	// lost anything yet.
	if after := len(h.outbox()); after != before {
		t.Errorf("a status change announced itself; that is Admin/Contact's job")
	}
}

func TestInternalStatusRejectsWhatItMayNotSet(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)

	// Reopening is not Admin/Contact's to do.
	res := h.doInternalWithBody(http.MethodPatch, "/internal/requests/"+requestID+"/status",
		testInternalAPIKey, `{"status":"OPEN"}`)
	if res.code != http.StatusBadRequest {
		t.Errorf("setting OPEN: status %d, want 400; body %s", res.code, res.raw)
	}

	res = h.doInternalWithBody(http.MethodPatch, "/internal/requests/"+requestID+"/status",
		testInternalAPIKey, `{"status":"NONSENSE"}`)
	if res.code != http.StatusBadRequest {
		t.Errorf("unknown status: status %d, want 400", res.code)
	}
}

func TestInternalStatusRequiresTheSharedKey(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)
	path := "/internal/requests/" + requestID + "/status"

	if res := h.doInternalWithBody(http.MethodPatch, path, "", `{"status":"CLOSED"}`); res.code != http.StatusUnauthorized {
		t.Errorf("no key: status %d, want 401", res.code)
	}
	if res := h.doInternalWithBody(http.MethodPatch, path, "wrong-key", `{"status":"CLOSED"}`); res.code != http.StatusUnauthorized {
		t.Errorf("wrong key: status %d, want 401", res.code)
	}
}
