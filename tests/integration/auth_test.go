//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/streamforge/streamforge/internal/auth"
	"github.com/streamforge/streamforge/internal/health"
	"github.com/streamforge/streamforge/internal/platform/httpserver"
	"github.com/streamforge/streamforge/internal/user"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestAuthenticationWorkflow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	databaseURL := startMigratedPostgres(t, ctx)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)

	userRepository := user.NewPostgresRepository(pool)
	userService := user.NewService(userRepository)
	passwordHasher := auth.NewArgon2idHasher()
	tokenManager := auth.NewJWTTokenManager("streamforge-integration", "integration-test-secret-at-least-32-bytes", 15*time.Minute)
	sessionRepository := auth.NewPostgresSessionRepository(pool)
	authService, err := auth.NewService(userRepository, sessionRepository, passwordHasher, tokenManager, 24*time.Hour)
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	authMiddleware := auth.NewMiddleware(tokenManager)
	authHandler := auth.NewHandler(authService, passthrough, passthrough)
	userHandler := user.NewHandler(userService, authMiddleware.Authenticate, auth.UserIDFromContext)
	router := httpserver.NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		health.NewService(pool, time.Second),
		prometheus.NewRegistry(),
		authHandler,
		userHandler,
	)

	register := performJSON(t, router, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "forge_user", "email": "Forge@Example.com", "password": "correct horse battery staple", "display_name": "Forge User",
	}, "")
	if register.Code != http.StatusCreated || !strings.Contains(register.Body.String(), `"email":"forge@example.com"`) {
		t.Fatalf("register: status=%d body=%s", register.Code, register.Body.String())
	}

	duplicateEmail := performJSON(t, router, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "another_user", "email": "forge@example.com", "password": "correct horse battery staple",
	}, "")
	if duplicateEmail.Code != http.StatusConflict || !strings.Contains(duplicateEmail.Body.String(), "EMAIL_ALREADY_REGISTERED") {
		t.Fatalf("duplicate email: status=%d body=%s", duplicateEmail.Code, duplicateEmail.Body.String())
	}

	wrongLogin := performJSON(t, router, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email": "forge@example.com", "password": "this password is wrong",
	}, "")
	if wrongLogin.Code != http.StatusUnauthorized || !strings.Contains(wrongLogin.Body.String(), "INVALID_CREDENTIALS") {
		t.Fatalf("wrong login: status=%d body=%s", wrongLogin.Code, wrongLogin.Body.String())
	}

	login := performJSON(t, router, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email": "FORGE@example.com", "password": "correct horse battery staple",
	}, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login: status=%d body=%s", login.Code, login.Body.String())
	}
	firstSession := decodeSession(t, login)

	unauthenticated := performJSON(t, router, http.MethodGet, "/api/v1/users/me", nil, "")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated me: status=%d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}
	me := performJSON(t, router, http.MethodGet, "/api/v1/users/me", nil, firstSession.AccessToken)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"role":"USER"`) {
		t.Fatalf("me: status=%d body=%s", me.Code, me.Body.String())
	}
	profile := performJSON(t, router, http.MethodGet, "/api/v1/users/forge_user", nil, "")
	if profile.Code != http.StatusOK || strings.Contains(profile.Body.String(), `"email"`) {
		t.Fatalf("public profile: status=%d body=%s", profile.Code, profile.Body.String())
	}
	updated := performJSON(t, router, http.MethodPatch, "/api/v1/users/me", map[string]any{
		"display_name": "Updated Forge", "bio": "Building StreamForge",
	}, firstSession.AccessToken)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), "Building StreamForge") {
		t.Fatalf("update profile: status=%d body=%s", updated.Code, updated.Body.String())
	}

	refresh := performJSON(t, router, http.MethodPost, "/api/v1/auth/refresh", map[string]any{
		"refresh_token": firstSession.RefreshToken,
	}, "")
	if refresh.Code != http.StatusOK {
		t.Fatalf("refresh: status=%d body=%s", refresh.Code, refresh.Body.String())
	}
	secondSession := decodeSession(t, refresh)
	if secondSession.RefreshToken == firstSession.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}

	replay := performJSON(t, router, http.MethodPost, "/api/v1/auth/refresh", map[string]any{
		"refresh_token": firstSession.RefreshToken,
	}, "")
	if replay.Code != http.StatusUnauthorized || !strings.Contains(replay.Body.String(), "REFRESH_TOKEN_REUSED") {
		t.Fatalf("replay: status=%d body=%s", replay.Code, replay.Body.String())
	}
	revokedReplacement := performJSON(t, router, http.MethodPost, "/api/v1/auth/refresh", map[string]any{
		"refresh_token": secondSession.RefreshToken,
	}, "")
	if revokedReplacement.Code != http.StatusUnauthorized {
		t.Fatalf("revoked replacement: status=%d body=%s", revokedReplacement.Code, revokedReplacement.Body.String())
	}

	secondLogin := performJSON(t, router, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email": "forge@example.com", "password": "correct horse battery staple",
	}, "")
	secondLoginSession := decodeSession(t, secondLogin)
	logout := performJSON(t, router, http.MethodPost, "/api/v1/auth/logout", map[string]any{
		"refresh_token": secondLoginSession.RefreshToken,
	}, "")
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout: status=%d body=%s", logout.Code, logout.Body.String())
	}
	afterLogout := performJSON(t, router, http.MethodPost, "/api/v1/auth/refresh", map[string]any{
		"refresh_token": secondLoginSession.RefreshToken,
	}, "")
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout: status=%d body=%s", afterLogout.Code, afterLogout.Body.String())
	}

	var passwordHash string
	if err := pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE email = 'forge@example.com'`).Scan(&passwordHash); err != nil {
		t.Fatalf("query password hash: %v", err)
	}
	if passwordHash == "correct horse battery staple" || !strings.HasPrefix(passwordHash, "$argon2id$") {
		t.Fatalf("password stored insecurely: %q", passwordHash)
	}
	var invalidHashLengths int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM refresh_tokens WHERE octet_length(token_hash) <> 32`).Scan(&invalidHashLengths); err != nil {
		t.Fatalf("query refresh hash lengths: %v", err)
	}
	if invalidHashLengths != 0 {
		t.Fatalf("invalid refresh hash count = %d", invalidHashLengths)
	}
}

func startMigratedPostgres(t *testing.T, ctx context.Context) string {
	t.Helper()
	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("streamforge"),
		postgres.WithUsername("streamforge"),
		postgres.WithPassword("streamforge"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	_, currentFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations")
	migrator, err := migrate.New("file://"+migrationPath, databaseURL)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _, _ = migrator.Close() })
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate up: %v", err)
	}
	return databaseURL
}

func passthrough(next http.Handler) http.Handler { return next }

func performJSON(t *testing.T, handler http.Handler, method, path string, body any, accessToken string) *httptest.ResponseRecorder {
	t.Helper()
	var encoded io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		encoded = bytes.NewReader(payload)
	}
	request := httptest.NewRequest(method, path, encoded)
	request.RemoteAddr = "192.0.2.1:1234"
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type decodedSession struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func decodeSession(t *testing.T, response *httptest.ResponseRecorder) decodedSession {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("session response: status=%d body=%s", response.Code, response.Body.String())
	}
	var session decodedSession
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if session.AccessToken == "" || session.RefreshToken == "" {
		t.Fatalf("incomplete session response: %#v", session)
	}
	return session
}
