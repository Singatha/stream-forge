package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/streamforge/streamforge/internal/user"
)

var ErrInvalidAccessToken = errors.New("invalid access token")

type Principal struct {
	UserID   uuid.UUID
	Username string
	Role     string
}

type AccessClaims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type RefreshToken struct {
	ID        uuid.UUID
	Plaintext string
	Hash      []byte
}

type TokenManager interface {
	NewAccessToken(user.User) (string, time.Time, error)
	ParseAccessToken(string) (Principal, error)
	NewRefreshToken() (RefreshToken, error)
	HashRefreshToken(string) ([]byte, error)
}

type JWTTokenManager struct {
	issuer    string
	secret    []byte
	accessTTL time.Duration
	now       func() time.Time
}

func NewJWTTokenManager(issuer, secret string, accessTTL time.Duration) *JWTTokenManager {
	return &JWTTokenManager{issuer: issuer, secret: []byte(secret), accessTTL: accessTTL, now: time.Now}
}

func (m *JWTTokenManager) NewAccessToken(account user.User) (string, time.Time, error) {
	now := m.now().UTC()
	expiresAt := now.Add(m.accessTTL)
	claims := AccessClaims{
		Username: account.Username,
		Role:     account.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   account.ID.String(),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

func (m *JWTTokenManager) ParseAccessToken(encoded string) (Principal, error) {
	claims := &AccessClaims{}
	parsed, err := jwt.ParseWithClaims(encoded, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidAccessToken
		}
		return m.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(30*time.Second),
		jwt.WithTimeFunc(m.now),
	)
	if err != nil || !parsed.Valid {
		return Principal{}, ErrInvalidAccessToken
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil || claims.Username == "" || (claims.Role != user.RoleUser && claims.Role != user.RoleAdmin) {
		return Principal{}, ErrInvalidAccessToken
	}
	return Principal{UserID: userID, Username: claims.Username, Role: claims.Role}, nil
}

func (m *JWTTokenManager) NewRefreshToken() (RefreshToken, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return RefreshToken{}, fmt.Errorf("generate refresh token: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256(raw)
	return RefreshToken{ID: uuid.New(), Plaintext: plaintext, Hash: hash[:]}, nil
}

func (m *JWTTokenManager) HashRefreshToken(plaintext string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(plaintext)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("refresh token has an invalid format")
	}
	hash := sha256.Sum256(raw)
	return hash[:], nil
}
