//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/streamforge/streamforge/internal/auth"
	"github.com/streamforge/streamforge/internal/health"
	"github.com/streamforge/streamforge/internal/platform/httpserver"
	"github.com/streamforge/streamforge/internal/user"
	"github.com/streamforge/streamforge/internal/video"
)

func TestVideoMetadataWorkflow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	databaseURL := startMigratedPostgres(t, ctx)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)

	userRepository := user.NewPostgresRepository(pool)
	passwordHasher := auth.NewArgon2idHasher()
	tokenManager := auth.NewJWTTokenManager("streamforge-video-integration", "integration-test-secret-at-least-32-bytes", 15*time.Minute)
	authService, err := auth.NewService(
		userRepository,
		auth.NewPostgresSessionRepository(pool),
		passwordHasher,
		tokenManager,
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	authMiddleware := auth.NewMiddleware(tokenManager)
	videoRepository := video.NewPostgresRepository(pool)
	router := httpserver.NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		health.NewService(pool, time.Second),
		prometheus.NewRegistry(),
		auth.NewHandler(authService, passthrough, passthrough),
		user.NewHandler(user.NewService(userRepository), authMiddleware.Authenticate, auth.UserIDFromContext),
		video.NewHandler(video.NewService(videoRepository), authMiddleware.Authenticate, authMiddleware.OptionalAuthenticate),
	)

	ownerToken := registerAndLogin(t, router, "video_owner", "owner@example.com")
	otherToken := registerAndLogin(t, router, "other_user", "other@example.com")

	privateResponse := performJSON(t, router, http.MethodPost, "/api/v1/videos", map[string]any{
		"title": "Private draft", "description": "Not public yet",
	}, ownerToken)
	privateVideo := decodeVideo(t, privateResponse, http.StatusCreated)
	if privateVideo.Visibility != video.VisibilityPrivate || privateVideo.ProcessingStatus != video.StatusCreated || privateVideo.ViewCount != 0 {
		t.Fatalf("private video defaults = %#v", privateVideo)
	}

	massAssignment := performJSON(t, router, http.MethodPost, "/api/v1/videos", map[string]any{
		"title": "Injected", "owner_id": "00000000-0000-0000-0000-000000000000", "processing_status": "READY",
	}, ownerToken)
	if massAssignment.Code != http.StatusBadRequest || !strings.Contains(massAssignment.Body.String(), "INVALID_REQUEST") {
		t.Fatalf("mass assignment: status=%d body=%s", massAssignment.Code, massAssignment.Body.String())
	}

	anonymousPrivate := performJSON(t, router, http.MethodGet, "/api/v1/videos/"+privateVideo.ID, nil, "")
	if anonymousPrivate.Code != http.StatusNotFound {
		t.Fatalf("anonymous private read: status=%d body=%s", anonymousPrivate.Code, anonymousPrivate.Body.String())
	}
	otherPrivate := performJSON(t, router, http.MethodGet, "/api/v1/videos/"+privateVideo.ID, nil, otherToken)
	if otherPrivate.Code != http.StatusNotFound {
		t.Fatalf("other private read: status=%d body=%s", otherPrivate.Code, otherPrivate.Body.String())
	}
	ownerPrivate := performJSON(t, router, http.MethodGet, "/api/v1/videos/"+privateVideo.ID, nil, ownerToken)
	if ownerPrivate.Code != http.StatusOK {
		t.Fatalf("owner private read: status=%d body=%s", ownerPrivate.Code, ownerPrivate.Body.String())
	}

	otherUpdate := performJSON(t, router, http.MethodPatch, "/api/v1/videos/"+privateVideo.ID, map[string]any{
		"title": "Stolen title",
	}, otherToken)
	if otherUpdate.Code != http.StatusForbidden {
		t.Fatalf("other update: status=%d body=%s", otherUpdate.Code, otherUpdate.Body.String())
	}
	publicUpdate := performJSON(t, router, http.MethodPatch, "/api/v1/videos/"+privateVideo.ID, map[string]any{
		"title": "Published metadata", "visibility": "public",
	}, ownerToken)
	updated := decodeVideo(t, publicUpdate, http.StatusOK)
	if updated.Visibility != video.VisibilityPublic || updated.Title != "Published metadata" {
		t.Fatalf("updated video = %#v", updated)
	}

	secondPublicResponse := performJSON(t, router, http.MethodPost, "/api/v1/videos", map[string]any{
		"title": "Second public video", "visibility": "PUBLIC",
	}, ownerToken)
	secondPublic := decodeVideo(t, secondPublicResponse, http.StatusCreated)
	unlistedResponse := performJSON(t, router, http.MethodPost, "/api/v1/videos", map[string]any{
		"title": "Unlisted video", "visibility": "UNLISTED",
	}, ownerToken)
	unlisted := decodeVideo(t, unlistedResponse, http.StatusCreated)

	publicPageOne := performJSON(t, router, http.MethodGet, "/api/v1/videos?limit=1", nil, "")
	firstPage := decodeVideoPage(t, publicPageOne)
	if len(firstPage.Items) != 1 || firstPage.NextCursor == "" {
		t.Fatalf("first public page = %#v", firstPage)
	}
	publicPageTwo := performJSON(t, router, http.MethodGet, "/api/v1/videos?limit=1&cursor="+url.QueryEscape(firstPage.NextCursor), nil, "")
	secondPage := decodeVideoPage(t, publicPageTwo)
	if len(secondPage.Items) != 1 || secondPage.Items[0].ID == firstPage.Items[0].ID {
		t.Fatalf("second public page = %#v", secondPage)
	}
	publicIDs := map[string]bool{firstPage.Items[0].ID: true, secondPage.Items[0].ID: true}
	if !publicIDs[privateVideo.ID] || !publicIDs[secondPublic.ID] || publicIDs[unlisted.ID] {
		t.Fatalf("public IDs = %#v", publicIDs)
	}

	invalidCursor := performJSON(t, router, http.MethodGet, "/api/v1/videos?cursor=invalid***", nil, "")
	if invalidCursor.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor: status=%d body=%s", invalidCursor.Code, invalidCursor.Body.String())
	}
	mine := performJSON(t, router, http.MethodGet, "/api/v1/users/me/videos?limit=10", nil, ownerToken)
	minePage := decodeVideoPage(t, mine)
	if len(minePage.Items) != 3 {
		t.Fatalf("owner video count = %d, want 3", len(minePage.Items))
	}

	otherDelete := performJSON(t, router, http.MethodDelete, "/api/v1/videos/"+unlisted.ID, nil, otherToken)
	if otherDelete.Code != http.StatusForbidden {
		t.Fatalf("other delete: status=%d body=%s", otherDelete.Code, otherDelete.Body.String())
	}
	ownerDelete := performJSON(t, router, http.MethodDelete, "/api/v1/videos/"+unlisted.ID, nil, ownerToken)
	if ownerDelete.Code != http.StatusNoContent {
		t.Fatalf("owner delete: status=%d body=%s", ownerDelete.Code, ownerDelete.Body.String())
	}
	afterDelete := performJSON(t, router, http.MethodGet, "/api/v1/videos/"+unlisted.ID, nil, ownerToken)
	if afterDelete.Code != http.StatusNotFound {
		t.Fatalf("read deleted video: status=%d body=%s", afterDelete.Code, afterDelete.Body.String())
	}
	var softDeleted bool
	if err := pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM videos WHERE id = $1`, unlisted.ID).Scan(&softDeleted); err != nil {
		t.Fatalf("query deleted video: %v", err)
	}
	if !softDeleted {
		t.Fatal("video was not soft-deleted")
	}
}

func registerAndLogin(t *testing.T, handler http.Handler, username, email string) string {
	t.Helper()
	registration := performJSON(t, handler, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": username, "email": email, "password": "correct horse battery staple",
	}, "")
	if registration.Code != http.StatusCreated {
		t.Fatalf("register %s: status=%d body=%s", username, registration.Code, registration.Body.String())
	}
	login := performJSON(t, handler, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email": email, "password": "correct horse battery staple",
	}, "")
	return decodeSession(t, login).AccessToken
}

type decodedVideo struct {
	ID               string `json:"id"`
	Visibility       string `json:"visibility"`
	ProcessingStatus string `json:"processing_status"`
	Title            string `json:"title"`
	ViewCount        int64  `json:"view_count"`
}

func decodeVideo(t *testing.T, response *httptest.ResponseRecorder, expectedStatus int) decodedVideo {
	t.Helper()
	if response.Code != expectedStatus {
		t.Fatalf("video response: status=%d body=%s", response.Code, response.Body.String())
	}
	var result decodedVideo
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode video: %v", err)
	}
	if result.ID == "" {
		t.Fatal("video response has no ID")
	}
	return result
}

type decodedVideoPage struct {
	Items      []decodedVideo `json:"items"`
	NextCursor string         `json:"next_cursor"`
}

func decodeVideoPage(t *testing.T, response *httptest.ResponseRecorder) decodedVideoPage {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("video page: status=%d body=%s", response.Code, response.Body.String())
	}
	var page decodedVideoPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode video page: %v", err)
	}
	return page
}
