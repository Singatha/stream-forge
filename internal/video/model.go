package video

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	VisibilityPublic   = "PUBLIC"
	VisibilityPrivate  = "PRIVATE"
	VisibilityUnlisted = "UNLISTED"

	StatusCreated    = "CREATED"
	StatusUploading  = "UPLOADING"
	StatusUploaded   = "UPLOADED"
	StatusProcessing = "PROCESSING"
	StatusReady      = "READY"
	StatusFailed     = "FAILED"
)

var (
	ErrNotFound      = errors.New("video not found")
	ErrForbidden     = errors.New("video operation forbidden")
	ErrInvalidInput  = errors.New("invalid video input")
	ErrInvalidCursor = errors.New("invalid pagination cursor")
)

type Video struct {
	ID               uuid.UUID
	OwnerID          uuid.UUID
	Title            string
	Description      string
	Visibility       string
	ProcessingStatus string
	ViewCount        int64
	PublishedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateParams struct {
	OwnerID     uuid.UUID
	Title       string
	Description string
	Visibility  string
}

type UpdateParams struct {
	Title       *string
	Description *string
	Visibility  *string
}

type Actor struct {
	UserID uuid.UUID
	Role   string
}

type Cursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        uuid.UUID `json:"id"`
}

type Page struct {
	Items      []Video
	NextCursor string
}
