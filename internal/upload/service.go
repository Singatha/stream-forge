package upload

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/streamforge/streamforge/internal/storage"
	"github.com/streamforge/streamforge/internal/user"
	"github.com/streamforge/streamforge/internal/video"
)

type VideoRepository interface {
	FindByID(context.Context, uuid.UUID) (video.Video, error)
}

type Service struct {
	repository     Repository
	videos         VideoRepository
	storage        storage.Store
	presignTTL     time.Duration
	maxUploadBytes int64
	now            func() time.Time
}

func NewService(repository Repository, videos VideoRepository, objectStore storage.Store, presignTTL time.Duration, maxUploadBytes int64) *Service {
	return &Service{
		repository: repository, videos: videos, storage: objectStore,
		presignTTL: presignTTL, maxUploadBytes: maxUploadBytes, now: time.Now,
	}
}

func (s *Service) RequestUpload(ctx context.Context, videoID uuid.UUID, actor Actor, input RequestInput) (UploadTicket, error) {
	found, err := s.authorize(ctx, videoID, actor)
	if err != nil {
		return UploadTicket{}, err
	}
	if found.ProcessingStatus != video.StatusCreated && found.ProcessingStatus != video.StatusUploading {
		return UploadTicket{}, ErrInvalidState
	}
	contentType, extension, err := validateInput(input, s.maxUploadBytes)
	if err != nil {
		return UploadTicket{}, err
	}
	objectKey := fmt.Sprintf("videos/%s/source%s", videoID, extension)
	asset, err := s.repository.PrepareSource(ctx, PrepareParams{
		VideoID: videoID, ObjectKey: objectKey, ContentType: contentType, SizeBytes: input.SizeBytes,
	})
	if err != nil {
		return UploadTicket{}, err
	}
	presigned, err := s.storage.PresignPut(ctx, asset.ObjectKey, asset.ExpectedContentType, s.presignTTL)
	if err != nil {
		return UploadTicket{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	return UploadTicket{
		VideoID: videoID, AssetID: asset.ID, ObjectKey: asset.ObjectKey,
		UploadURL: presigned.URL, RequiredHeaders: presigned.Headers,
		ExpiresAt: s.now().UTC().Add(s.presignTTL),
	}, nil
}

func (s *Service) CompleteUpload(ctx context.Context, videoID uuid.UUID, actor Actor) (Completion, error) {
	found, err := s.authorize(ctx, videoID, actor)
	if err != nil {
		return Completion{}, err
	}
	asset, err := s.repository.FindSource(ctx, videoID)
	if err != nil {
		return Completion{}, err
	}
	if found.ProcessingStatus == video.StatusUploaded && asset.StorageStatus == AssetReady {
		return Completion{Video: found, Asset: asset}, nil
	}
	if found.ProcessingStatus != video.StatusUploading || asset.StorageStatus != AssetPending {
		return Completion{}, ErrInvalidState
	}

	info, err := s.storage.Stat(ctx, asset.ObjectKey)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return Completion{}, ErrObjectMissing
	}
	if err != nil {
		return Completion{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	actualType := normalizeContentType(info.ContentType)
	etag := strings.Trim(strings.TrimSpace(info.ETag), `"`)
	if info.Size != asset.ExpectedSizeBytes || info.Size <= 0 || info.Size > s.maxUploadBytes ||
		actualType != asset.ExpectedContentType || etag == "" {
		_ = s.storage.Delete(ctx, asset.ObjectKey)
		return Completion{}, ErrObjectMismatch
	}
	completedAsset, newlyCompleted, err := s.repository.CompleteSource(ctx, CompleteParams{
		VideoID: videoID, AssetID: asset.ID, ContentType: actualType,
		SizeBytes: info.Size, ETag: etag,
	})
	if err != nil {
		return Completion{}, err
	}
	completedVideo, err := s.videos.FindByID(ctx, videoID)
	if err != nil {
		return Completion{}, err
	}
	return Completion{Video: completedVideo, Asset: completedAsset, NewlyCompleted: newlyCompleted}, nil
}

func (s *Service) authorize(ctx context.Context, videoID uuid.UUID, actor Actor) (video.Video, error) {
	if actor.UserID == uuid.Nil {
		return video.Video{}, ErrForbidden
	}
	found, err := s.videos.FindByID(ctx, videoID)
	if errors.Is(err, video.ErrNotFound) {
		return video.Video{}, ErrNotFound
	}
	if err != nil {
		return video.Video{}, err
	}
	if actor.UserID != found.OwnerID && actor.Role != user.RoleAdmin {
		return video.Video{}, ErrForbidden
	}
	return found, nil
}

func validateInput(input RequestInput, maxBytes int64) (string, string, error) {
	filename := strings.TrimSpace(input.Filename)
	if filename == "" || len(filename) > 255 || strings.ContainsAny(filename, `/\\\x00`) || filepath.Base(filename) != filename {
		return "", "", fmt.Errorf("%w: filename must be a plain file name", ErrInvalidInput)
	}
	if input.SizeBytes <= 0 || input.SizeBytes > maxBytes {
		return "", "", fmt.Errorf("%w: size_bytes must be between 1 and %d", ErrInvalidInput, maxBytes)
	}
	extension := strings.ToLower(filepath.Ext(filename))
	contentType := normalizeContentType(input.ContentType)
	allowed := map[string]map[string]bool{
		".mp4":  {"video/mp4": true},
		".mov":  {"video/quicktime": true},
		".webm": {"video/webm": true},
		".mkv":  {"video/x-matroska": true, "video/matroska": true},
	}
	if !allowed[extension][contentType] {
		return "", "", fmt.Errorf("%w: supported formats are MP4, MOV, WebM, and Matroska with a matching video MIME type", ErrInvalidInput)
	}
	return contentType, extension, nil
}

func normalizeContentType(value string) string {
	if index := strings.IndexByte(value, ';'); index >= 0 {
		value = value[:index]
	}
	return strings.ToLower(strings.TrimSpace(value))
}
