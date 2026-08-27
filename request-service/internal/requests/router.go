package requests

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/auth"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/docs"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/httpx"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/middleware"
)

type RouterConfig struct {
	Handler        *Handler
	Verifier       *auth.Verifier
	InternalAPIKey string
	Ready          func() error
}

// NewRouter wires the three call styles the platform defines (spec section 6): public
// /api routes behind a JWT, internal /internal routes behind the shared key, and open
// health endpoints so probes can reach them.
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

	r.Route("/api/requests", func(r chi.Router) {
		// Reading demand needs no token at all. A visitor has to be able to see what
		// the platform is before being asked to join it, and there is nothing here to
		// withhold: requestResponse carries aggregate totals and no participant
		// identity, which is exactly the projection a seller already gets. Everything
		// that writes still authenticates, one group down.
		//
		// /me is registered below as a static segment, so it is matched ahead of
		// {requestId} and never reaches this handler.
		r.Get("/", cfg.Handler.List)
		r.Get("/{requestId}", cfg.Handler.Get)

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireJWT(cfg.Verifier))

			// Everything that records or changes participation is a customer action.
			r.With(middleware.RequireRole(auth.RoleCustomer)).Post("/", cfg.Handler.Create)
			r.With(middleware.RequireRole(auth.RoleCustomer)).Get("/me", cfg.Handler.Mine)

			// Flat patterns rather than a nested Route: {requestId} already carries a
			// public GET, and mounting a subrouter on the same segment would collide.
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(auth.RoleCustomer))
				r.Post("/{requestId}/participants", cfg.Handler.Join)
				// Only the creator may close it; the service enforces that.
				r.Post("/{requestId}/close", cfg.Handler.Close)
				r.Put("/{requestId}/participants/me", cfg.Handler.UpdateQuantity)
				r.Delete("/{requestId}/participants/me", cfg.Handler.Leave)
			})
		})
	})

	r.Route("/internal/requests", func(r chi.Router) {
		r.Use(middleware.RequireInternalAPIKey(cfg.InternalAPIKey))

		r.Get("/{requestId}", cfg.Handler.InternalGet)
		r.Get("/{requestId}/demand", cfg.Handler.InternalDemand)
		r.Get("/{requestId}/participants", cfg.Handler.InternalParticipants)
		r.Patch("/{requestId}/status", cfg.Handler.InternalSetStatus)
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
