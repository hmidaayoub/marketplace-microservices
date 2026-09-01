package requests

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/store"
)

// Field names are camelCase to match the JSON the Java services already emit.

type createRequestBody struct {
	ItemName    string `json:"itemName"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Quantity    int32  `json:"quantity"`
}

// ensureRequestBody is the internal find-or-create body. It is createRequestBody
// without the quantity, and the missing field is the whole point: nobody is joining, so
// there is no amount anybody wants.
type ensureRequestBody struct {
	ItemName    string `json:"itemName"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

type quantityBody struct {
	Quantity int32 `json:"quantity"`
}

// statusBody is the internal status call. The set of acceptable values is checked in
// the service, so one list governs both this and any other caller.
type statusBody struct {
	Status string `json:"status"`
}

// offerCountBody is what offer-service pushes when the live offers on a request change.
// A count and not a delta: a call that is retried or arrives twice has to land on the
// same answer rather than drift.
type offerCountBody struct {
	TotalOffers *int32 `json:"totalOffers"`
}

// requestResponse is the shape returned to end users and to internal callers alike.
// It carries no customerId: participant identity is only ever exposed through
// /internal/requests/{id}/participants, so a seller browsing demand cannot enumerate
// the customers behind it (R8/R9 keep contact data behind an explicit grant).
type requestResponse struct {
	RequestID uuid.UUID `json:"requestId"`

	// The customer who opened the request. Absent when nobody did - a request opened for
	// a seller offering against an item has no buyer behind it.
	CreatedBy *uuid.UUID `json:"createdBy,omitempty"`

	ItemName       string `json:"itemName"`
	Description    string `json:"description"`
	Category       string `json:"category"`
	Status         string `json:"status"`
	TotalCustomers int32  `json:"totalCustomers"`
	TotalQuantity  int64  `json:"totalQuantity"`

	// How many live offers stand on this request. Counted by offer-service - PENDING or
	// APPROVED, since a cancelled or rejected one is a record rather than an offer - and
	// reported here because it is half of what the status means.
	TotalOffers int32     `json:"totalOffers"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`

	// Whether /api/requests/{id}/image will answer with a picture. A flag and not the
	// bytes, and not a URL either: the bytes would make a browse page of twenty
	// requests twenty megabytes, and a URL the client can already build from the id it
	// holds is a second thing to keep in step with the routing table.
	HasImage bool `json:"hasImage"`

	// How close this request's item name is to the one that was searched for, 0 to 1.
	// Carried only by the near-match endpoints, so the caller can order and explain the
	// suggestions rather than being handed an unranked list.
	Similarity float32 `json:"similarity,omitempty"`

	// Set when this request carries the searched-for name outright rather than merely
	// something like it. It is what tells a client that creating is not on offer here -
	// only joining is - so the form can say so before the customer finds out from a 409.
	Exact bool `json:"exact,omitempty"`
}

// requestExistsBody is the 409 a create gets when the item already has an open request.
// It keeps message and status so it still reads as the platform-wide error shape, and
// adds the request itself - refusing without saying what to join instead would leave the
// caller nowhere to go.
type requestExistsBody struct {
	Message  string          `json:"message"`
	Status   int             `json:"status"`
	Existing requestResponse `json:"existing"`
}

type demandResponse struct {
	RequestID      uuid.UUID `json:"requestId"`
	TotalCustomers int32     `json:"totalCustomers"`
	TotalQuantity  int64     `json:"totalQuantity"`
}

type participantsResponse struct {
	RequestID   uuid.UUID   `json:"requestId"`
	CustomerIDs []uuid.UUID `json:"customerIds"`
}

func toResponse(r store.PurchaseRequest) requestResponse {
	out := requestResponse{
		RequestID:      r.RequestID,
		ItemName:       r.ItemName,
		Description:    r.Description,
		Category:       r.Category,
		Status:         r.Status,
		TotalCustomers: r.TotalCustomers,
		TotalQuantity:  r.TotalQuantity,
		TotalOffers:    r.TotalOffers,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
		// The media type is stored on the request precisely so this does not need a
		// join: it is empty for every request that carries no picture.
		HasImage: r.ImageType != "",
	}
	// The owner is a customerId, which participants already learn nothing else from -
	// it is the same identifier the request is keyed by internally, and knowing who
	// opened a request is part of reading it.
	//
	// A pointer on the way out, because omitempty does not omit a uuid.UUID: it is a
	// [16]byte, and an array is never empty as far as encoding/json is concerned. That
	// went unnoticed while every request had a creator. One opened for a seller has none
	// by design, and "createdBy": "00000000-0000-0000-0000-000000000000" would name a
	// customer who does not exist.
	if r.CreatedBy.Valid {
		owner := uuid.UUID(r.CreatedBy.Bytes)
		out.CreatedBy = &owner
	}
	return out
}

// toScoredResponses carries the similarity across, which is the one thing a near-match
// has that a plain request does not.
func toScoredResponses(rs []store.FindSimilarRequestsRow) []requestResponse {
	out := make([]requestResponse, 0, len(rs))
	for _, r := range rs {
		scored := toResponse(r.PurchaseRequest)
		scored.Similarity = r.Score
		scored.Exact = r.Exact
		out = append(out, scored)
	}
	return out
}

func toResponses(rs []store.PurchaseRequest) []requestResponse {
	out := make([]requestResponse, 0, len(rs))
	for _, r := range rs {
		out = append(out, toResponse(r))
	}
	return out
}

// validate enforces the input rules the database also enforces, so a bad request is
// answered with 400 and a clear message instead of surfacing a constraint violation.
func (b *createRequestBody) validate() string {
	b.ItemName = strings.TrimSpace(b.ItemName)
	b.Description = strings.TrimSpace(b.Description)
	b.Category = strings.TrimSpace(b.Category)

	switch {
	case b.ItemName == "":
		return "itemName is required"
	case len(b.ItemName) > 200:
		return "itemName must be at most 200 characters"
	case b.Quantity <= 0:
		return "quantity must be greater than zero"
	}
	return ""
}

// validate holds the same name rules as a customer create. A request opened for a
// seller is a request like any other, and a name the platform would refuse from a
// customer is not one it should accept here.
func (b *ensureRequestBody) validate() string {
	b.ItemName = strings.TrimSpace(b.ItemName)
	b.Description = strings.TrimSpace(b.Description)
	b.Category = strings.TrimSpace(b.Category)

	switch {
	case b.ItemName == "":
		return "itemName is required"
	case len(b.ItemName) > 200:
		return "itemName must be at most 200 characters"
	}
	return ""
}

// validate takes a pointer for totalOffers so a body that omits it is told so, rather
// than being read as zero and quietly emptying a request of its offers.
func (b offerCountBody) validate() string {
	switch {
	case b.TotalOffers == nil:
		return "totalOffers is required"
	case *b.TotalOffers < 0:
		return "totalOffers cannot be negative"
	}
	return ""
}

func (b quantityBody) validate() string {
	if b.Quantity <= 0 {
		return "quantity must be greater than zero"
	}
	return ""
}
