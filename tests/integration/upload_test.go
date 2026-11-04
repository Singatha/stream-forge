//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/streamforge/streamforge/internal/auth"
	"github.com/streamforge/streamforge/internal/health"
	"github.com/streamforge/streamforge/internal/platform/httpserver"
	objectstorage "github.com/streamforge/streamforge/internal/storage"
	"github.com/streamforge/streamforge/internal/upload"
	"github.com/streamforge/streamforge/internal/user"
	"github.com/streamforge/streamforge/internal/video"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestDirectUploadWorkflow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	databaseURL := startMigratedPostgres(t, ctx)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)

	minioContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "minio/minio:RELEASE.2025-04-22T22-12-26Z",
			ExposedPorts: []string{"9000/tcp"},
			Env: map[string]string{
				"MINIO_ROOT_USER":     "streamforge",
				"MINIO_ROOT_PASSWORD": "streamforge-integration-secret",
			},
			Cmd: []string{"server", "/data"},
			WaitingFor: wait.ForHTTP("/minio/health/ready").WithPort("9000/tcp").
				WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start MinIO container: %v", err)
	}
	t.Cleanup(func() { _ = minioContainer.Terminate(context.Background()) })
	host, err := minioContainer.Host(ctx)
	if err != nil {
		t.Fatalf("MinIO host: %v", err)
	}
	port, err := minioContainer.MappedPort(ctx, "9000/tcp")
	if err != nil {
		t.Fatalf("MinIO port: %v", err)
	}
	endpoint := net.JoinHostPort(host, port.Port())
	objectStore, err := objectstorage.NewMinIOStore(objectstorage.MinIOConfig{
		Endpoint: endpoint, PublicEndpoint: endpoint,
		AccessKey: "streamforge", SecretKey: "streamforge-integration-secret",
		Bucket: "streamforge-videos", Region: "us-east-1", AutoCreate: true,
	})
	if err != nil {
		t.Fatalf("create MinIO store: %v", err)
	}
	if err := objectStore.EnsureBucket(ctx); err != nil {
		t.Fatalf("ensure MinIO bucket: %v", err)
	}

	userRepository := user.NewPostgresRepository(pool)
	tokenManager := auth.NewJWTTokenManager("streamforge-upload-integration", "integration-test-secret-at-least-32-bytes", 15*time.Minute)
	authService, err := auth.NewService(
		userRepository, auth.NewPostgresSessionRepository(pool), auth.NewArgon2idHasher(), tokenManager, 24*time.Hour,
	)
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	authMiddleware := auth.NewMiddleware(tokenManager)
	videoRepository := video.NewPostgresRepository(pool)
	uploadService := upload.NewService(
		upload.NewPostgresRepository(pool), videoRepository, objectStore, 5*time.Minute, 1024,
	)
	registry := prometheus.NewRegistry()
	router := httpserver.NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		health.NewService(pool, time.Second), registry,
		auth.NewHandler(authService, passthrough, passthrough),
		user.NewHandler(user.NewService(userRepository), authMiddleware.Authenticate, auth.UserIDFromContext),
		video.NewHandler(video.NewService(videoRepository), authMiddleware.Authenticate, authMiddleware.OptionalAuthenticate),
		upload.NewHandler(
			uploadService, authMiddleware.Authenticate,
			httpserver.NewIPRateLimiter(100, time.Minute).Middleware,
			upload.NewMetrics(registry),
		),
	)

	ownerToken := registerAndLogin(t, router, "upload_owner", "upload-owner@example.com")
	otherToken := registerAndLogin(t, router, "upload_other", "upload-other@example.com")
	created := decodeVideo(t, performJSON(t, router, http.MethodPost, "/api/v1/videos", map[string]any{
		"title": "Direct upload",
	}, ownerToken), http.StatusCreated)
	source := "phase-four-source"
	uploadPath := "/api/v1/videos/" + created.ID + "/upload-url"

	forbidden := performJSON(t, router, http.MethodPost, uploadPath, map[string]any{
		"filename": "source.mp4", "content_type": "video/mp4", "size_bytes": len(source),
	}, otherToken)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("other owner upload URL: status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}
	oversized := performJSON(t, router, http.MethodPost, uploadPath, map[string]any{
		"filename": "source.mp4", "content_type": "video/mp4", "size_bytes": 1025,
	}, ownerToken)
	if oversized.Code != http.StatusUnprocessableEntity {
		t.Fatalf("oversized upload URL: status=%d body=%s", oversized.Code, oversized.Body.String())
	}

	ticketResponse := performJSON(t, router, http.MethodPost, uploadPath, map[string]any{
		"filename": "source.mp4", "content_type": "video/mp4", "size_bytes": len(source),
	}, ownerToken)
	if ticketResponse.Code != http.StatusOK {
		t.Fatalf("upload URL: status=%d body=%s", ticketResponse.Code, ticketResponse.Body.String())
	}
	var ticket struct {
		UploadURL       string            `json:"upload_url"`
		ObjectKey       string            `json:"object_key"`
		RequiredHeaders map[string]string `json:"required_headers"`
	}
	if err := json.Unmarshal(ticketResponse.Body.Bytes(), &ticket); err != nil {
		t.Fatalf("decode upload ticket: %v", err)
	}
	if !strings.HasPrefix(ticket.ObjectKey, "videos/"+created.ID+"/") || strings.Contains(ticket.ObjectKey, "..") {
		t.Fatalf("unsafe object key %q", ticket.ObjectKey)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, ticket.UploadURL, strings.NewReader(source))
	if err != nil {
		t.Fatalf("create direct upload request: %v", err)
	}
	for key, value := range ticket.RequiredHeaders {
		request.Header.Set(key, value)
	}
	directResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("direct upload: %v", err)
	}
	_, _ = io.Copy(io.Discard, directResponse.Body)
	_ = directResponse.Body.Close()
	if directResponse.StatusCode != http.StatusOK {
		t.Fatalf("direct upload status = %d", directResponse.StatusCode)
	}

	completePath := "/api/v1/videos/" + created.ID + "/upload-complete"
	completion := performJSON(t, router, http.MethodPost, completePath, nil, ownerToken)
	if completion.Code != http.StatusOK || !strings.Contains(completion.Body.String(), `"processing_status":"UPLOADED"`) {
		t.Fatalf("upload completion: status=%d body=%s", completion.Code, completion.Body.String())
	}
	repeated := performJSON(t, router, http.MethodPost, completePath, nil, ownerToken)
	if repeated.Code != http.StatusOK {
		t.Fatalf("repeated completion: status=%d body=%s", repeated.Code, repeated.Body.String())
	}

	var status, storageStatus, objectKey string
	var size int64
	if err := pool.QueryRow(ctx, `
		SELECT v.processing_status, a.storage_status, a.object_key, a.size_bytes
		FROM videos v JOIN video_assets a ON a.video_id = v.id
		WHERE v.id = $1 AND a.asset_type = 'SOURCE'`, created.ID,
	).Scan(&status, &storageStatus, &objectKey, &size); err != nil {
		t.Fatalf("query completed upload: %v", err)
	}
	if status != video.StatusUploaded || storageStatus != upload.AssetReady || objectKey != ticket.ObjectKey || size != int64(len(source)) {
		t.Fatalf("persisted upload = status %q storage %q key %q size %d", status, storageStatus, objectKey, size)
	}

	interrupted := decodeVideo(t, performJSON(t, router, http.MethodPost, "/api/v1/videos", map[string]any{
		"title": fmt.Sprintf("Interrupted %d", time.Now().UnixNano()),
	}, ownerToken), http.StatusCreated)
	interruptedPath := "/api/v1/videos/" + interrupted.ID + "/upload-url"
	response := performJSON(t, router, http.MethodPost, interruptedPath, map[string]any{
		"filename": "source.webm", "content_type": "video/webm", "size_bytes": 20,
	}, ownerToken)
	if response.Code != http.StatusOK {
		t.Fatalf("interrupted upload URL: status=%d body=%s", response.Code, response.Body.String())
	}
	missing := performJSON(t, router, http.MethodPost, "/api/v1/videos/"+interrupted.ID+"/upload-complete", nil, ownerToken)
	if missing.Code != http.StatusUnprocessableEntity || !strings.Contains(missing.Body.String(), "UPLOAD_NOT_FOUND") {
		t.Fatalf("interrupted completion: status=%d body=%s", missing.Code, missing.Body.String())
	}
}
