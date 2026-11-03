package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/streamforge/streamforge/internal/user"
)

type fakeUsers struct {
	created user.CreateParams
	account user.User
	findErr error
}

func (f *fakeUsers) Create(_ context.Context, params user.CreateParams) (user.User, error) {
	f.created = params
	if f.findErr != nil {
		return user.User{}, f.findErr
	}
	created := f.account
	created.Username = params.Username
	created.Email = params.Email
	created.PasswordHash = params.PasswordHash
	created.DisplayName = params.DisplayName
	created.Role = params.Role
	return created, nil
}

func (f *fakeUsers) FindByID(context.Context, uuid.UUID) (user.User, error) {
	return f.account, f.findErr
}

func (f *fakeUsers) FindByEmail(context.Context, string) (user.User, error) {
	return f.account, f.findErr
}

type fakeSessions struct {
	created      RefreshSession
	rotated      RefreshSession
	rotateUserID uuid.UUID
	rotateErr    error
	revokedHash  []byte
	createErr    error
}

func (f *fakeSessions) Create(_ context.Context, session RefreshSession) error {
	f.created = session
	return f.createErr
}

func (f *fakeSessions) Rotate(_ context.Context, _ []byte, replacement RefreshSession, _ time.Time) (uuid.UUID, error) {
	f.rotated = replacement
	return f.rotateUserID, f.rotateErr
}

func (f *fakeSessions) RevokeFamilyByTokenHash(_ context.Context, hash []byte, _ time.Time) error {
	f.revokedHash = hash
	return nil
}

type fakePasswords struct {
	match       bool
	hashCalls   int
	verifyCalls int
}

func (f *fakePasswords) Hash(string) (string, error) {
	f.hashCalls++
	return "encoded-password", nil
}

func (f *fakePasswords) Verify(string, string) (bool, error) {
	f.verifyCalls++
	return f.match, nil
}

type fakeTokens struct {
	refresh RefreshToken
	account user.User
}

func (f *fakeTokens) NewAccessToken(account user.User) (string, time.Time, error) {
	f.account = account
	return "access-token", time.Now().Add(15 * time.Minute), nil
}

func (f *fakeTokens) ParseAccessToken(string) (Principal, error) { return Principal{}, nil }
func (f *fakeTokens) NewRefreshToken() (RefreshToken, error)     { return f.refresh, nil }
func (f *fakeTokens) HashRefreshToken(value string) ([]byte, error) {
	if value == "invalid" {
		return nil, errors.New("invalid")
	}
	return []byte("hashed-refresh-token-32-bytes!!"), nil
}

func newTestService(t *testing.T, users *fakeUsers, sessions *fakeSessions, passwords *fakePasswords, tokens *fakeTokens) *Service {
	t.Helper()
	service, err := NewService(users, sessions, passwords, tokens, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func TestRegisterNormalizesAndHashesCredentials(t *testing.T) {
	users := &fakeUsers{account: user.User{ID: uuid.New()}}
	passwords := &fakePasswords{}
	service := newTestService(t, users, &fakeSessions{}, passwords, &fakeTokens{})

	_, err := service.Register(context.Background(), RegisterInput{
		Username: "  Forge_User ", Email: " USER@Example.COM ",
		Password: "a secure password", DisplayName: " Forge User ",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if users.created.Username != "forge_user" || users.created.Email != "user@example.com" {
		t.Fatalf("normalized credentials = %q, %q", users.created.Username, users.created.Email)
	}
	if users.created.PasswordHash != "encoded-password" || users.created.Role != user.RoleUser {
		t.Fatalf("created params = %#v", users.created)
	}
	if passwords.hashCalls != 2 { // one dummy hash and one registration hash
		t.Fatalf("Hash() calls = %d, want 2", passwords.hashCalls)
	}
}

func TestLoginUsesGenericFailureAndCreatesRefreshSession(t *testing.T) {
	account := user.User{ID: uuid.New(), Email: "user@example.com", Username: "forge", PasswordHash: "hash", Role: user.RoleUser}
	users := &fakeUsers{account: account}
	sessions := &fakeSessions{}
	passwords := &fakePasswords{match: true}
	tokens := &fakeTokens{refresh: RefreshToken{ID: uuid.New(), Plaintext: "refresh", Hash: make([]byte, 32)}}
	service := newTestService(t, users, sessions, passwords, tokens)

	result, err := service.Login(context.Background(), LoginInput{Email: account.Email, Password: "a secure password"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.AccessToken != "access-token" || result.RefreshToken != "refresh" || sessions.created.UserID != account.ID {
		t.Fatalf("session = %#v, stored = %#v", result, sessions.created)
	}

	users.findErr = user.ErrNotFound
	_, err = service.Login(context.Background(), LoginInput{Email: "missing@example.com", Password: "a secure password"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login(missing) error = %v, want ErrInvalidCredentials", err)
	}
}

func TestRefreshAndLogout(t *testing.T) {
	account := user.User{ID: uuid.New(), Username: "forge", Role: user.RoleUser}
	users := &fakeUsers{account: account}
	sessions := &fakeSessions{rotateUserID: account.ID}
	tokens := &fakeTokens{refresh: RefreshToken{ID: uuid.New(), Plaintext: "replacement", Hash: make([]byte, 32)}}
	service := newTestService(t, users, sessions, &fakePasswords{match: true}, tokens)

	result, err := service.Refresh(context.Background(), "old-refresh")
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if result.RefreshToken != "replacement" || sessions.rotated.ID != tokens.refresh.ID {
		t.Fatalf("result = %#v, rotated = %#v", result, sessions.rotated)
	}
	if err := service.Logout(context.Background(), "old-refresh"); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if len(sessions.revokedHash) == 0 {
		t.Fatal("Logout() did not revoke a token family")
	}
	if err := service.Logout(context.Background(), "invalid"); err != nil {
		t.Fatalf("Logout(invalid) error = %v", err)
	}
}
