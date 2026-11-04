package storage

import (
	"context"
	"errors"
	"net/http"
	"time"
)

var ErrObjectNotFound = errors.New("object not found")

type ObjectInfo struct {
	Key          string
	Size         int64
	ContentType  string
	ETag         string
	LastModified time.Time
}

type PresignedUpload struct {
	URL     string
	Headers http.Header
}

// Store is the S3-compatible boundary used by the upload domain. Object keys
// and bucket selection remain server-controlled.
type Store interface {
	EnsureBucket(context.Context) error
	PresignPut(context.Context, string, string, time.Duration) (PresignedUpload, error)
	Stat(context.Context, string) (ObjectInfo, error)
	Delete(context.Context, string) error
}
