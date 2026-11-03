package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/streamforge/streamforge/internal/user"
)

var (
	ErrInvalidInput       = errors.New("invalid authentication input")
	ErrInvalidCredentials = errors.New("invalid email or password")
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{2,31}$`)

type UserRepository interface {
	Create(context.Context, user.CreateParams) (user.User, error)
	FindByID(context.Context, uuid.UUID) (user.User, error)
	FindByEmail(context.Context, string) (user.User, error)
}

type Service struct {
	users      UserRepository
	sessions   SessionRepository
	passwords  PasswordHasher
	tokens     TokenManager
	refreshTTL time.Duration
	now        func() time.Time
	dummyHash  string
}

type RegisterInput struct {
	Username    string
	Email       string
	Password    string
	DisplayName string
}

type LoginInput struct {
	Email    string
	Password string
}

type Session struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	User         user.User
}

func NewService(
	users UserRepository,
	sessions SessionRepository,
	passwords PasswordHasher,
	tokens TokenManager,
	refreshTTL time.Duration,
) (*Service, error) {
	dummyHash, err := passwords.Hash("streamforge-dummy-password-not-used-for-login")
	if err != nil {
		return nil, fmt.Errorf("create timing-equalization password hash: %w", err)
	}
	return &Service{
		users: users, sessions: sessions, passwords: passwords, tokens: tokens,
		refreshTTL: refreshTTL, now: time.Now, dummyHash: dummyHash,
	}, nil
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (user.User, error) {
	username := strings.ToLower(strings.TrimSpace(input.Username))
	if !usernamePattern.MatchString(username) {
		return user.User{}, fmt.Errorf("%w: username must be 3-32 lowercase letters, numbers, or underscores and start with a letter or number", ErrInvalidInput)
	}
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return user.User{}, err
	}
	if err := validatePassword(input.Password); err != nil {
		return user.User{}, err
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = username
	}
	if length := utf8.RuneCountInString(displayName); length < 1 || length > 80 {
		return user.User{}, fmt.Errorf("%w: display_name must contain between 1 and 80 characters", ErrInvalidInput)
	}

	passwordHash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return user.User{}, fmt.Errorf("hash password: %w", err)
	}
	return s.users.Create(ctx, user.CreateParams{
		Username: username, Email: email, PasswordHash: passwordHash,
		DisplayName: displayName, Role: user.RoleUser,
	})
}

func (s *Service) Login(ctx context.Context, input LoginInput) (Session, error) {
	email, emailError := normalizeEmail(input.Email)
	if emailError != nil || input.Password == "" {
		_, _ = s.passwords.Verify(s.dummyHash, input.Password)
		return Session{}, ErrInvalidCredentials
	}

	account, err := s.users.FindByEmail(ctx, email)
	if errors.Is(err, user.ErrNotFound) {
		_, _ = s.passwords.Verify(s.dummyHash, input.Password)
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, err
	}
	matches, err := s.passwords.Verify(account.PasswordHash, input.Password)
	if err != nil {
		return Session{}, fmt.Errorf("verify password: %w", err)
	}
	if !matches {
		return Session{}, ErrInvalidCredentials
	}
	return s.newSession(ctx, account)
}

func (s *Service) Refresh(ctx context.Context, plaintext string) (Session, error) {
	oldHash, err := s.tokens.HashRefreshToken(strings.TrimSpace(plaintext))
	if err != nil {
		return Session{}, ErrInvalidRefreshToken
	}
	replacement, err := s.tokens.NewRefreshToken()
	if err != nil {
		return Session{}, err
	}
	now := s.now().UTC()
	userID, err := s.sessions.Rotate(ctx, oldHash, RefreshSession{
		ID: replacement.ID, TokenHash: replacement.Hash,
		ExpiresAt: now.Add(s.refreshTTL), CreatedAt: now,
	}, now)
	if err != nil {
		return Session{}, err
	}
	account, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return Session{}, err
	}
	accessToken, expiresAt, err := s.tokens.NewAccessToken(account)
	if err != nil {
		return Session{}, err
	}
	return Session{AccessToken: accessToken, RefreshToken: replacement.Plaintext, ExpiresAt: expiresAt, User: account}, nil
}

func (s *Service) Logout(ctx context.Context, plaintext string) error {
	hash, err := s.tokens.HashRefreshToken(strings.TrimSpace(plaintext))
	if err != nil {
		return nil
	}
	return s.sessions.RevokeFamilyByTokenHash(ctx, hash, s.now().UTC())
}

func (s *Service) newSession(ctx context.Context, account user.User) (Session, error) {
	accessToken, expiresAt, err := s.tokens.NewAccessToken(account)
	if err != nil {
		return Session{}, err
	}
	refreshToken, err := s.tokens.NewRefreshToken()
	if err != nil {
		return Session{}, err
	}
	now := s.now().UTC()
	if err := s.sessions.Create(ctx, RefreshSession{
		ID: refreshToken.ID, FamilyID: uuid.New(), UserID: account.ID,
		TokenHash: refreshToken.Hash, ExpiresAt: now.Add(s.refreshTTL), CreatedAt: now,
	}); err != nil {
		return Session{}, err
	}
	return Session{AccessToken: accessToken, RefreshToken: refreshToken.Plaintext, ExpiresAt: expiresAt, User: account}, nil
}

func normalizeEmail(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if len(normalized) > 254 {
		return "", fmt.Errorf("%w: email must be a valid address", ErrInvalidInput)
	}
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized || !strings.Contains(normalized, "@") {
		return "", fmt.Errorf("%w: email must be a valid address", ErrInvalidInput)
	}
	return normalized, nil
}

func validatePassword(password string) error {
	if utf8.RuneCountInString(password) < 12 {
		return fmt.Errorf("%w: password must contain at least 12 characters", ErrInvalidInput)
	}
	if len(password) > 128 {
		return fmt.Errorf("%w: password must not exceed 128 bytes", ErrInvalidInput)
	}
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("%w: password cannot contain only whitespace", ErrInvalidInput)
	}
	return nil
}
