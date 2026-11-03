package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/streamforge/streamforge/internal/platform/web"
)

type principalContextKey struct{}

type Middleware struct {
	tokens TokenManager
}

func NewMiddleware(tokens TokenManager) *Middleware {
	return &Middleware{tokens: tokens}
}

func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			web.WriteError(w, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid Bearer access token is required")
			return
		}
		principal, err := m.tokens.ParseAccessToken(parts[1])
		if err != nil {
			web.WriteError(w, http.StatusUnauthorized, "INVALID_ACCESS_TOKEN", "The access token is invalid or expired")
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *Middleware) RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := PrincipalFromContext(r.Context())
			if !ok {
				web.WriteError(w, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required")
				return
			}
			if _, ok := allowed[principal.Role]; !ok {
				web.WriteError(w, http.StatusForbidden, "FORBIDDEN", "You are not authorized to perform this action")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return uuid.Nil, false
	}
	return principal.UserID, true
}
