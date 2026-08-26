package requests

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/clients"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/httpx"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/middleware"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// customerResolver is the part of the customer-service client the handlers need.
// Narrowing it here keeps the tests free of an HTTP stub for cases that never call out.
type customerResolver interface {
	ResolveCustomerID(ctx context.Context, userID uuid.UUID) (uuid.UUID, error)
	ResolveUserID(ctx context.Context, customerID uuid.UUID) (uuid.UUID, error)
}

type Handler struct {
	service   *Service
	customers customerResolver
}

func NewHandler(service *Service, customers customerResolver) *Handler {
	return &Handler{service: service, customers: customers}
}

// Create handles POST /api/requests.
//
//	@Summary	Create a purchase request
//	@Description Opens a request other customers can join. The caller becomes its first
//	@Description participant; the customerId is resolved from the token, never sent.
//	@Tags		requests
//	@Accept		json
//	@Produce	json
//	@Param		body	body		createRequestBody	true	"Item, category and the caller's own quantity"
//	@Success	201		{object}	requestResponse
//	@Failure	400		{object}	httpx.ErrorBody	"Validation failed"
//	@Failure	401		{object}	httpx.ErrorBody	"Missing or invalid token"
//	@Failure	403		{object}	httpx.ErrorBody	"Not a CUSTOMER, or no customer profile"
//	@Security	bearerAuth
//	@Router		/api/requests [post]
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

	actorUserID, ok := callerUserID(w, r)
	if !ok {
		return
	}

	created, err := h.service.Create(r.Context(), customerID, CreateInput{
		ItemName:    body.ItemName,
		Description: body.Description,
		Category:    body.Category,
		Quantity:    body.Quantity,
		ActorUserID: actorUserID,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, toResponse(created))
}

// List handles GET /api/requests. Any authenticated user may browse demand - that is
// how a seller finds a request worth making an offer against.
//
//	@Summary	Browse open demand
//	@Description Open to any authenticated role, sellers included: this is how a seller
//	@Description finds a request worth offering against.
//	@Tags		requests
//	@Produce	json
//	@Param		q			query		string	false	"Match on item name"
//	@Param		category	query		string	false	"Filter by category"
//	@Param		status		query		string	false	"OPEN, OFFER_APPROVED or CLOSED"
//	@Param		limit		query		int		false	"Page size, 1-100"	default(20)
//	@Param		offset		query		int		false	"Rows to skip"		default(0)
//	@Success	200			{array}		requestResponse
//	@Failure	400			{object}	httpx.ErrorBody	"Bad paging parameters"
//	@Failure	401			{object}	httpx.ErrorBody
//	@Security	bearerAuth
//	@Router		/api/requests [get]
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
//
//	@Summary	Read one request with its aggregated demand
//	@Description Returns totals, not the participant list: who joined is withheld from
//	@Description the public projection and served only on the internal endpoint.
//	@Tags		requests
//	@Produce	json
//	@Param		requestId	path		string	true	"Request id"	format(uuid)
//	@Success	200			{object}	requestResponse
//	@Failure	400			{object}	httpx.ErrorBody	"Malformed id"
//	@Failure	401			{object}	httpx.ErrorBody
//	@Failure	404			{object}	httpx.ErrorBody
//	@Security	bearerAuth
//	@Router		/api/requests/{requestId} [get]
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
//
//	@Summary	List the caller's own requests
//	@Tags		requests
//	@Produce	json
//	@Success	200	{array}		requestResponse
//	@Failure	401	{object}	httpx.ErrorBody
//	@Failure	403	{object}	httpx.ErrorBody	"Not a CUSTOMER"
//	@Security	bearerAuth
//	@Router		/api/requests/me [get]
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
//
//	@Summary	Join a request with a quantity
//	@Description Adds the caller's demand to the aggregate. Joining twice is refused;
//	@Description use the participants/me endpoint to change a quantity.
//	@Tags		participation
//	@Accept		json
//	@Produce	json
//	@Param		requestId	path		string			true	"Request id"	format(uuid)
//	@Param		body		body		quantityBody	true	"Quantity wanted"
//	@Success	201			{object}	requestResponse
//	@Failure	400			{object}	httpx.ErrorBody	"Quantity must be positive"
//	@Failure	401			{object}	httpx.ErrorBody
//	@Failure	403			{object}	httpx.ErrorBody	"Not a CUSTOMER"
//	@Failure	404			{object}	httpx.ErrorBody
//	@Failure	409			{object}	httpx.ErrorBody	"Already joined, or the request is not OPEN"
//	@Security	bearerAuth
//	@Router		/api/requests/{requestId}/participants [post]
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

	actorUserID, ok := callerUserID(w, r)
	if !ok {
		return
	}

	updated, err := h.service.Join(r.Context(), requestID, customerID, actorUserID, body.Quantity)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, toResponse(updated))
}

// UpdateQuantity handles PUT /api/requests/{requestId}/participants/me.
//
//	@Summary	Change how much the caller wants
//	@Description Addressed as participants/me rather than by participant id: a customer
//	@Description can only ever change their own row, so there is nothing else to name.
//	@Tags		participation
//	@Accept		json
//	@Produce	json
//	@Param		requestId	path		string			true	"Request id"	format(uuid)
//	@Param		body		body		quantityBody	true	"New quantity"
//	@Success	200			{object}	requestResponse
//	@Failure	400			{object}	httpx.ErrorBody
//	@Failure	401			{object}	httpx.ErrorBody
//	@Failure	403			{object}	httpx.ErrorBody	"Not a CUSTOMER"
//	@Failure	404			{object}	httpx.ErrorBody	"Not a participant"
//	@Failure	409			{object}	httpx.ErrorBody	"The request is no longer OPEN"
//	@Security	bearerAuth
//	@Router		/api/requests/{requestId}/participants/me [put]
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
//
//	@Summary	Withdraw from a request
//	@Tags		participation
//	@Param		requestId	path	string	true	"Request id"	format(uuid)
//	@Success	204			"Withdrawn"
//	@Failure	400			{object}	httpx.ErrorBody
//	@Failure	401			{object}	httpx.ErrorBody
//	@Failure	403			{object}	httpx.ErrorBody	"Not a CUSTOMER"
//	@Failure	404			{object}	httpx.ErrorBody	"Not a participant"
//	@Failure	409			{object}	httpx.ErrorBody	"The request is no longer OPEN"
//	@Security	bearerAuth
//	@Router		/api/requests/{requestId}/participants/me [delete]
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

// Close handles POST /api/requests/{requestId}/close.
//
//	@Summary	Close a request
//	@Description Only the creator may close it. Closing notifies every participant in a
//	@Description single fan-out event, written in the same transaction as the status change.
//	@Tags		requests
//	@Produce	json
//	@Param		requestId	path		string	true	"Request id"	format(uuid)
//	@Success	200			{object}	requestResponse
//	@Failure	400			{object}	httpx.ErrorBody
//	@Failure	401			{object}	httpx.ErrorBody
//	@Failure	403			{object}	httpx.ErrorBody	"Not the creator"
//	@Failure	404			{object}	httpx.ErrorBody
//	@Failure	409			{object}	httpx.ErrorBody	"Already closed"
//	@Security	bearerAuth
//	@Router		/api/requests/{requestId}/close [post]
func (h *Handler) Close(w http.ResponseWriter, r *http.Request) {
	requestID, ok := pathUUID(w, r, "requestId")
	if !ok {
		return
	}

	customerID, ok := h.callerCustomerID(w, r)
	if !ok {
		return
	}

	// Resolved before the transaction: closing tells every participant, and each one
	// needs a userId that only customer-service can supply. A participant whose lookup
	// fails is simply not notified - see Close.
	recipients, err := h.participantUserIDs(r, requestID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	closed, err := h.service.Close(r.Context(), CloseInput{
		RequestID:  requestID,
		CustomerID: customerID,
		Recipients: recipients,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, toResponse(closed))
}

// participantUserIDs maps every participant to the userId a notification is addressed
// to. One unresolvable participant does not fail the close: the request is the owner's
// to end, and a customer-service blip must not stand in the way of that.
func (h *Handler) participantUserIDs(r *http.Request, requestID uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	customerIDs, err := h.service.ParticipantCustomerIDs(r.Context(), requestID)
	if err != nil {
		return nil, err
	}

	recipients := make(map[uuid.UUID]uuid.UUID, len(customerIDs))
	for _, customerID := range customerIDs {
		userID, err := h.customers.ResolveUserID(r.Context(), customerID)
		if err != nil {
			slog.WarnContext(r.Context(), "cannot address a participant; closing without them",
				"customerId", customerID, "error", err)
			continue
		}
		recipients[customerID] = userID
	}
	return recipients, nil
}

// InternalSetStatus handles PATCH /internal/requests/{requestId}/status.
//
// How Admin/Contact moves a request to OFFER_APPROVED once it has approved an offer.
// Offer-service already refuses new offers against a request in that state, so until
// something made the transition that guard could never fire.
func (h *Handler) InternalSetStatus(w http.ResponseWriter, r *http.Request) {
	requestID, ok := pathUUID(w, r, "requestId")
	if !ok {
		return
	}

	var body statusBody
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}

	updated, err := h.service.SetStatus(r.Context(), requestID, body.Status)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, toResponse(updated))
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

// callerUserID returns the token subject, which is who a notification is addressed to.
// Notification-service is addressed by global userId and never resolves an identity, so
// the producer supplies the one it already holds.
func callerUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "Unauthenticated")
		return uuid.Nil, false
	}
	return claims.UserID, true
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
	case errors.Is(err, ErrRequestNotClosable):
		httpx.Error(w, http.StatusConflict, "Request can no longer be closed")
	case errors.Is(err, ErrInvalidStatus):
		httpx.Error(w, http.StatusBadRequest, "status must be one of OFFER_PENDING, OFFER_APPROVED, CLOSED, CANCELLED")

	// Not 404: the caller can see the request, they simply did not create it.
	case errors.Is(err, ErrNotRequestOwner):
		httpx.Error(w, http.StatusForbidden, "Only the customer who created this request may close it")
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
