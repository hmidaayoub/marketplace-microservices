package admin

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/events"
)

// --- flow 3: the admin decides ------------------------------------------------------

// R7 + R8 together: approving records the decision AND creates one contact permission
// per customer, rather than exposing anything by itself.
func TestApproveRecordsDecisionAndGrantsOnePermissionPerParticipant(t *testing.T) {
	h := newHarness(t)
	adminID, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	first, second := h.newCustomer("+21611111111"), h.newCustomer("+21622222222")
	offerID, requestID := h.newOffer(sellerID, first, second)

	res := h.approve(adminToken, offerID)
	if res.code != http.StatusCreated {
		t.Fatalf("approve: status %d body %s", res.code, res.raw)
	}

	if got := str(t, res.body, "decision"); got != DecisionApproved {
		t.Errorf("decision = %q, want APPROVED", got)
	}
	if got := str(t, res.body, "adminUserId"); got != adminID.String() {
		t.Errorf("adminUserId = %q, want the token subject %q", got, adminID)
	}
	if got := num(t, res.body, "contactsGranted"); got != 2 {
		t.Errorf("contactsGranted = %d, want 2", got)
	}

	// The status change reached offer-service, which owns offer state.
	if got := h.offerStatus(offerID); got != DecisionApproved {
		t.Errorf("offer status at offer-service = %q, want APPROVED", got)
	}

	// And the grants are real rows, keyed to this seller, request and offer.
	if got := countRows(t, "contact_access"); got != 2 {
		t.Errorf("contact_access rows = %d, want 2", got)
	}
	list := h.do(http.MethodGet, "/api/admin/contact-access?requestId="+requestID.String(), adminToken, "")
	if len(list.list) != 2 {
		t.Fatalf("contact-access listing returned %d rows: %s", len(list.list), list.raw)
	}
	for _, row := range list.list {
		if row["sellerId"] != sellerID.String() {
			t.Errorf("grant sellerId = %v, want %s", row["sellerId"], sellerID)
		}
		if row["offerId"] != offerID.String() {
			t.Errorf("grant offerId = %v, want %s", row["offerId"], offerID)
		}
		if row["status"] != statusGranted {
			t.Errorf("grant status = %v, want GRANTED", row["status"])
		}
	}
}

// R8 read the other way: a rejection is recorded but exposes nobody.
func TestRejectRecordsDecisionAndGrantsNothing(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	offerID, _ := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	res := h.do(http.MethodPost, "/api/admin/offers/"+offerID.String()+"/reject", adminToken, `{"reason":"price too high"}`)
	if res.code != http.StatusCreated {
		t.Fatalf("reject: status %d body %s", res.code, res.raw)
	}
	if got := str(t, res.body, "decision"); got != DecisionRejected {
		t.Errorf("decision = %q, want REJECTED", got)
	}
	if got := num(t, res.body, "contactsGranted"); got != 0 {
		t.Errorf("contactsGranted = %d, want 0 - a rejection grants nothing", got)
	}
	if got := countRows(t, "contact_access"); got != 0 {
		t.Errorf("contact_access rows = %d, want 0", got)
	}
	if got := h.offerStatus(offerID); got != DecisionRejected {
		t.Errorf("offer status = %q, want REJECTED", got)
	}
}

// R7: one decision per offer. The second is refused rather than overwriting the audit
// record of the first.
func TestSecondDecisionOnSameOfferIsRejected(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	offerID, _ := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	if res := h.approve(adminToken, offerID); res.code != http.StatusCreated {
		t.Fatalf("first approve: status %d body %s", res.code, res.raw)
	}

	res := h.do(http.MethodPost, "/api/admin/offers/"+offerID.String()+"/reject", adminToken, "")
	if res.code != http.StatusConflict {
		t.Fatalf("second decision: status %d, want 409; body %s", res.code, res.raw)
	}
	if got := countRows(t, "offer_decision"); got != 1 {
		t.Errorf("offer_decision rows = %d, want 1", got)
	}
	if got := h.offerStatus(offerID); got != DecisionApproved {
		t.Errorf("offer status = %q, want the original APPROVED", got)
	}
}

// A dangling request used to be reported as "Offer not found" - about the offer the
// admin was looking straight at. The offer is fine; what it points at is not.
func TestApprovingAnOfferWhoseRequestIsGoneSaysSo(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	offerID, requestID := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	// The request disappears out from under the offer.
	h.p.mu.Lock()
	delete(h.p.participant, requestID)
	h.p.mu.Unlock()

	res := h.approve(adminToken, offerID)
	if res.code != http.StatusConflict {
		t.Fatalf("approve with no request: status %d, want 409; body %s", res.code, res.raw)
	}
	if msg := str(t, res.body, "message"); !strings.Contains(msg, "no longer exists") {
		t.Errorf("message = %q, want it to name the request, not the offer", msg)
	}
	if got := countRows(t, "offer_decision"); got != 0 {
		t.Errorf("offer_decision rows = %d, want 0 - nothing was decided", got)
	}
	if got := countRows(t, "contact_access"); got != 0 {
		t.Errorf("contact_access rows = %d, want 0", got)
	}

	// Still clearable: a rejection never needs the participants.
	rej := h.do(http.MethodPost, "/api/admin/offers/"+offerID.String()+"/reject", adminToken, `{"reason":"request withdrawn"}`)
	if rej.code != http.StatusCreated {
		t.Fatalf("reject with no request: status %d, want 201; body %s", rej.code, rej.raw)
	}
}

// Every participant can leave - the creator is added as one and has no exemption - and
// approving what is left would grant nobody while telling the seller they were granted.
func TestApprovingARequestWithNoBuyersLeftIsRefused(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	offerID, requestID := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	h.p.mu.Lock()
	h.p.participant[requestID] = nil // the request stands; everyone has left it
	h.p.mu.Unlock()

	res := h.approve(adminToken, offerID)
	if res.code != http.StatusConflict {
		t.Fatalf("approve with no buyers: status %d, want 409; body %s", res.code, res.raw)
	}
	if msg := str(t, res.body, "message"); !strings.Contains(msg, "no buyers") {
		t.Errorf("message = %q, want it to say the request has no buyers", msg)
	}
	if got := countRows(t, "offer_decision"); got != 0 {
		t.Errorf("offer_decision rows = %d, want 0 - an approval granting nobody is not recorded", got)
	}
	if got := h.offerStatus(offerID); got != statusPending {
		t.Errorf("offer status = %q, want it left PENDING", got)
	}
}

func TestDecidingAnOfferThatIsNotPendingIsRejected(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	offerID, _ := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	h.p.mu.Lock()
	h.p.offers[offerID].Status = "CANCELLED"
	h.p.mu.Unlock()

	res := h.approve(adminToken, offerID)
	if res.code != http.StatusConflict {
		t.Fatalf("status %d, want 409; body %s", res.code, res.raw)
	}
	if got := countRows(t, "offer_decision"); got != 0 {
		t.Errorf("offer_decision rows = %d, want 0", got)
	}
}

func TestDecidingAnUnknownOfferIs404(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()

	res := h.approve(adminToken, uuid.New())
	if res.code != http.StatusNotFound {
		t.Fatalf("status %d, want 404; body %s", res.code, res.raw)
	}
}

// The decision and its grants are one unit: if offer-service refuses the relay, the
// local rows must not survive, or the platform would hold an audit record and live
// contact permissions for an offer that is still PENDING.
func TestFailedStatusRelayRollsBackDecisionAndGrants(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	offerID, _ := h.newOffer(sellerID, h.newCustomer("+21611111111"), h.newCustomer("+21622222222"))

	h.p.mu.Lock()
	h.p.failStatusPatch = true
	h.p.mu.Unlock()

	res := h.approve(adminToken, offerID)
	if res.code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503; body %s", res.code, res.raw)
	}

	if got := countRows(t, "offer_decision"); got != 0 {
		t.Errorf("offer_decision rows = %d, want 0 after rollback", got)
	}
	if got := countRows(t, "contact_access"); got != 0 {
		t.Errorf("contact_access rows = %d, want 0 after rollback", got)
	}
	if got := h.offerStatus(offerID); got != statusPending {
		t.Errorf("offer status = %q, want it left PENDING", got)
	}

	// And the same offer can still be decided once the dependency recovers.
	h.p.mu.Lock()
	h.p.failStatusPatch = false
	h.p.mu.Unlock()
	if res := h.approve(adminToken, offerID); res.code != http.StatusCreated {
		t.Fatalf("retry after recovery: status %d body %s", res.code, res.raw)
	}
}

func TestPendingQueueIsServedFromOfferService(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	h.newOffer(sellerID, h.newCustomer("+21611111111"))
	decided, _ := h.newOffer(sellerID, h.newCustomer("+21622222222"))

	if res := h.approve(adminToken, decided); res.code != http.StatusCreated {
		t.Fatalf("approve: status %d body %s", res.code, res.raw)
	}

	res := h.do(http.MethodGet, "/api/admin/offers/pending", adminToken, "")
	if res.code != http.StatusOK {
		t.Fatalf("status %d body %s", res.code, res.raw)
	}
	if len(res.list) != 1 {
		t.Fatalf("pending queue has %d offers, want 1 (the decided one must drop out): %s", len(res.list), res.raw)
	}
}

// --- flow 4: the seller reads contacts ----------------------------------------------

// R9: the phone numbers arrive only after a grant exists, and only for the customers
// on that request.
func TestSellerReadsContactsOnlyForGrantedRequest(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, sellerToken := h.newSeller()
	first, second := h.newCustomer("+21611111111"), h.newCustomer("+21622222222")
	offerID, requestID := h.newOffer(sellerID, first, second)

	// Before any decision the seller may not see a thing.
	before := h.do(http.MethodGet, "/api/contacts/requests/"+requestID.String(), sellerToken, "")
	if before.code != http.StatusForbidden {
		t.Fatalf("before approval: status %d, want 403; body %s", before.code, before.raw)
	}
	h.p.mu.Lock()
	calls := h.p.phoneCalls
	h.p.mu.Unlock()
	if calls != 0 {
		t.Errorf("auth-service was asked for %d phone numbers before any grant existed, want 0", calls)
	}

	if res := h.approve(adminToken, offerID); res.code != http.StatusCreated {
		t.Fatalf("approve: status %d body %s", res.code, res.raw)
	}

	after := h.do(http.MethodGet, "/api/contacts/requests/"+requestID.String(), sellerToken, "")
	if after.code != http.StatusOK {
		t.Fatalf("after approval: status %d body %s", after.code, after.raw)
	}

	contacts, ok := after.body["contacts"].([]any)
	if !ok || len(contacts) != 2 {
		t.Fatalf("want 2 contacts, got %s", after.raw)
	}
	got := map[string]string{}
	for _, entry := range contacts {
		row := entry.(map[string]any)
		got[row["customerId"].(string)] = row["phoneNumber"].(string)
	}
	if got[first.String()] != "+21611111111" || got[second.String()] != "+21622222222" {
		t.Errorf("contacts = %v, want both customers with their own numbers", got)
	}
}

// A grant is per seller: approving one seller's offer tells another seller nothing.
func TestGrantDoesNotLeakToAnotherSeller(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, grantedSeller, _ := h.newSeller()
	_, _, rivalToken := h.newSeller()
	offerID, requestID := h.newOffer(grantedSeller, h.newCustomer("+21611111111"))

	if res := h.approve(adminToken, offerID); res.code != http.StatusCreated {
		t.Fatalf("approve: status %d body %s", res.code, res.raw)
	}

	res := h.do(http.MethodGet, "/api/contacts/requests/"+requestID.String(), rivalToken, "")
	if res.code != http.StatusForbidden {
		t.Fatalf("rival seller: status %d, want 403; body %s", res.code, res.raw)
	}
}

// Revoking takes effect immediately and keeps the row for the audit history.
func TestRevokingAccessStopsTheSellerReadingContacts(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, sellerToken := h.newSeller()
	offerID, requestID := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	if res := h.approve(adminToken, offerID); res.code != http.StatusCreated {
		t.Fatalf("approve: status %d body %s", res.code, res.raw)
	}

	list := h.do(http.MethodGet, "/api/admin/contact-access", adminToken, "")
	if len(list.list) != 1 {
		t.Fatalf("want 1 grant, got %s", list.raw)
	}
	accessID := list.list[0]["accessId"].(string)

	revoked := h.do(http.MethodDelete, "/api/admin/contact-access/"+accessID, adminToken, "")
	if revoked.code != http.StatusOK {
		t.Fatalf("revoke: status %d body %s", revoked.code, revoked.raw)
	}
	if got := str(t, revoked.body, "status"); got != "REVOKED" {
		t.Errorf("status = %q, want REVOKED", got)
	}

	// The row survives - revoking is an audit event, not an erasure.
	if got := countRows(t, "contact_access"); got != 1 {
		t.Errorf("contact_access rows = %d, want the revoked row kept", got)
	}

	after := h.do(http.MethodGet, "/api/contacts/requests/"+requestID.String(), sellerToken, "")
	if after.code != http.StatusForbidden {
		t.Fatalf("after revoke: status %d, want 403; body %s", after.code, after.raw)
	}

	// Revoking twice is a conflict, not a silent success.
	if again := h.do(http.MethodDelete, "/api/admin/contact-access/"+accessID, adminToken, ""); again.code != http.StatusConflict {
		t.Errorf("second revoke: status %d, want 409; body %s", again.code, again.raw)
	}
}

func TestRevokingUnknownAccessIs404(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()

	res := h.do(http.MethodDelete, "/api/admin/contact-access/"+uuid.New().String(), adminToken, "")
	if res.code != http.StatusNotFound {
		t.Fatalf("status %d, want 404; body %s", res.code, res.raw)
	}
}

func TestSellerWithoutAProfileCannotReadContacts(t *testing.T) {
	h := newHarness(t)
	// A SELLER token whose userId seller-service does not know.
	token := h.token(uuid.New(), "SELLER")

	res := h.do(http.MethodGet, "/api/contacts/requests/"+uuid.New().String(), token, "")
	if res.code != http.StatusForbidden {
		t.Fatalf("status %d, want 403; body %s", res.code, res.raw)
	}
}

// --- internal permission check -------------------------------------------------------

func TestInternalContactAccessCheck(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	customerID := h.newCustomer("+21611111111")
	offerID, requestID := h.newOffer(sellerID, customerID)

	path := func(seller, customer uuid.UUID) string {
		return "/internal/contact-access?sellerId=" + seller.String() + "&customerId=" + customer.String()
	}

	before := h.doInternal(http.MethodGet, path(sellerID, customerID), testInternalAPIKey)
	if before.code != http.StatusOK || before.body["allowed"] != false {
		t.Fatalf("before approval: status %d body %s", before.code, before.raw)
	}

	if res := h.approve(adminToken, offerID); res.code != http.StatusCreated {
		t.Fatalf("approve: status %d body %s", res.code, res.raw)
	}

	after := h.doInternal(http.MethodGet, path(sellerID, customerID), testInternalAPIKey)
	if after.code != http.StatusOK || after.body["allowed"] != true {
		t.Fatalf("after approval: status %d body %s", after.code, after.raw)
	}

	// Scoped to the right request, and negative for one the grant does not cover.
	scoped := h.doInternal(http.MethodGet, path(sellerID, customerID)+"&requestId="+requestID.String(), testInternalAPIKey)
	if scoped.body["allowed"] != true {
		t.Errorf("scoped to the granted request: %s", scoped.raw)
	}
	other := h.doInternal(http.MethodGet, path(sellerID, customerID)+"&requestId="+uuid.New().String(), testInternalAPIKey)
	if other.body["allowed"] != false {
		t.Errorf("scoped to an unrelated request should be false: %s", other.raw)
	}

	// An unrelated seller is never allowed.
	stranger := h.doInternal(http.MethodGet, path(uuid.New(), customerID), testInternalAPIKey)
	if stranger.body["allowed"] != false {
		t.Errorf("unrelated seller should be false: %s", stranger.raw)
	}
}

// --- the security boundary -----------------------------------------------------------

// R7 at the edge: only an ADMIN reaches the decision routes.
func TestAdminRoutesRejectEveryOtherRole(t *testing.T) {
	h := newHarness(t)
	_, sellerID, sellerToken := h.newSeller()
	offerID, _ := h.newOffer(sellerID, h.newCustomer("+21611111111"))
	customerToken := h.token(uuid.New(), "CUSTOMER")

	for _, tc := range []struct {
		name, method, path string
	}{
		{"pending queue", http.MethodGet, "/api/admin/offers/pending"},
		{"approve", http.MethodPost, "/api/admin/offers/" + offerID.String() + "/approve"},
		{"reject", http.MethodPost, "/api/admin/offers/" + offerID.String() + "/reject"},
		{"list access", http.MethodGet, "/api/admin/contact-access"},
		{"revoke", http.MethodDelete, "/api/admin/contact-access/" + uuid.New().String()},
	} {
		for role, token := range map[string]string{"SELLER": sellerToken, "CUSTOMER": customerToken} {
			res := h.do(tc.method, tc.path, token, "")
			if res.code != http.StatusForbidden {
				t.Errorf("%s as %s: status %d, want 403; body %s", tc.name, role, res.code, res.raw)
			}
		}

		// And no token at all is a 401, not a 403.
		if res := h.do(tc.method, tc.path, "", ""); res.code != http.StatusUnauthorized {
			t.Errorf("%s with no token: status %d, want 401", tc.name, res.code)
		}
	}

	if got := countRows(t, "offer_decision"); got != 0 {
		t.Errorf("offer_decision rows = %d, want 0 - no rejected caller may write one", got)
	}
}

func TestContactRouteIsSellerOnly(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	path := "/api/contacts/requests/" + uuid.New().String()

	for role, token := range map[string]string{
		"ADMIN":    adminToken,
		"CUSTOMER": h.token(uuid.New(), "CUSTOMER"),
	} {
		if res := h.do(http.MethodGet, path, token, ""); res.code != http.StatusForbidden {
			t.Errorf("as %s: status %d, want 403; body %s", role, res.code, res.raw)
		}
	}
	if res := h.do(http.MethodGet, path, "", ""); res.code != http.StatusUnauthorized {
		t.Errorf("with no token: status %d, want 401", res.code)
	}
}

func TestInternalRouteRequiresTheSharedKey(t *testing.T) {
	h := newHarness(t)
	path := "/internal/contact-access?sellerId=" + uuid.New().String() + "&customerId=" + uuid.New().String()

	if res := h.doInternal(http.MethodGet, path, ""); res.code != http.StatusUnauthorized {
		t.Errorf("no key: status %d, want 401", res.code)
	}
	if res := h.doInternal(http.MethodGet, path, "wrong-key"); res.code != http.StatusUnauthorized {
		t.Errorf("wrong key: status %d, want 401", res.code)
	}

	// A user JWT is not a substitute for the internal key.
	_, adminToken := h.adminToken()
	if res := h.do(http.MethodGet, path, adminToken, ""); res.code != http.StatusUnauthorized {
		t.Errorf("user JWT on an internal route: status %d, want 401", res.code)
	}
}

// Outbound calls must carry the shared key, or every dependency would refuse them.
func TestOutboundCallsCarryTheInternalKey(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, sellerToken := h.newSeller()
	offerID, requestID := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	if res := h.approve(adminToken, offerID); res.code != http.StatusCreated {
		t.Fatalf("approve: status %d body %s", res.code, res.raw)
	}
	if res := h.do(http.MethodGet, "/api/contacts/requests/"+requestID.String(), sellerToken, ""); res.code != http.StatusOK {
		t.Fatalf("contacts: status %d body %s", res.code, res.raw)
	}

	h.p.mu.Lock()
	defer h.p.mu.Unlock()
	for _, dependency := range []string{"offer", "request", "seller", "customer", "auth"} {
		if h.p.keys[dependency] != testInternalAPIKey {
			t.Errorf("%s-service was called with key %q, want the shared key", dependency, h.p.keys[dependency])
		}
	}
}

// --- input validation ---------------------------------------------------------------

// Issue #28: a malformed id is a 400 from every service in the platform, not a 500.
func TestMalformedIdsAreRejectedWith400(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, _, sellerToken := h.newSeller()

	for _, tc := range []struct {
		name, method, path, token string
	}{
		{"approve", http.MethodPost, "/api/admin/offers/not-a-uuid/approve", adminToken},
		{"reject", http.MethodPost, "/api/admin/offers/not-a-uuid/reject", adminToken},
		{"revoke", http.MethodDelete, "/api/admin/contact-access/not-a-uuid", adminToken},
		{"contacts", http.MethodGet, "/api/contacts/requests/not-a-uuid", sellerToken},
		{"access filter", http.MethodGet, "/api/admin/contact-access?sellerId=not-a-uuid", adminToken},
	} {
		res := h.do(tc.method, tc.path, tc.token, "")
		if res.code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400; body %s", tc.name, res.code, res.raw)
		}
	}

	// The internal check validates its query parameters the same way.
	bad := h.doInternal(http.MethodGet, "/internal/contact-access?sellerId=nope&customerId="+uuid.New().String(), testInternalAPIKey)
	if bad.code != http.StatusBadRequest {
		t.Errorf("internal check with a bad sellerId: status %d, want 400", bad.code)
	}
}

func TestDecisionBodyIsOptionalButValidated(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	offerID, _ := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	// No body at all is fine: approving without an explanation is normal.
	if res := h.do(http.MethodPost, "/api/admin/offers/"+offerID.String()+"/approve", adminToken, ""); res.code != http.StatusCreated {
		t.Fatalf("approve with no body: status %d body %s", res.code, res.raw)
	}

	// But a body that smuggles in a field the service owns is rejected outright,
	// rather than silently ignored.
	other, _ := h.newOffer(sellerID, h.newCustomer("+21622222222"))
	res := h.do(http.MethodPost, "/api/admin/offers/"+other.String()+"/approve", adminToken,
		`{"reason":"ok","decision":"REJECTED"}`)
	if res.code != http.StatusBadRequest {
		t.Errorf("caller-supplied decision: status %d, want 400; body %s", res.code, res.raw)
	}
}

func TestHealthReportsUp(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/health", "/actuator/health"} {
		res := h.do(http.MethodGet, path, "", "")
		if res.code != http.StatusOK || res.body["status"] != "UP" {
			t.Errorf("%s: status %d body %s", path, res.code, res.raw)
		}
	}
}

// --- notification events (spec flow 3 step 7, docs/events.md) -------------------------

func TestApprovalWritesBothTheOutcomeAndTheContactGrant(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	sellerUserID, sellerID, _ := h.newSeller()
	offerID, _ := h.newOffer(sellerID, h.newCustomer("+21611111111"), h.newCustomer("+21622222222"))

	if res := h.approve(adminToken, offerID); res.code != http.StatusCreated {
		t.Fatalf("approve: status %d body %s", res.code, res.raw)
	}

	// Two events, not one: R8 is precisely that approval and contact permission are
	// separate facts, so they are separate messages.
	want := []string{events.KeyOfferApproved, events.KeyContactAccessGranted}
	if got := h.keys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("routing keys = %v, want %v", got, want)
	}

	outbox := h.outbox()
	for _, e := range outbox {
		n := e.notifications[0]
		// Addressed by userId: the service holds a sellerId and makes the hop itself,
		// because notification-service never resolves an identity.
		if n.UserID != sellerUserID {
			t.Errorf("%s addressed to %s, want the seller's userId %s", e.routingKey, n.UserID, sellerUserID)
		}
		// Written by the transaction, not yet sent: the relay owns publishing.
		if e.publishedAt != nil {
			t.Errorf("%s is already marked published", e.routingKey)
		}
	}
	if outbox[0].notifications[0].Type != "OFFER_APPROVED" {
		t.Errorf("type = %q, want OFFER_APPROVED", outbox[0].notifications[0].Type)
	}
	if outbox[1].notifications[0].Type != "CONTACT_ACCESS_GRANTED" {
		t.Errorf("type = %q, want CONTACT_ACCESS_GRANTED", outbox[1].notifications[0].Type)
	}
	if !strings.Contains(outbox[1].notifications[0].Message, "2 customer") {
		t.Errorf("grant message does not carry the count: %q", outbox[1].notifications[0].Message)
	}
	if !strings.Contains(outbox[0].notifications[0].Message, "looks good") {
		t.Errorf("outcome message does not carry the admin's reason: %q", outbox[0].notifications[0].Message)
	}
}

func TestRejectionWritesOnlyTheOutcome(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	sellerUserID, sellerID, _ := h.newSeller()
	offerID, _ := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	res := h.do(http.MethodPost, "/api/admin/offers/"+offerID.String()+"/reject", adminToken,
		`{"reason":"price too high"}`)
	if res.code != http.StatusCreated {
		t.Fatalf("reject: status %d body %s", res.code, res.raw)
	}

	// A rejection grants nothing, so there is no contact-access event to send.
	if got := h.keys(); !reflect.DeepEqual(got, []string{events.KeyOfferRejected}) {
		t.Fatalf("routing keys = %v, want just %q", got, events.KeyOfferRejected)
	}
	n := h.outbox()[0].notifications[0]
	if n.UserID != sellerUserID || n.Type != "OFFER_REJECTED" {
		t.Errorf("notification = %+v, want OFFER_REJECTED to %s", n, sellerUserID)
	}
}

// The whole point of the outbox: the decision and the promise to announce it are one
// write, so a failed decision cannot leave an event behind.
func TestAFailedDecisionWritesNoEvent(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	offerID, _ := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	h.approve(adminToken, offerID)
	before := len(h.outbox())

	h.approve(adminToken, offerID)    // already decided
	h.approve(adminToken, uuid.New()) // unknown offer

	if after := len(h.outbox()); after != before {
		t.Errorf("failed decisions wrote %d events, want none", after-before)
	}
}

// And the reverse: a rolled-back decision must not leave an event either.
func TestARolledBackDecisionWritesNoEvent(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	offerID, _ := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	h.p.mu.Lock()
	h.p.failStatusPatch = true
	h.p.mu.Unlock()

	if res := h.approve(adminToken, offerID); res.code != http.StatusServiceUnavailable {
		t.Fatalf("approve: status %d body %s", res.code, res.raw)
	}

	if got := len(h.outbox()); got != 0 {
		t.Errorf("outbox holds %d events after a rollback, want 0", got)
	}
	if got := countRows(t, "offer_decision"); got != 0 {
		t.Errorf("offer_decision rows = %d, want 0", got)
	}
}

// A decision that committed must stand even when the seller cannot be addressed.
func TestADecisionSurvivesAnUnaddressableSeller(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	offerID, _ := h.newOffer(uuid.New(), h.newCustomer("+21611111111")) // seller unknown to the stub

	res := h.approve(adminToken, offerID)
	if res.code != http.StatusCreated {
		t.Fatalf("approve: status %d body %s", res.code, res.raw)
	}
	if got := countRows(t, "offer_decision"); got != 1 {
		t.Errorf("offer_decision rows = %d, want the decision kept", got)
	}
	if got := h.keys(); len(got) != 0 {
		t.Errorf("wrote %v, want nothing when the recipient cannot be resolved", got)
	}
}

// The relay is nudged on commit so a notification is not held for the poll interval.
func TestCommittingADecisionWakesTheRelay(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	offerID, _ := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	h.approve(adminToken, offerID)

	h.waker.mu.Lock()
	defer h.waker.mu.Unlock()
	if h.waker.wakes == 0 {
		t.Error("the relay was never woken, so the event waits for the next tick")
	}
}

// Offer-service already refuses new offers against an OFFER_APPROVED request; until
// something made that transition the guard could never fire.
func TestApprovingAnOfferMarksTheRequestApproved(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	offerID, requestID := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	if res := h.approve(adminToken, offerID); res.code != http.StatusCreated {
		t.Fatalf("approve: status %d body %s", res.code, res.raw)
	}

	h.p.mu.Lock()
	defer h.p.mu.Unlock()
	if got := h.p.requestStatus[requestID]; got != "OFFER_APPROVED" {
		t.Errorf("request-service was told %q, want OFFER_APPROVED", got)
	}
}

func TestRejectingAnOfferLeavesTheRequestAlone(t *testing.T) {
	h := newHarness(t)
	_, adminToken := h.adminToken()
	_, sellerID, _ := h.newSeller()
	offerID, requestID := h.newOffer(sellerID, h.newCustomer("+21611111111"))

	res := h.do(http.MethodPost, "/api/admin/offers/"+offerID.String()+"/reject", adminToken, "")
	if res.code != http.StatusCreated {
		t.Fatalf("reject: status %d body %s", res.code, res.raw)
	}

	// The demand is still live: another seller may still win it.
	h.p.mu.Lock()
	defer h.p.mu.Unlock()
	if got, ok := h.p.requestStatus[requestID]; ok {
		t.Errorf("request-service was told %q; a rejection changes nothing", got)
	}
}
