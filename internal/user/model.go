package user

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	RoleUser  = "USER"
	RoleAdmin = "ADMIN"
)

var (
	ErrNotFound      = errors.New("user not found")
	ErrEmailTaken    = errors.New("email is already registered")
	ErrUsernameTaken = errors.New("username is already registered")
	ErrInvalidInput  = errors.New("invalid user input")
)

type User struct {
	ID           uuid.UUID
	Username     string
	Email        string
	PasswordHash string
	Role         string
	DisplayName  string
	Bio          string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateParams struct {
	Username     string
	Email        string
	PasswordHash string
	DisplayName  string
	Role         string
}

type UpdateProfileParams struct {
	DisplayName *string
	Bio         *string
}
