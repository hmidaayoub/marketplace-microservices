package admin

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/clients"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/store"
)

// Field names are camelCase to match the JSON the other services already emit.

// decisionBody is the optional body of an approve/reject call. The decision itself is
// taken from the route, and the admin from the token - only the reason is the caller's
// to supply, so there is nothing here a client could use to decide on someone's behalf.
type decisionBody struct {
	Reason string `json:"reason"`
}

type decisionResponse struct {
	DecisionID  uuid.UUID `json:"decisionId"`
	OfferID     uuid.UUID `json:"offerId"`
	AdminUserID uuid.UUID `json:"adminUserId"`
	Decision    string    `json:"decision"`
	Reason      string    `json:"reason"`
	DecidedAt   time.Time `json:"decidedAt"`

	// How many customers the approval exposed to the seller. Zero for a rejection,
	// which grants nothing (R8).
	ContactsGranted int `json:"contactsGranted"`
}

type contactAccessResponse struct {
	AccessID   uuid.UUID  `json:"accessId"`
	SellerID   uuid.UUID  `json:"sellerId"`
	CustomerID uuid.UUID  `json:"customerId"`
	RequestID  uuid.UUID  `json:"requestId"`
	OfferID    uuid.UUID  `json:"offerId"`
	Status     string     `json:"status"`
	GrantedBy  uuid.UUID  `json:"grantedBy"`
	GrantedAt  time.Time  `json:"grantedAt"`
	ExpiresAt  *time.Time `json:"expiresAt"`
}

// contact is what a seller receives once permission exists: the customer they may
// reach and the number to reach them on, and nothing else about that person. The
// number is fetched from auth-service per call and never stored here (R10).
type contact struct {
	CustomerID  uuid.UUID `json:"customerId"`
	PhoneNumber string    `json:"phoneNumber"`
}

type contactsResponse struct {
	RequestID uuid.UUID `json:"requestId"`
	Contacts  []contact `json:"contacts"`
}

// accessCheckResponse answers the internal permission question with a bare boolean.
// Callers get the verdict, not the grant records behind it.
type accessCheckResponse struct {
	SellerID   uuid.UUID `json:"sellerId"`
	CustomerID uuid.UUID `json:"customerId"`
	Allowed    bool      `json:"allowed"`
}

// pendingOfferResponse mirrors offer-service's OfferOut. Admin/Contact does not store
// offers, so the review queue is passed through from the service that owns them.
type pendingOfferResponse struct {
	OfferID           uuid.UUID `json:"offerId"`
	RequestID         uuid.UUID `json:"requestId"`
	SellerID          uuid.UUID `json:"sellerId"`
	AvailableQuantity int64     `json:"availableQuantity"`
	PricePerUnit      string    `json:"pricePerUnit"`
	Currency          string    `json:"currency"`
	Description       string    `json:"description"`
	Status            string    `json:"status"`
}

func toDecisionResponse(d store.OfferDecision, contactsGranted int) decisionResponse {
	return decisionResponse{
		DecisionID:      d.DecisionID,
		OfferID:         d.OfferID,
		AdminUserID:     d.AdminUserID,
		Decision:        d.Decision,
		Reason:          d.Reason,
		DecidedAt:       d.DecidedAt,
		ContactsGranted: contactsGranted,
	}
}

func toContactAccessResponse(a store.ContactAccess) contactAccessResponse {
	out := contactAccessResponse{
		AccessID:   a.AccessID,
		SellerID:   a.SellerID,
		CustomerID: a.CustomerID,
		RequestID:  a.RequestID,
		OfferID:    a.OfferID,
		Status:     a.Status,
		GrantedBy:  a.GrantedBy,
		GrantedAt:  a.GrantedAt,
	}
	if a.ExpiresAt.Valid {
		expires := a.ExpiresAt.Time
		out.ExpiresAt = &expires
	}
	return out
}

func toContactAccessResponses(as []store.ContactAccess) []contactAccessResponse {
	out := make([]contactAccessResponse, 0, len(as))
	for _, a := range as {
		out = append(out, toContactAccessResponse(a))
	}
	return out
}

func toPendingOfferResponses(offers []clients.Offer) []pendingOfferResponse {
	out := make([]pendingOfferResponse, 0, len(offers))
	for _, o := range offers {
		out = append(out, pendingOfferResponse{
			OfferID:           o.OfferID,
			RequestID:         o.RequestID,
			SellerID:          o.SellerID,
			AvailableQuantity: o.AvailableQuantity,
			PricePerUnit:      o.PricePerUnit,
			Currency:          o.Currency,
			Description:       o.Description,
			Status:            o.Status,
		})
	}
	return out
}

func (b *decisionBody) validate() string {
	b.Reason = strings.TrimSpace(b.Reason)
	if len(b.Reason) > 1000 {
		return "reason must be at most 1000 characters"
	}
	return ""
}
