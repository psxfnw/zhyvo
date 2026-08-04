package httpapi

import (
	"context"
	"net/http"
	"strings"

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

func principalFromContext(ctx context.Context) (auth.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(auth.Principal)
	return principal, ok
}
