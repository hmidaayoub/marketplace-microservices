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

type quantityBody struct {
	Quantity int32 `json:"quantity"`
}

// requestResponse is the shape returned to end users and to internal callers alike.
// It carries no customerId: participant identity is only ever exposed through
// /internal/requests/{id}/participants, so a seller browsing demand cannot enumerate
// the customers behind it (R8/R9 keep contact data behind an explicit grant).
type requestResponse struct {
	RequestID      uuid.UUID `json:"requestId"`
	ItemName       string    `json:"itemName"`
	Description    string    `json:"description"`
	Category       string    `json:"category"`
	Status         string    `json:"status"`
	TotalCustomers int32     `json:"totalCustomers"`
	TotalQuantity  int64     `json:"totalQuantity"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
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
	return requestResponse{
		RequestID:      r.RequestID,
		ItemName:       r.ItemName,
		Description:    r.Description,
		Category:       r.Category,
		Status:         r.Status,
		TotalCustomers: r.TotalCustomers,
		TotalQuantity:  r.TotalQuantity,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
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

func (b quantityBody) validate() string {
	if b.Quantity <= 0 {
		return "quantity must be greater than zero"
	}
	return ""
}
