package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"photodrop/internal/auth"
)

type principalContextKey struct{}

func requireAuth(tokens *auth.TokenManager, service *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			parts := strings.Fields(request.Header.Get("Authorization"))
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Valid bearer token is required")
				return
			}

			principal, err := tokens.Parse(parts[1])
			if err != nil {
				writeAPIError(response, request, http.StatusUnauthorized, "INVALID_ACCESS_TOKEN", "Access token is invalid or expired")
				return
			}
			active, err := service.SessionIsActive(request.Context(), principal)
			if err != nil {
				writeAPIError(response, request, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
				return
			}
			if !active {
				writeAPIError(response, request, http.StatusUnauthorized, "SESSION_INACTIVE", "Session is expired or revoked")
				return
			}

			ctx := context.WithValue(request.Context(), principalContextKey{}, principal)
			next.ServeHTTP(response, request.WithContext(ctx))
		})
	}
}

func optionalAuth(tokens *auth.TokenManager, service *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if strings.TrimSpace(request.Header.Get("Authorization")) == "" {
				next.ServeHTTP(response, request)
				return
			}
			requireAuth(tokens, service)(next).ServeHTTP(response, request)
		})
	}
}

func requireAdmin(service interface {
	IsAdmin(context.Context, uuid.UUID) (bool, error)
}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			principal, ok := principalFromContext(request.Context())
			if !ok {
				writeAPIError(response, request, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required")
				return
			}
			allowed, err := service.IsAdmin(request.Context(), principal.IdentityID)
			if err != nil {
				writeAPIError(response, request, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
				return
			}
			if !allowed {
				writeAPIError(response, request, http.StatusForbidden, "ADMIN_REQUIRED", "Administrator access is required")
				return
			}
			next.ServeHTTP(response, request)
		})
	}
}

func principalFromContext(ctx context.Context) (auth.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(auth.Principal)
	return principal, ok
}
