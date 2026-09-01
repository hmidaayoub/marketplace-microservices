package clients

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// Offer is the part of offer-service's OfferOut this service acts on. Deciding an
// offer needs the sellerId and requestId behind it: the grant that approval produces
// links seller, customer, request and offer together (spec section 5).
type Offer struct {
	OfferID           uuid.UUID `json:"offerId"`
	RequestID         uuid.UUID `json:"requestId"`
	SellerID          uuid.UUID `json:"sellerId"`
	AvailableQuantity int64     `json:"availableQuantity"`
	PricePerUnit      string    `json:"pricePerUnit"`
	Currency          string    `json:"currency"`
	Description       string    `json:"description"`
	Status            string    `json:"status"`

	// Whether the offer carries a picture of the product. Only the flag travels: the
	// bytes stay in offer-service, and the admin's browser fetches them from there with
	// the admin's own token, so a queue of twenty offers is twenty small JSON rows here
	// rather than twenty megabytes through this service.
	HasImage  bool   `json:"hasImage"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type OfferClient struct{ t transport }

func NewOffer(baseURL, apiKey string, httpClient *http.Client) *OfferClient {
	return &OfferClient{t: transport{baseURL: baseURL, apiKey: apiKey, name: "offer-service", http: httpClient}}
}

// Get reads one offer. Called before a decision is written so the terms an admin acts
// on are the terms offer-service actually holds, not what the caller claims.
func (c *OfferClient) Get(ctx context.Context, offerID uuid.UUID) (Offer, error) {
	var out Offer
	err := c.t.do(ctx, http.MethodGet, "/internal/offers/"+offerID.String(), nil, &out)
	return out, err
}

// ListPending reads the admin review queue straight from the service that owns offer
// state, so the queue cannot drift from it.
func (c *OfferClient) ListPending(ctx context.Context, limit, offset int) ([]Offer, error) {
	out := []Offer{}
	path := fmt.Sprintf("/internal/offers/pending?limit=%d&offset=%d", limit, offset)
	err := c.t.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// SetStatus relays a decision to offer-service. That service accepts only APPROVED or
// REJECTED and only on a PENDING offer, so a second decision comes back as ErrConflict
// rather than silently overwriting the first.
func (c *OfferClient) SetStatus(ctx context.Context, offerID uuid.UUID, status string) (Offer, error) {
	var out Offer
	err := c.t.do(ctx, http.MethodPatch, "/internal/offers/"+offerID.String()+"/status",
		map[string]string{"status": status}, &out)
	return out, err
}
