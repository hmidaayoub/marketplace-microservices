// Package middleware ports the common-security filter chain to chi.
package middleware

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/auth"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/httpx"
)

type contextKey struct{ name string }

var claimsKey = contextKey{"claims"}

// InternalAPIKeyHeader mirrors InternalApiKeyFilter.INTERNAL_API_KEY_HEADER.
const InternalAPIKeyHeader = "X-Internal-Api-Key"

// ClaimsFrom returns the verified identity attached by RequireJWT. The ok result is
// false only if it is called outside a RequireJWT-protected route.
func ClaimsFrom(ctx context.Context) (auth.Claims, bool) {
	c, ok := ctx.Value(claimsKey).(auth.Claims)
	return c, ok
}

// RequireJWT rejects the request unless it carries a valid access token. Unlike the
// Java JwtAuthenticationFilter, which populates the context and defers the decision to
// SecurityConfig, this rejects inline: a route that mounts it is always authenticated.
func RequireJWT(v *auth.Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				httpx.Error(w, http.StatusUnauthorized, "Missing or malformed Authorization header")
				return
			}

			claims, err := v.Verify(token)
			if err != nil {
				slog.WarnContext(r.Context(), "rejected token", "path", r.URL.Path, "error", err)
				httpx.Error(w, http.StatusUnauthorized, "Invalid token")
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
		})
	}
}

// RequireRole allows only the listed roles through. It must be mounted behind
// RequireJWT; without verified claims it fails closed.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFrom(r.Context())
			if !ok {
				httpx.Error(w, http.StatusUnauthorized, "Unauthenticated")
				return
			}
			if _, ok := allowed[claims.Role]; !ok {
				httpx.Error(w, http.StatusForbidden, "Role "+claims.Role+" may not perform this action")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireInternalAPIKey guards /internal/**. Like its Java counterpart it fails closed
// on an unconfigured key and compares in constant time, since a byte-by-byte compare
// leaks the secret through response timing.
func RequireInternalAPIKey(configured string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if configured == "" {
				slog.ErrorContext(r.Context(), "INTERNAL_API_KEY is not configured - rejecting all /internal requests")
				httpx.Error(w, http.StatusUnauthorized, "Unauthorized internal request")
				return
			}

			presented := r.Header.Get(InternalAPIKeyHeader)
			if subtle.ConstantTimeCompare([]byte(presented), []byte(configured)) != 1 {
				slog.WarnContext(r.Context(), "rejected internal request", "path", r.URL.Path)
				httpx.Error(w, http.StatusUnauthorized, "Unauthorized internal request")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if len(header) < len("Bearer ") || !strings.EqualFold(header[:7], "bearer ") {
		return "", false
	}
	token := strings.TrimSpace(header[7:])
	return token, token != ""
}
