package upload

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/streamforge/streamforge/internal/video"
)

const (
	AssetTypeSource = "SOURCE"
	AssetPending    = "PENDING"
	AssetReady      = "READY"
)

var (
	ErrNotFound           = errors.New("upload not found")
	ErrForbidden          = errors.New("upload forbidden")
	ErrInvalidInput       = errors.New("invalid upload input")
	ErrInvalidState       = errors.New("invalid video upload state")
	ErrUploadConflict     = errors.New("upload already initiated with different metadata")
	ErrObjectMissing      = errors.New("uploaded object not found")
	ErrObjectMismatch     = errors.New("uploaded object metadata does not match")
	ErrStorageUnavailable = errors.New("object storage unavailable")
)

type Asset struct {
	ID                  uuid.UUID
	VideoID             uuid.UUID
	AssetType           string
	ObjectKey           string
	ExpectedContentType string
	ExpectedSizeBytes   int64
	ContentType         *string
	SizeBytes           *int64
	ETag                *string
	StorageStatus       string
	CompletedAt         *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type PrepareParams struct {
	VideoID     uuid.UUID
	ObjectKey   string
	ContentType string
	SizeBytes   int64
}

type CompleteParams struct {
	VideoID     uuid.UUID
	AssetID     uuid.UUID
	ContentType string
	SizeBytes   int64
	ETag        string
}

type Actor struct {
	UserID uuid.UUID
	Role   string
}

type RequestInput struct {
	Filename    string
	ContentType string
	SizeBytes   int64
}

type UploadTicket struct {
	VideoID         uuid.UUID
	AssetID         uuid.UUID
	ObjectKey       string
	UploadURL       string
	RequiredHeaders http.Header
	ExpiresAt       time.Time
}

type Completion struct {
	Video          video.Video
	Asset          Asset
	NewlyCompleted bool
}
