package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/streamforge/streamforge/internal/user"
)

type middlewareTokens struct {
	principal Principal
	err       error
}

func (m middlewareTokens) NewAccessToken(user.User) (string, time.Time, error) {
	return "", time.Time{}, nil
}
func (m middlewareTokens) ParseAccessToken(string) (Principal, error) { return m.principal, m.err }
func (m middlewareTokens) NewRefreshToken() (RefreshToken, error)     { return RefreshToken{}, nil }
func (m middlewareTokens) HashRefreshToken(string) ([]byte, error)    { return nil, nil }

func TestAuthenticationAndRoleMiddleware(t *testing.T) {
	principal := Principal{UserID: uuid.New(), Username: "forge", Role: user.RoleUser}
	middleware := NewMiddleware(middlewareTokens{principal: principal})
	protected := middleware.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual, ok := PrincipalFromContext(r.Context())
		if !ok || actual != principal {
			t.Fatalf("principal = %#v, %v", actual, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	missing := httptest.NewRecorder()
	protected.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/", nil))
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d", missing.Code)
	}

	authenticated := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	protected.ServeHTTP(authenticated, request)
	if authenticated.Code != http.StatusNoContent {
		t.Fatalf("authenticated status = %d", authenticated.Code)
	}

	adminOnly := middleware.RequireRole(user.RoleAdmin)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	forbidden := httptest.NewRecorder()
	ctx := context.WithValue(httptest.NewRequest(http.MethodGet, "/", nil).Context(), principalContextKey{}, principal)
	adminOnly.ServeHTTP(forbidden, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("USER accessing ADMIN route status = %d", forbidden.Code)
	}
}
