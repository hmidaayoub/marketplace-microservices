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
	h.putProfile(sellerUserID)

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

	if seen := h.apiKeySeen(); seen != testInternalAPIKey {
		t.Fatalf("customer-service saw api key %q, want %q", seen, testInternalAPIKey)
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

// A request nobody is on is still the request for that item. Joining it is what makes it
// demand again - and it may already carry a seller's offer - so a second one would split
// what the first is waiting to pool.
func TestCreateRequest_handsOverTheEmptiedRequestRatherThanOpeningASecond(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	_, other := h.newCustomer()

	emptiedID := h.createRequest(owner, "Espresso Machine", 3)
	if res := h.do(http.MethodDelete, "/api/requests/"+emptiedID+"/participants/me", owner, ""); res.code != http.StatusNoContent {
		t.Fatalf("leaving: status %d body %s", res.code, res.raw)
	}

	res := h.do(http.MethodPost, "/api/requests", other,
		`{"itemName":"Espresso Machine","quantity":1}`)

	if res.code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", res.code, res.raw)
	}
	existing, ok := res.body["existing"].(map[string]any)
	if !ok || existing["requestId"] != emptiedID {
		t.Fatalf("the emptied request was not offered to join: %s", res.raw)
	}
	// The wording has to fit what is actually there: "join the open request" would read
	// as a mistake about a request with nobody on it.
	if msg, _ := res.body["message"].(string); !strings.Contains(msg, "nobody on it") {
		t.Errorf("message = %q, want it to say the request has nobody on it", msg)
	}
}

// And joining it is what the refusal promised: the emptied request comes back rather
// than a second one being needed.
func TestCreateRequest_theEmptiedRequestItOffersCanBeJoined(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	_, other := h.newCustomer()

	emptiedID := h.createRequest(owner, "Espresso Machine", 3)
	if res := h.do(http.MethodDelete, "/api/requests/"+emptiedID+"/participants/me", owner, ""); res.code != http.StatusNoContent {
		t.Fatalf("leaving: status %d body %s", res.code, res.raw)
	}

	res := h.do(http.MethodPost, "/api/requests/"+emptiedID+"/participants", other, `{"quantity":4}`)

	if res.code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", res.code, res.raw)
	}
	if got := res.body["status"]; got != "OPEN" {
		t.Errorf("status = %v, want OPEN - the join revived it", got)
	}
	list := h.do(http.MethodGet, "/api/requests", "", "")
	if len(list.list) != 1 {
		t.Errorf("browse returns %d requests, want 1 - the demand must not have split: %s",
			len(list.list), list.raw)
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

// What browsing and searching are built on, one level up: the browse page asks for OPEN
// and a search drops the filter. Dormant demand is not worth putting in front of someone
// looking around - nothing is happening on it - but it has to be findable by name,
// because the platform allows one request per item and somebody typing that name would
// otherwise be refused by a request they were never shown.
func TestListRequests_dormantDemandIsFoundByNameButNotByBrowsing(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()

	requestID := h.createRequest(owner, "Ceramic Mug", 1)
	if res := h.do(http.MethodDelete, "/api/requests/"+requestID+"/participants/me", owner, ""); res.code != http.StatusNoContent {
		t.Fatalf("emptying it: status %d body %s", res.code, res.raw)
	}

	browsing := h.do(http.MethodGet, "/api/requests?status=OPEN", "", "")
	if len(browsing.list) != 0 {
		t.Errorf("browsing shows %d requests, want none - it is dormant: %s",
			len(browsing.list), browsing.raw)
	}

	searching := h.do(http.MethodGet, "/api/requests?q=mug", "", "")
	if len(searching.list) != 1 {
		t.Fatalf("searching finds %d requests, want 1: %s", len(searching.list), searching.raw)
	}
	if got := searching.list[0]["status"]; got != "INACTIVE" {
		t.Errorf("status = %v, want INACTIVE", got)
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
		customerID, _ := h.customerFor(userID)
		want := customerID.String()
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

// --- a request opened for a seller, with nobody on it -------------------------------
//
// Demand and supply do not have to arrive in that order. A seller holding stock nobody
// has asked for is worth letting speak first, and the request their offer needs is what
// gives buyers somewhere to arrive. offer-service opens it through this endpoint.

func TestInternalEnsure_opensARequestNobodyIsOn(t *testing.T) {
	h := newHarness(t)

	res := h.doInternalWithBody(http.MethodPost, "/internal/requests", testInternalAPIKey,
		`{"itemName":"Espresso Machine","description":"forty in stock","category":"kitchen"}`)

	if res.code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", res.code, res.raw)
	}
	if got := res.body["status"]; got != "INACTIVE" {
		t.Errorf("status = %v, want INACTIVE - nobody is on it", got)
	}
	if got := num(t, res.body, "totalCustomers"); got != 0 {
		t.Errorf("totalCustomers = %d, want 0", got)
	}
	if got := num(t, res.body, "totalQuantity"); got != 0 {
		t.Errorf("totalQuantity = %d, want 0", got)
	}
	// A seller is not a customer and cannot join, so recording them as the buyer who
	// wanted this would be a lie about who the request belongs to.
	if owner, present := res.body["createdBy"]; present {
		t.Errorf("createdBy = %v, want none - nobody asked for this item", owner)
	}
	if got := res.body["itemName"]; got != "Espresso Machine" {
		t.Errorf("itemName = %v, want it kept as typed", got)
	}
}

// It is find-or-create, not create. An item that already has demand must not get a
// second request, which is the whole rule a customer create is refused on.
func TestInternalEnsure_returnsTheRequestTheItemAlreadyHas(t *testing.T) {
	h := newHarness(t)
	_, customer := h.newCustomer()
	existingID := h.createRequest(customer, "Espresso Machine", 3)

	res := h.doInternalWithBody(http.MethodPost, "/internal/requests", testInternalAPIKey,
		`{"itemName":"Espresso Machine"}`)

	if res.code != http.StatusOK {
		t.Fatalf("status = %d, want 200 - nothing was created; body %s", res.code, res.raw)
	}
	if res.body["requestId"] != existingID {
		t.Errorf("requestId = %v, want the existing %s", res.body["requestId"], existingID)
	}
	// The demand it found is untouched: the seller's offer joins it, it does not reset it.
	if got := num(t, res.body, "totalCustomers"); got != 1 {
		t.Errorf("totalCustomers = %d, want 1", got)
	}
	if got := res.body["status"]; got != "OPEN" {
		t.Errorf("status = %v, want OPEN", got)
	}
}

// The same normalization a customer create is matched on. A seller typing the name their
// own way must not open a second request for a product that already has one.
func TestInternalEnsure_matchesAnItemHoweverItIsSpelled(t *testing.T) {
	h := newHarness(t)
	_, customer := h.newCustomer()
	existingID := h.createRequest(customer, "Espresso Machine", 3)

	for _, name := range []string{"  espresso MACHINE ", "Espresso-Machine!", "Espresso Machine 2024"} {
		res := h.doInternalWithBody(http.MethodPost, "/internal/requests", testInternalAPIKey,
			fmt.Sprintf(`{"itemName":%q}`, name))

		if res.code != http.StatusOK {
			t.Errorf("%q: status = %d, want 200; body %s", name, res.code, res.raw)
			continue
		}
		if res.body["requestId"] != existingID {
			t.Errorf("%q: opened %v instead of matching %s", name, res.body["requestId"], existingID)
		}
	}
}

// Twice for the same item is one request, so a seller who offers twice does not split
// the demand their own first offer is waiting for.
func TestInternalEnsure_isIdempotentForTheSameItem(t *testing.T) {
	h := newHarness(t)

	first := h.doInternalWithBody(http.MethodPost, "/internal/requests", testInternalAPIKey,
		`{"itemName":"Standing Desk"}`)
	second := h.doInternalWithBody(http.MethodPost, "/internal/requests", testInternalAPIKey,
		`{"itemName":"Standing Desk"}`)

	if first.code != http.StatusCreated || second.code != http.StatusOK {
		t.Fatalf("statuses = %d then %d, want 201 then 200", first.code, second.code)
	}
	if first.body["requestId"] != second.body["requestId"] {
		t.Errorf("two ids for one item: %v and %v", first.body["requestId"], second.body["requestId"])
	}
}

// The lock is shared with Create, which is what makes this true across the two paths as
// well as within this one.
func TestInternalEnsure_concurrentCallsProduceOneRequest(t *testing.T) {
	h := newHarness(t)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.doInternalWithBody(http.MethodPost, "/internal/requests", testInternalAPIKey,
				`{"itemName":"Bulk Coffee"}`)
		}()
	}
	wg.Wait()

	list := h.do(http.MethodGet, "/api/requests?status=INACTIVE", "", "")
	if len(list.list) != 1 {
		t.Fatalf("browse returns %d requests, want 1: %s", len(list.list), list.raw)
	}
}

// Nobody joined, so there is nobody to tell. The seller learns the outcome from the
// response to the offer they were making.
func TestInternalEnsure_writesNoEvent(t *testing.T) {
	h := newHarness(t)

	h.doInternalWithBody(http.MethodPost, "/internal/requests", testInternalAPIKey,
		`{"itemName":"Espresso Machine"}`)

	if events := h.outbox(); len(events) != 0 {
		t.Errorf("outbox has %d events, want none", len(events))
	}
}

func TestInternalEnsure_rejectsANamelessItem(t *testing.T) {
	h := newHarness(t)

	for _, body := range []string{`{}`, `{"itemName":"   "}`, `{"itemName":""}`} {
		res := h.doInternalWithBody(http.MethodPost, "/internal/requests", testInternalAPIKey, body)
		if res.code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body %s", body, res.code, res.raw)
		}
	}
}

// Opening demand is otherwise a customer action, and this endpoint enrolls nobody - so
// it is behind the internal key like the rest of /internal, and a user token is not it.
func TestInternalEnsure_isNotReachableWithoutTheInternalKey(t *testing.T) {
	h := newHarness(t)
	_, customer := h.newCustomer()

	for name, res := range map[string]response{
		"no key":    h.doInternalWithBody(http.MethodPost, "/internal/requests", "", `{"itemName":"Drone"}`),
		"wrong key": h.doInternalWithBody(http.MethodPost, "/internal/requests", "not-the-key", `{"itemName":"Drone"}`),
		"user jwt":  h.do(http.MethodPost, "/internal/requests", customer, `{"itemName":"Drone"}`),
	} {
		if res.code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401; body %s", name, res.code, res.raw)
		}
	}

	list := h.do(http.MethodGet, "/api/requests?status=INACTIVE", "", "")
	if len(list.list) != 0 {
		t.Errorf("something opened a request anyway: %s", list.raw)
	}
}

// The first customer to want the item finds the seller's request instead of opening a
// rival one - which is the point of the whole arrangement. Without this the seller's
// offer would sit on a request that never fills while the demand pooled elsewhere.
func TestCreateRequest_findsTheRequestASellerOpened(t *testing.T) {
	h := newHarness(t)
	_, customer := h.newCustomer()

	opened := h.doInternalWithBody(http.MethodPost, "/internal/requests", testInternalAPIKey,
		`{"itemName":"Drone"}`)
	sellerOpenedID := opened.body["requestId"]

	refused := h.do(http.MethodPost, "/api/requests", customer, `{"itemName":"drone","quantity":2}`)

	if refused.code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", refused.code, refused.raw)
	}
	existing, ok := refused.body["existing"].(map[string]any)
	if !ok || existing["requestId"] != sellerOpenedID {
		t.Fatalf("was not handed the seller's request to join: %s", refused.raw)
	}

	joined := h.do(http.MethodPost, "/api/requests/"+sellerOpenedID.(string)+"/participants", customer,
		`{"quantity":2}`)
	if joined.code != http.StatusCreated {
		t.Fatalf("joining: status = %d, want 201; body %s", joined.code, joined.raw)
	}
	if got := joined.body["status"]; got != "OPEN" {
		t.Errorf("status = %v, want OPEN - the item now has a buyer", got)
	}
	if got := num(t, joined.body, "totalQuantity"); got != 2 {
		t.Errorf("totalQuantity = %d, want 2", got)
	}
}

// And it is suggested while they type, before they ever reach the refusal.
func TestSimilarRequests_suggestsARequestWithNoBuyersOnIt(t *testing.T) {
	h := newHarness(t)

	h.doInternalWithBody(http.MethodPost, "/internal/requests", testInternalAPIKey,
		`{"itemName":"Espresso Machine"}`)

	res := h.do(http.MethodGet, "/api/requests/similar?itemName="+url.QueryEscape("espreso machin"), "", "")

	if res.code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", res.code, res.raw)
	}
	if len(res.list) != 1 {
		t.Fatalf("%d suggestions, want 1: %s", len(res.list), res.raw)
	}
	// The status travels with it, so a client can say what it is offering: an item
	// somebody is selling rather than one buyers have pooled behind.
	if got := res.list[0]["status"]; got != "INACTIVE" {
		t.Errorf("status = %v, want INACTIVE", got)
	}
}

// --- offers keep a request open ------------------------------------------------------
//
// A request nobody is on but somebody is selling into is not dormant. The count comes
// from offer-service, because the offers are its data; what the count means is decided
// here.

// setOfferCount is offer-service reporting what stands on a request.
func (h *harness) setOfferCount(requestID string, count int) response {
	h.t.Helper()
	return h.doInternalWithBody(http.MethodPut,
		"/internal/requests/"+requestID+"/offers/count", testInternalAPIKey,
		fmt.Sprintf(`{"totalOffers":%d}`, count))
}

func TestOfferCount_keepsARequestOpenWhenTheLastBuyerLeaves(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()

	requestID := h.createRequest(owner, "Espresso Machine", 3)
	if res := h.setOfferCount(requestID, 1); res.code != http.StatusOK {
		t.Fatalf("reporting the count: status %d body %s", res.code, res.raw)
	}

	res := h.do(http.MethodDelete, "/api/requests/"+requestID+"/participants/me", owner, "")
	if res.code != http.StatusNoContent {
		t.Fatalf("leaving: status %d body %s", res.code, res.raw)
	}

	after := h.do(http.MethodGet, "/api/requests/"+requestID, "", "")
	if got := after.body["status"]; got != "OPEN" {
		t.Errorf("status = %v, want OPEN - an offer still stands on it", got)
	}
	if got := num(t, after.body, "totalCustomers"); got != 0 {
		t.Errorf("totalCustomers = %d, want 0", got)
	}
}

// Both counts have to be zero. One of them is not enough.
func TestOfferCount_aRequestWithNeitherBuyersNorOffersIsInactive(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()

	requestID := h.createRequest(owner, "Espresso Machine", 3)
	h.setOfferCount(requestID, 1)
	h.do(http.MethodDelete, "/api/requests/"+requestID+"/participants/me", owner, "")

	// The seller withdraws, and now nothing is holding it open.
	res := h.setOfferCount(requestID, 0)

	if res.code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", res.code, res.raw)
	}
	if got := res.body["status"]; got != "INACTIVE" {
		t.Errorf("status = %v, want INACTIVE - nobody wants it and nobody is selling it", got)
	}
}

// The request a seller opened by offering against an item: no buyers from the start, and
// its own offer is what keeps it from reading as dormant.
func TestOfferCount_opensTheRequestASellerCreatedForTheirOwnOffer(t *testing.T) {
	h := newHarness(t)

	opened := h.doInternalWithBody(http.MethodPost, "/internal/requests", testInternalAPIKey,
		`{"itemName":"Drone"}`)
	requestID := opened.body["requestId"].(string)
	if got := opened.body["status"]; got != "INACTIVE" {
		t.Fatalf("status = %v, want INACTIVE before any offer is reported", got)
	}

	res := h.setOfferCount(requestID, 1)

	if got := res.body["status"]; got != "OPEN" {
		t.Errorf("status = %v, want OPEN - a seller is offering on it", got)
	}
	if got := num(t, res.body, "totalOffers"); got != 1 {
		t.Errorf("totalOffers = %d, want 1", got)
	}
}

// The count travels with the request everywhere it is read, because it is half of what
// the status means and somebody browsing demand should see what they are up against.
func TestOfferCount_isCarriedByEveryProjectionOfARequest(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)
	h.setOfferCount(requestID, 2)

	one := h.do(http.MethodGet, "/api/requests/"+requestID, "", "")
	if got := num(t, one.body, "totalOffers"); got != 2 {
		t.Errorf("reading one request: totalOffers = %d, want 2", got)
	}

	list := h.do(http.MethodGet, "/api/requests", "", "")
	if got := num(t, list.list[0], "totalOffers"); got != 2 {
		t.Errorf("browsing: totalOffers = %d, want 2", got)
	}

	mine := h.do(http.MethodGet, "/api/requests/me", owner, "")
	if got := num(t, mine.list[0], "totalOffers"); got != 2 {
		t.Errorf("my requests: totalOffers = %d, want 2", got)
	}
}

// A participant change must not disturb it. RecalculateDemand reads the column rather
// than recomputing it, because it cannot see the offers behind it.
func TestOfferCount_survivesAJoin(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	_, joiner := h.newCustomer()

	requestID := h.createRequest(owner, "Espresso Machine", 3)
	h.setOfferCount(requestID, 3)

	res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":2}`)

	if got := num(t, res.body, "totalOffers"); got != 3 {
		t.Errorf("totalOffers = %d, want it left at 3 by a join", got)
	}
}

// A count, not a delta - which is what makes the call safe for offer-service to retry.
func TestOfferCount_isIdempotent(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)

	h.setOfferCount(requestID, 2)
	res := h.setOfferCount(requestID, 2)

	if got := num(t, res.body, "totalOffers"); got != 2 {
		t.Errorf("totalOffers = %d, want 2 - a repeated report is not a second offer", got)
	}
}

func TestOfferCount_rejectsANonsenseCount(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)

	for _, body := range []string{`{}`, `{"totalOffers":-1}`, `{"totalOffers":"two"}`} {
		res := h.doInternalWithBody(http.MethodPut,
			"/internal/requests/"+requestID+"/offers/count", testInternalAPIKey, body)
		if res.code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body %s", body, res.code, res.raw)
		}
	}
}

func TestOfferCount_returns404ForAnUnknownRequest(t *testing.T) {
	h := newHarness(t)
	if res := h.setOfferCount(uuid.NewString(), 1); res.code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.code)
	}
}

// Nobody outside the platform may assert what offers exist: the only honest source is
// the service that owns them.
func TestOfferCount_isNotReachableWithoutTheInternalKey(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)

	for name, res := range map[string]response{
		"no key": h.doInternalWithBody(http.MethodPut,
			"/internal/requests/"+requestID+"/offers/count", "", `{"totalOffers":9}`),
		"user jwt": h.do(http.MethodPut,
			"/internal/requests/"+requestID+"/offers/count", owner, `{"totalOffers":9}`),
	} {
		if res.code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401; body %s", name, res.code, res.raw)
		}
	}

	after := h.do(http.MethodGet, "/api/requests/"+requestID, "", "")
	if got := num(t, after.body, "totalOffers"); got != 0 {
		t.Errorf("totalOffers = %d, want 0 - nothing should have been recorded", got)
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

// --- the lifecycle: OPEN while anyone wants the item, INACTIVE when nobody does -------

// A request does not end, it empties. The status follows the participant count rather
// than any call, so there is nothing to set and nothing that can disagree with it.
func TestARequestGoesInactiveWhenTheLastParticipantLeaves(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)

	if res := h.do(http.MethodDelete, "/api/requests/"+requestID+"/participants/me", owner, ""); res.code != http.StatusNoContent {
		t.Fatalf("leave: status %d body %s", res.code, res.raw)
	}

	res := h.do(http.MethodGet, "/api/requests/"+requestID, owner, "")
	if got := res.body["status"]; got != StatusInactive {
		t.Errorf("status = %v, want INACTIVE", got)
	}
	if got := res.body["totalCustomers"]; got != float64(0) {
		t.Errorf("totalCustomers = %v, want 0", got)
	}
	if got := res.body["totalQuantity"]; got != float64(0) {
		t.Errorf("totalQuantity = %v, want 0", got)
	}
}

// The owner is a participant like any other. Their leaving empties the request only
// because they were the last one on it, not because it was theirs.
func TestARequestStaysOpenWhileAnyoneIsStillOnIt(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	_, joiner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)
	if res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":5}`); res.code != http.StatusCreated {
		t.Fatalf("join: status %d body %s", res.code, res.raw)
	}

	if res := h.do(http.MethodDelete, "/api/requests/"+requestID+"/participants/me", owner, ""); res.code != http.StatusNoContent {
		t.Fatalf("owner leaves: status %d body %s", res.code, res.raw)
	}

	res := h.do(http.MethodGet, "/api/requests/"+requestID, joiner, "")
	if got := res.body["status"]; got != StatusOpen {
		t.Errorf("status = %v, want OPEN - the joiner still wants the item", got)
	}
	if got := res.body["totalQuantity"]; got != float64(5) {
		t.Errorf("totalQuantity = %v, want 5", got)
	}
}

// INACTIVE is not terminal. Refusing a join here would make whoever left last the
// person who ended the request for everyone after them.
func TestJoiningAnInactiveRequestOpensItAgain(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	_, joiner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)
	h.do(http.MethodDelete, "/api/requests/"+requestID+"/participants/me", owner, "")

	res := h.do(http.MethodPost, "/api/requests/"+requestID+"/participants", joiner, `{"quantity":2}`)
	if res.code != http.StatusCreated {
		t.Fatalf("joining an inactive request: status %d body %s", res.code, res.raw)
	}
	if got := res.body["status"]; got != StatusOpen {
		t.Errorf("status = %v, want OPEN again", got)
	}
	if got := res.body["totalQuantity"]; got != float64(2) {
		t.Errorf("totalQuantity = %v, want 2", got)
	}
}

// Emptying a request is not an event. Nobody is left on it to be told, and the customer
// who left already knows.
func TestEmptyingARequestWritesNoEvent(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)
	before := len(h.outbox())

	h.do(http.MethodDelete, "/api/requests/"+requestID+"/participants/me", owner, "")

	if after := len(h.outbox()); after != before {
		t.Errorf("wrote %d events, want none", after-before)
	}
}

// Both of these ended a request from outside its participants. Neither exists: the
// owner has no power to close, and Admin/Contact decides offers rather than demand.
func TestThereIsNoWayToEndARequestFromOutside(t *testing.T) {
	h := newHarness(t)
	_, owner := h.newCustomer()
	requestID := h.createRequest(owner, "Espresso Machine", 3)

	if res := h.do(http.MethodPost, "/api/requests/"+requestID+"/close", owner, ""); res.code != http.StatusNotFound {
		t.Errorf("POST /close: status %d, want 404 - the route is gone", res.code)
	}
	res := h.doInternalWithBody(http.MethodPatch, "/internal/requests/"+requestID+"/status",
		testInternalAPIKey, `{"status":"CLOSED"}`)
	if res.code != http.StatusNotFound {
		t.Errorf("PATCH /internal status: status %d, want 404 - the route is gone", res.code)
	}

	if got := h.do(http.MethodGet, "/api/requests/"+requestID, owner, "").body["status"]; got != StatusOpen {
		t.Errorf("status = %v, want it untouched at OPEN", got)
	}
}
