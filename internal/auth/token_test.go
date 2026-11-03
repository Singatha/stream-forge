package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/streamforge/streamforge/internal/user"
)

func TestJWTTokenManagerRoundTrip(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	manager := NewJWTTokenManager("streamforge", "test-only-secret-with-at-least-32-bytes", 15*time.Minute)
	manager.now = func() time.Time { return now }
	account := user.User{ID: uuid.New(), Username: "forge_user", Role: user.RoleUser}

	encoded, expiresAt, err := manager.NewAccessToken(account)
	if err != nil {
		t.Fatalf("NewAccessToken() error = %v", err)
	}
	if !expiresAt.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("expiresAt = %v", expiresAt)
	}
	principal, err := manager.ParseAccessToken(encoded)
	if err != nil {
		t.Fatalf("ParseAccessToken() error = %v", err)
	}
	if principal.UserID != account.ID || principal.Username != account.Username || principal.Role != account.Role {
		t.Fatalf("principal = %#v", principal)
	}

	manager.now = func() time.Time { return now.Add(16 * time.Minute) }
	if _, err := manager.ParseAccessToken(encoded); err == nil {
		t.Fatal("ParseAccessToken(expired) error = nil")
	}
}

func TestRefreshTokensAreOpaqueAndHashable(t *testing.T) {
	manager := NewJWTTokenManager("streamforge", "test-only-secret-with-at-least-32-bytes", 15*time.Minute)
	first, err := manager.NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() error = %v", err)
	}
	second, err := manager.NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() error = %v", err)
	}
	if first.Plaintext == second.Plaintext || string(first.Hash) == string(second.Hash) {
		t.Fatal("refresh tokens are not unique")
	}
	hash, err := manager.HashRefreshToken(first.Plaintext)
	if err != nil {
		t.Fatalf("HashRefreshToken() error = %v", err)
	}
	if string(hash) != string(first.Hash) {
		t.Fatal("HashRefreshToken() does not reproduce token hash")
	}
	if _, err := manager.HashRefreshToken("malformed"); err == nil {
		t.Fatal("HashRefreshToken(malformed) error = nil")
	}
}
