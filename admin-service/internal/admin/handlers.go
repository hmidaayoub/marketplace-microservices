package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/clients"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/events"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/httpx"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/middleware"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// notifier is the part of the event publisher the handlers need. Narrowing it keeps
// the tests free of a broker: they assert on what would have been published.
type notifier interface {
	PublishOrLog(ctx context.Context, routingKey string, notifications ...events.Notification)
}

type Handler struct {
	service *Service
	events  notifier
}

func NewHandler(service *Service, publisher notifier) *Handler {
	return &Handler{service: service, events: publisher}
}

// PendingOffers handles GET /api/admin/offers/pending.
func (h *Handler) PendingOffers(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pageParams(w, r)
	if !ok {
		return
	}

	offers, err := h.service.ListPendingOffers(r.Context(), int(limit), int(offset))
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, toPendingOfferResponses(offers))
}

// Approve handles POST /api/admin/offers/{offerId}/approve.
func (h *Handler) Approve(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, DecisionApproved)
}

// Reject handles POST /api/admin/offers/{offerId}/reject.
func (h *Handler) Reject(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, DecisionRejected)
}

// decide is the shared body of both routes. The verdict comes from which route was
// called, never from the payload, so there is no field a caller could set to turn a
// rejection into an approval.
func (h *Handler) decide(w http.ResponseWriter, r *http.Request, decision string) {
	offerID, ok := pathUUID(w, r, "offerId")
	if !ok {
		return
	}

	// The body is optional: approving with no explanation is normal, rejecting with
	// one is good practice, and neither should be a 400.
	var body decisionBody
	if r.ContentLength > 0 {
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if msg := body.validate(); msg != "" {
			httpx.Error(w, http.StatusBadRequest, msg)
			return
		}
	}

	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "Unauthenticated")
		return
	}

	result, err := h.service.Decide(r.Context(), DecideInput{
		OfferID:     offerID,
		AdminUserID: claims.UserID,
		Decision:    decision,
		Reason:      body.Reason,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// Flow 3, step 7. After the decision has committed, and unable to undo it.
	h.notifySeller(r, result)

	httpx.JSON(w, http.StatusCreated, toDecisionResponse(result.Decision, result.ContactsGranted))
}

// ListAccess handles GET /api/admin/contact-access.
func (h *Handler) ListAccess(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pageParams(w, r)
	if !ok {
		return
	}

	query := r.URL.Query()
	sellerID, ok := optionalQueryUUID(w, query.Get("sellerId"), "sellerId")
	if !ok {
		return
	}
	requestID, ok := optionalQueryUUID(w, query.Get("requestId"), "requestId")
	if !ok {
		return
	}

	found, err := h.service.ListContactAccess(r.Context(), AccessFilter{
		SellerID:  sellerID,
		RequestID: requestID,
		Status:    query.Get("status"),
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, toContactAccessResponses(found))
}

// RevokeAccess handles DELETE /api/admin/contact-access/{accessId}.
func (h *Handler) RevokeAccess(w http.ResponseWriter, r *http.Request) {
	accessID, ok := pathUUID(w, r, "accessId")
	if !ok {
		return
	}

	revoked, err := h.service.Revoke(r.Context(), accessID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, toContactAccessResponse(revoked))
}

// Contacts handles GET /api/contacts/requests/{requestId}, the one public endpoint in
// the platform that returns a phone number - and only to a seller the admin has
// granted access to, for the customers on that specific request (R9).
func (h *Handler) Contacts(w http.ResponseWriter, r *http.Request) {
	requestID, ok := pathUUID(w, r, "requestId")
	if !ok {
		return
	}

	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "Unauthenticated")
		return
	}

	contacts, err := h.service.ContactsForRequest(r.Context(), claims.UserID, requestID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, contactsResponse{RequestID: requestID, Contacts: contacts})
}

// InternalCheckAccess handles GET /internal/contact-access, used by other services to
// ask whether a seller may reach a customer before acting on that assumption.
func (h *Handler) InternalCheckAccess(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	sellerID, err := uuid.Parse(query.Get("sellerId"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid value for parameter 'sellerId'")
		return
	}
	customerID, err := uuid.Parse(query.Get("customerId"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid value for parameter 'customerId'")
		return
	}
	requestID, ok := optionalQueryUUID(w, query.Get("requestId"), "requestId")
	if !ok {
		return
	}

	allowed, err := h.service.HasContactAccess(r.Context(), sellerID, customerID, requestID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, accessCheckResponse{
		SellerID:   sellerID,
		CustomerID: customerID,
		Allowed:    allowed,
	})
}

// notifySeller emits the events that follow a decision (spec flow 3, step 7): the
// outcome itself, and - when the approval granted contact permission - the fact that
// the seller may now reach the customers behind the request.
//
// Everything here is best-effort. The decision is already committed and the grants are
// already written; a broker or a lookup failing at this point must not change either,
// so nothing below can return an error to the caller.
func (h *Handler) notifySeller(r *http.Request, result DecideResult) {
	if h.events == nil {
		return
	}

	// The seller is held as a sellerId, but notification-service is addressed by
	// userId and never resolves an identity - so the hop is made here.
	sellerUserID, err := h.service.ResolveSellerUserID(r.Context(), result.SellerID)
	if err != nil {
		slog.ErrorContext(r.Context(), "cannot address decision notification; skipping",
			"sellerId", result.SellerID, "error", err)
		return
	}

	approved := result.Decision.Decision == DecisionApproved
	key, notificationType, title := events.KeyOfferRejected, "OFFER_REJECTED", "Your offer was not approved"
	message := "An admin reviewed your offer and did not approve it."
	if approved {
		key, notificationType, title = events.KeyOfferApproved, "OFFER_APPROVED", "Your offer was approved"
		message = "An admin approved your offer."
	}
	if reason := result.Decision.Reason; reason != "" {
		message += " Reason: " + reason
	}

	h.events.PublishOrLog(r.Context(), key, events.Notification{
		UserID:  sellerUserID,
		Type:    notificationType,
		Title:   title,
		Message: message,
	})

	// A separate event, because it is a separate fact: R8 is precisely that approval
	// and contact permission are not the same thing.
	if approved && result.ContactsGranted > 0 {
		h.events.PublishOrLog(r.Context(), events.KeyContactAccessGranted, events.Notification{
			UserID: sellerUserID,
			Type:   "CONTACT_ACCESS_GRANTED",
			Title:  "You can now contact the customers",
			Message: fmt.Sprintf(
				"You have been granted contact access to %d customer(s) on this request.",
				result.ContactsGranted),
		})
	}
}

// fail maps a domain error to its status. Anything unrecognised is a real fault: it is
// logged with detail and reported without, so an internal message never reaches a client.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrOfferNotFound):
		httpx.Error(w, http.StatusNotFound, "Offer not found")
	case errors.Is(err, ErrAccessNotFound):
		httpx.Error(w, http.StatusNotFound, "Contact access not found")
	case errors.Is(err, ErrOfferNotPending):
		httpx.Error(w, http.StatusConflict, "Offer is no longer pending")
	case errors.Is(err, ErrAlreadyDecided):
		httpx.Error(w, http.StatusConflict, "Offer has already been decided")
	case errors.Is(err, ErrAlreadyRevoked):
		httpx.Error(w, http.StatusConflict, "Contact access is not currently granted")

	// A seller with no grant is told they may not see these contacts, not that the
	// request does not exist - they can already read the request itself.
	case errors.Is(err, ErrNoContactAccess):
		httpx.Error(w, http.StatusForbidden, "No contact access has been granted for this request")

	// A seller whose profile is missing cannot hold a grant in the first place.
	case errors.Is(err, clients.ErrNotFound):
		httpx.Error(w, http.StatusForbidden, "No seller profile exists for this account")

	case errors.Is(err, clients.ErrUnavailable), errors.Is(err, clients.ErrConflict):
		slog.ErrorContext(r.Context(), "dependency call failed", "path", r.URL.Path, "error", err)
		httpx.Error(w, http.StatusServiceUnavailable, "A dependent service is unavailable")

	default:
		slog.ErrorContext(r.Context(), "unhandled error", "path", r.URL.Path, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "Internal server error")
	}
}

// pathUUID answers a malformed id with 400 rather than letting it reach the database -
// the same defect that had to be fixed across the Java services (issue #28).
func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid value for parameter '"+name+"'")
		return uuid.Nil, false
	}
	return id, true
}

// optionalQueryUUID treats an absent parameter as "no filter" but still rejects a
// present-but-malformed one, so a typo narrows nothing silently.
func optionalQueryUUID(w http.ResponseWriter, raw, name string) (uuid.UUID, bool) {
	if raw == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid value for parameter '"+name+"'")
		return uuid.Nil, false
	}
	return id, true
}

func pageParams(w http.ResponseWriter, r *http.Request) (int32, int32, bool) {
	query := r.URL.Query()

	limit, err := intParam(query.Get("limit"), defaultPageSize)
	if err != nil || limit <= 0 || limit > maxPageSize {
		httpx.Error(w, http.StatusBadRequest, "limit must be between 1 and "+strconv.Itoa(maxPageSize))
		return 0, 0, false
	}
	offset, err := intParam(query.Get("offset"), 0)
	if err != nil || offset < 0 {
		httpx.Error(w, http.StatusBadRequest, "offset must be zero or greater")
		return 0, 0, false
	}
	return limit, offset, true
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
