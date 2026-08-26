package requests

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/clients"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/events"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/httpx"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/middleware"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/store"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// customerResolver is the part of the customer-service client the handlers need.
// Narrowing it here keeps the tests free of an HTTP stub for cases that never call out.
type customerResolver interface {
	ResolveCustomerID(ctx context.Context, userID uuid.UUID) (uuid.UUID, error)
}

// notifier is the part of the event publisher the handlers need. Narrowing it keeps
// the tests free of a broker: they assert on what would have been published.
type notifier interface {
	PublishOrLog(ctx context.Context, routingKey string, notifications ...events.Notification)
}

type Handler struct {
	service   *Service
	customers customerResolver
	events    notifier
}

func NewHandler(service *Service, customers customerResolver, publisher notifier) *Handler {
	return &Handler{service: service, customers: customers, events: publisher}
}

// Create handles POST /api/requests.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var body createRequestBody
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	if msg := body.validate(); msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}

	customerID, ok := h.callerCustomerID(w, r)
	if !ok {
		return
	}

	created, err := h.service.Create(r.Context(), customerID, CreateInput{
		ItemName:    body.ItemName,
		Description: body.Description,
		Category:    body.Category,
		Quantity:    body.Quantity,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// Flow 1, step 8. After the transaction, and never able to fail it: the request
	// exists whether or not the customer is told about it.
	h.notifyParticipant(r, created, "Your request is open",
		fmt.Sprintf("Your request for %s is open. You are its first participant, wanting %d.",
			created.ItemName, body.Quantity))

	httpx.JSON(w, http.StatusCreated, toResponse(created))
}

// List handles GET /api/requests. Any authenticated user may browse demand - that is
// how a seller finds a request worth making an offer against.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit, err := intParam(query.Get("limit"), defaultPageSize)
	if err != nil || limit <= 0 || limit > maxPageSize {
		httpx.Error(w, http.StatusBadRequest, "limit must be between 1 and "+strconv.Itoa(maxPageSize))
		return
	}
	offset, err := intParam(query.Get("offset"), 0)
	if err != nil || offset < 0 {
		httpx.Error(w, http.StatusBadRequest, "offset must be zero or greater")
		return
	}

	found, err := h.service.List(r.Context(), ListFilter{
		ItemName: query.Get("q"),
		Category: query.Get("category"),
		Status:   query.Get("status"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, toResponses(found))
}

// Get handles GET /api/requests/{requestId}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	requestID, ok := pathUUID(w, r, "requestId")
	if !ok {
		return
	}

	found, err := h.service.Get(r.Context(), requestID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, toResponse(found))
}

// Mine handles GET /api/requests/me: every request the calling customer takes part in,
// including the ones they created.
func (h *Handler) Mine(w http.ResponseWriter, r *http.Request) {
	customerID, ok := h.callerCustomerID(w, r)
	if !ok {
		return
	}

	found, err := h.service.ListForCustomer(r.Context(), customerID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, toResponses(found))
}

// Join handles POST /api/requests/{requestId}/participants.
func (h *Handler) Join(w http.ResponseWriter, r *http.Request) {
	requestID, ok := pathUUID(w, r, "requestId")
	if !ok {
		return
	}

	var body quantityBody
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	if msg := body.validate(); msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}

	customerID, ok := h.callerCustomerID(w, r)
	if !ok {
		return
	}

	updated, err := h.service.Join(r.Context(), requestID, customerID, body.Quantity)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	h.notifyParticipant(r, updated, "You joined a request",
		fmt.Sprintf("You joined the request for %s, wanting %d. It now has %d customers wanting %d in total.",
			updated.ItemName, body.Quantity, updated.TotalCustomers, updated.TotalQuantity))

	httpx.JSON(w, http.StatusCreated, toResponse(updated))
}

// UpdateQuantity handles PUT /api/requests/{requestId}/participants/me.
func (h *Handler) UpdateQuantity(w http.ResponseWriter, r *http.Request) {
	requestID, ok := pathUUID(w, r, "requestId")
	if !ok {
		return
	}

	var body quantityBody
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	if msg := body.validate(); msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}

	customerID, ok := h.callerCustomerID(w, r)
	if !ok {
		return
	}

	updated, err := h.service.UpdateQuantity(r.Context(), requestID, customerID, body.Quantity)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, toResponse(updated))
}

// Leave handles DELETE /api/requests/{requestId}/participants/me.
func (h *Handler) Leave(w http.ResponseWriter, r *http.Request) {
	requestID, ok := pathUUID(w, r, "requestId")
	if !ok {
		return
	}

	customerID, ok := h.callerCustomerID(w, r)
	if !ok {
		return
	}

	if _, err := h.service.Leave(r.Context(), requestID, customerID); err != nil {
		h.fail(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// InternalGet handles GET /internal/requests/{requestId}, used by other services to
// validate that a request exists before acting on it.
func (h *Handler) InternalGet(w http.ResponseWriter, r *http.Request) {
	h.Get(w, r)
}

// InternalDemand handles GET /internal/requests/{requestId}/demand.
func (h *Handler) InternalDemand(w http.ResponseWriter, r *http.Request) {
	requestID, ok := pathUUID(w, r, "requestId")
	if !ok {
		return
	}

	found, err := h.service.Get(r.Context(), requestID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, demandResponse{
		RequestID:      found.RequestID,
		TotalCustomers: found.TotalCustomers,
		TotalQuantity:  found.TotalQuantity,
	})
}

// InternalParticipants handles GET /internal/requests/{requestId}/participants. This is
// the one route that exposes customer identity, and it is why /internal must never be
// routed by the public gateway: Admin/Contact consumes it to grant ContactAccess.
func (h *Handler) InternalParticipants(w http.ResponseWriter, r *http.Request) {
	requestID, ok := pathUUID(w, r, "requestId")
	if !ok {
		return
	}

	customerIDs, err := h.service.ParticipantCustomerIDs(r.Context(), requestID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, participantsResponse{RequestID: requestID, CustomerIDs: customerIDs})
}

// callerCustomerID turns the token subject into the customerId that participation
// records. Identity comes from the token only; a caller-supplied id is never trusted.
func (h *Handler) callerCustomerID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "Unauthenticated")
		return uuid.Nil, false
	}

	customerID, err := h.customers.ResolveCustomerID(r.Context(), claims.UserID)
	switch {
	case err == nil:
		return customerID, true
	case errors.Is(err, clients.ErrCustomerNotFound):
		httpx.Error(w, http.StatusForbidden, "No customer profile exists for this account")
	default:
		slog.ErrorContext(r.Context(), "resolving customer id", "error", err)
		httpx.Error(w, http.StatusServiceUnavailable, "customer-service is unavailable")
	}
	return uuid.Nil, false
}

// notifyParticipant emits REQUEST_JOINED to the customer who acted.
//
// The recipient is the token subject, not the customerId the request records:
// notification-service is addressed by global userId and never resolves an identity,
// so the producer supplies the one it already holds.
func (h *Handler) notifyParticipant(r *http.Request, request store.PurchaseRequest, title, message string) {
	if h.events == nil {
		return
	}
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		return
	}
	h.events.PublishOrLog(r.Context(), events.KeyRequestJoined, events.Notification{
		UserID:  claims.UserID,
		Type:    "REQUEST_JOINED",
		Title:   title,
		Message: message,
	})
}

// fail maps a domain error to its status. Anything unrecognised is a real fault: it is
// logged with detail and reported without, so an internal message never reaches a client.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrRequestNotFound):
		httpx.Error(w, http.StatusNotFound, "Request not found")
	case errors.Is(err, ErrNotParticipant):
		httpx.Error(w, http.StatusNotFound, "You have not joined this request")
	case errors.Is(err, ErrAlreadyParticipant):
		httpx.Error(w, http.StatusConflict, "You have already joined this request")
	case errors.Is(err, ErrRequestNotOpen):
		httpx.Error(w, http.StatusConflict, "Request is no longer open")
	default:
		slog.ErrorContext(r.Context(), "unhandled error", "path", r.URL.Path, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "Internal server error")
	}
}

// pathUUID answers a malformed id with 400 rather than letting it reach the database -
// the same defect that had to be fixed across the Java services (issue #28).
func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid value for parameter '"+name+"'")
		return uuid.Nil, false
	}
	return id, true
}

func intParam(raw string, fallback int32) (int32, error) {
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(parsed), nil
}
