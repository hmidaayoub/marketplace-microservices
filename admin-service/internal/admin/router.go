package admin

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/auth"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/docs"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/httpx"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/middleware"
)

type RouterConfig struct {
	Handler        *Handler
	Verifier       *auth.Verifier
	InternalAPIKey string
	Ready          func() error
}

// NewRouter wires the three call styles the platform defines (spec section 6). This
// service is the only one with a genuinely role-split public surface: /api/admin is
// ADMIN-only because R7 puts the decision there, and /api/contacts is SELLER-only
// because it is the seller's own contact list, not an admin view of it.
func NewRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)

	// Both paths are served: /health is the Go-native name, /actuator/health keeps the
	// compose healthchecks and probe configuration identical across all services.
	health := healthHandler(cfg.Ready)
	r.Get("/health", health)
	r.Get("/actuator/health", health)

	// This service's own OpenAPI document, generated from the handler annotations. Open
	// because it describes only the public surface and carries no data; the aggregated
	// Swagger UI at the gateway fetches it from the browser.
	r.Get("/v3/api-docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(docs.SpecJSON)
	})

	// R7: every route below decides or undoes a decision, so ADMIN is required on the
	// whole subtree rather than route by route - a new admin route cannot be added
	// without inheriting the check.
	r.Route("/api/admin", func(r chi.Router) {
		r.Use(middleware.RequireJWT(cfg.Verifier))
		r.Use(middleware.RequireRole(auth.RoleAdmin))

		r.Get("/offers/pending", cfg.Handler.PendingOffers)
		r.Post("/offers/{offerId}/approve", cfg.Handler.Approve)
		r.Post("/offers/{offerId}/reject", cfg.Handler.Reject)

		r.Get("/contact-access", cfg.Handler.ListAccess)
		r.Delete("/contact-access/{accessId}", cfg.Handler.RevokeAccess)
	})

	// The seller side of the same data. A seller sees only the contacts granted to
	// them; the sellerId is resolved from the token, never read from the request.
	r.Route("/api/contacts", func(r chi.Router) {
		r.Use(middleware.RequireJWT(cfg.Verifier))
		r.Use(middleware.RequireRole(auth.RoleSeller))

		r.Get("/requests/{requestId}", cfg.Handler.Contacts)
	})

	r.Route("/internal/contact-access", func(r chi.Router) {
		r.Use(middleware.RequireInternalAPIKey(cfg.InternalAPIKey))

		r.Get("/", cfg.Handler.InternalCheckAccess)
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, http.StatusNotFound, "No such endpoint")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, http.StatusMethodNotAllowed, "Method not allowed for this endpoint")
	})

	return r
}

// healthHandler reports DOWN when the database is unreachable, so an unhealthy
// container is restarted instead of accepting traffic it cannot serve.
func healthHandler(ready func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ready != nil {
			if err := ready(); err != nil {
				httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "DOWN"})
				return
			}
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "UP"})
	}
}
