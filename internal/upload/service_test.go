package upload

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	objectstorage "github.com/streamforge/streamforge/internal/storage"
	"github.com/streamforge/streamforge/internal/user"
	"github.com/streamforge/streamforge/internal/video"
)

type fakeRepository struct {
	asset       Asset
	prepare     PrepareParams
	complete    CompleteParams
	prepareErr  error
	findErr     error
	completeErr error
	newly       bool
}

func (r *fakeRepository) PrepareSource(_ context.Context, params PrepareParams) (Asset, error) {
	r.prepare = params
	return r.asset, r.prepareErr
}
func (r *fakeRepository) FindSource(context.Context, uuid.UUID) (Asset, error) {
	return r.asset, r.findErr
}
func (r *fakeRepository) CompleteSource(_ context.Context, params CompleteParams) (Asset, bool, error) {
	r.complete = params
	return r.asset, r.newly, r.completeErr
}

type fakeVideos struct {
	video video.Video
	err   error
}

func (v *fakeVideos) FindByID(context.Context, uuid.UUID) (video.Video, error) {
	return v.video, v.err
}

type fakeStore struct {
	presigned  objectstorage.PresignedUpload
	info       objectstorage.ObjectInfo
	presignErr error
	statErr    error
	deletedKey string
}

func (s *fakeStore) EnsureBucket(context.Context) error { return nil }
func (s *fakeStore) PresignPut(_ context.Context, _, _ string, _ time.Duration) (objectstorage.PresignedUpload, error) {
	return s.presigned, s.presignErr
}
func (s *fakeStore) Stat(context.Context, string) (objectstorage.ObjectInfo, error) {
	return s.info, s.statErr
}
func (s *fakeStore) Delete(_ context.Context, key string) error {
	s.deletedKey = key
	return nil
}

func TestRequestUploadProducesServerControlledTicket(t *testing.T) {
	videoID := uuid.New()
	ownerID := uuid.New()
	assetID := uuid.New()
	repository := &fakeRepository{asset: Asset{
		ID: assetID, VideoID: videoID, ObjectKey: "videos/" + videoID.String() + "/source.mp4",
		ExpectedContentType: "video/mp4", ExpectedSizeBytes: 1024, StorageStatus: AssetPending,
	}}
	videos := &fakeVideos{video: video.Video{
		ID: videoID, OwnerID: ownerID, ProcessingStatus: video.StatusCreated,
	}}
	store := &fakeStore{presigned: objectstorage.PresignedUpload{
		URL: "https://uploads.example.test/signed", Headers: http.Header{"Content-Type": {"video/mp4"}},
	}}
	service := NewService(repository, videos, store, 10*time.Minute, 2048)
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	ticket, err := service.RequestUpload(context.Background(), videoID, Actor{UserID: ownerID, Role: user.RoleUser}, RequestInput{
		Filename: "clip.MP4", ContentType: "video/mp4", SizeBytes: 1024,
	})
	if err != nil {
		t.Fatalf("RequestUpload() error = %v", err)
	}
	if repository.prepare.ObjectKey != "videos/"+videoID.String()+"/source.mp4" || repository.prepare.VideoID != videoID {
		t.Fatalf("PrepareSource() params = %#v", repository.prepare)
	}
	if ticket.AssetID != assetID || ticket.UploadURL == "" || ticket.ExpiresAt != now.Add(10*time.Minute) {
		t.Fatalf("ticket = %#v", ticket)
	}
	if ticket.RequiredHeaders.Get("Content-Type") != "video/mp4" {
		t.Fatalf("required headers = %#v", ticket.RequiredHeaders)
	}
}

func TestRequestUploadValidationAndAuthorization(t *testing.T) {
	videoID := uuid.New()
	ownerID := uuid.New()
	service := NewService(&fakeRepository{}, &fakeVideos{video: video.Video{
		ID: videoID, OwnerID: ownerID, ProcessingStatus: video.StatusCreated,
	}}, &fakeStore{}, time.Minute, 100)

	tests := []RequestInput{
		{Filename: "../clip.mp4", ContentType: "video/mp4", SizeBytes: 10},
		{Filename: "clip.exe", ContentType: "video/mp4", SizeBytes: 10},
		{Filename: "clip.mp4", ContentType: "video/webm", SizeBytes: 10},
		{Filename: "clip.mp4", ContentType: "video/mp4", SizeBytes: 101},
	}
	for _, input := range tests {
		if _, err := service.RequestUpload(context.Background(), videoID, Actor{UserID: ownerID}, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("RequestUpload(%#v) error = %v", input, err)
		}
	}
	if _, err := service.RequestUpload(context.Background(), videoID, Actor{UserID: uuid.New()}, RequestInput{
		Filename: "clip.mp4", ContentType: "video/mp4", SizeBytes: 10,
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("RequestUpload(other owner) error = %v", err)
	}
}

func TestCompleteUploadVerifiesObjectAndIsRetrySafe(t *testing.T) {
	videoID := uuid.New()
	ownerID := uuid.New()
	contentType := "video/mp4"
	size := int64(128)
	etag := "etag"
	completedAt := time.Now().UTC()
	repository := &fakeRepository{newly: true, asset: Asset{
		ID: uuid.New(), VideoID: videoID, ObjectKey: "videos/" + videoID.String() + "/source.mp4",
		ExpectedContentType: contentType, ExpectedSizeBytes: size, StorageStatus: AssetPending,
		ContentType: &contentType, SizeBytes: &size, ETag: &etag, CompletedAt: &completedAt,
	}}
	videos := &fakeVideos{video: video.Video{
		ID: videoID, OwnerID: ownerID, ProcessingStatus: video.StatusUploading,
	}}
	store := &fakeStore{info: objectstorage.ObjectInfo{Size: size, ContentType: contentType, ETag: etag}}
	service := NewService(repository, videos, store, time.Minute, 1024)

	completion, err := service.CompleteUpload(context.Background(), videoID, Actor{UserID: ownerID})
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	if !completion.NewlyCompleted || repository.complete.SizeBytes != size || repository.complete.ETag != etag {
		t.Fatalf("completion = %#v params = %#v", completion, repository.complete)
	}

	videos.video.ProcessingStatus = video.StatusUploaded
	repository.asset.StorageStatus = AssetReady
	repository.newly = false
	store.statErr = errors.New("should not be called")
	completion, err = service.CompleteUpload(context.Background(), videoID, Actor{UserID: ownerID})
	if err != nil || completion.NewlyCompleted {
		t.Fatalf("repeated CompleteUpload() = %#v, %v", completion, err)
	}
}

func TestCompleteUploadRejectsMissingAndMismatchedObjects(t *testing.T) {
	videoID := uuid.New()
	ownerID := uuid.New()
	key := "videos/" + videoID.String() + "/source.webm"
	repository := &fakeRepository{asset: Asset{
		ID: uuid.New(), VideoID: videoID, ObjectKey: key,
		ExpectedContentType: "video/webm", ExpectedSizeBytes: 100, StorageStatus: AssetPending,
	}}
	videos := &fakeVideos{video: video.Video{
		ID: videoID, OwnerID: ownerID, ProcessingStatus: video.StatusUploading,
	}}
	store := &fakeStore{statErr: objectstorage.ErrObjectNotFound}
	service := NewService(repository, videos, store, time.Minute, 1000)

	if _, err := service.CompleteUpload(context.Background(), videoID, Actor{UserID: ownerID}); !errors.Is(err, ErrObjectMissing) {
		t.Fatalf("CompleteUpload(missing) error = %v", err)
	}
	store.statErr = nil
	store.info = objectstorage.ObjectInfo{Size: 101, ContentType: "video/webm", ETag: strings.Repeat("a", 32)}
	if _, err := service.CompleteUpload(context.Background(), videoID, Actor{UserID: ownerID}); !errors.Is(err, ErrObjectMismatch) {
		t.Fatalf("CompleteUpload(mismatch) error = %v", err)
	}
	if store.deletedKey != key {
		t.Fatalf("deleted key = %q, want %q", store.deletedKey, key)
	}
}
