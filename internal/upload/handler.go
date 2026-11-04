package upload

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/streamforge/streamforge/internal/auth"
	"github.com/streamforge/streamforge/internal/platform/web"
)

type Metrics struct {
	videosUploaded prometheus.Counter
}

func NewMetrics(registerer prometheus.Registerer) *Metrics {
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "videos_uploaded_total",
		Help: "Total number of source video uploads successfully verified.",
	})
	registerer.MustRegister(counter)
	return &Metrics{videosUploaded: counter}
}

type Handler struct {
	service      *Service
	authenticate func(http.Handler) http.Handler
	rateLimit    func(http.Handler) http.Handler
	metrics      *Metrics
}

func NewHandler(
	service *Service,
	authenticate func(http.Handler) http.Handler,
	rateLimit func(http.Handler) http.Handler,
	metrics *Metrics,
) *Handler {
	return &Handler{service: service, authenticate: authenticate, rateLimit: rateLimit, metrics: metrics}
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	router.With(h.authenticate, h.rateLimit).Post("/videos/{id}/upload-url", h.requestUpload)
	router.With(h.authenticate).Post("/videos/{id}/upload-complete", h.completeUpload)
}

type requestUploadRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type uploadTicketResponse struct {
	VideoID         uuid.UUID         `json:"video_id"`
	AssetID         uuid.UUID         `json:"asset_id"`
	ObjectKey       string            `json:"object_key"`
	UploadURL       string            `json:"upload_url"`
	Method          string            `json:"method"`
	RequiredHeaders map[string]string `json:"required_headers"`
	ExpiresAt       time.Time         `json:"expires_at"`
}

type completionResponse struct {
	VideoID          uuid.UUID  `json:"video_id"`
	AssetID          uuid.UUID  `json:"asset_id"`
	ProcessingStatus string     `json:"processing_status"`
	StorageStatus    string     `json:"storage_status"`
	ContentType      string     `json:"content_type"`
	SizeBytes        int64      `json:"size_bytes"`
	CompletedAt      *time.Time `json:"completed_at"`
}

func (h *Handler) requestUpload(w http.ResponseWriter, r *http.Request) {
	videoID, ok := parseVideoID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var request requestUploadRequest
	if err := web.DecodeJSON(w, r, &request); err != nil {
		web.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	actor, ok := actorFromContext(r)
	if !ok {
		web.WriteError(w, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required")
		return
	}
	ticket, err := h.service.RequestUpload(r.Context(), videoID, actor, RequestInput(request))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	headers := make(map[string]string, len(ticket.RequiredHeaders))
	for key, values := range ticket.RequiredHeaders {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	web.WriteJSON(w, http.StatusOK, uploadTicketResponse{
		VideoID: ticket.VideoID, AssetID: ticket.AssetID, ObjectKey: ticket.ObjectKey,
		UploadURL: ticket.UploadURL, Method: http.MethodPut, RequiredHeaders: headers,
		ExpiresAt: ticket.ExpiresAt,
	})
}

func (h *Handler) completeUpload(w http.ResponseWriter, r *http.Request) {
	videoID, ok := parseVideoID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	actor, ok := actorFromContext(r)
	if !ok {
		web.WriteError(w, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required")
		return
	}
	completion, err := h.service.CompleteUpload(r.Context(), videoID, actor)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	if completion.NewlyCompleted {
		h.metrics.videosUploaded.Inc()
	}
	contentType := ""
	if completion.Asset.ContentType != nil {
		contentType = *completion.Asset.ContentType
	}
	var size int64
	if completion.Asset.SizeBytes != nil {
		size = *completion.Asset.SizeBytes
	}
	web.WriteJSON(w, http.StatusOK, completionResponse{
		VideoID: completion.Video.ID, AssetID: completion.Asset.ID,
		ProcessingStatus: completion.Video.ProcessingStatus,
		StorageStatus:    completion.Asset.StorageStatus, ContentType: contentType,
		SizeBytes: size, CompletedAt: completion.Asset.CompletedAt,
	})
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		web.WriteError(w, http.StatusNotFound, "VIDEO_OR_UPLOAD_NOT_FOUND", "The requested video or source upload was not found")
	case errors.Is(err, ErrForbidden):
		web.WriteError(w, http.StatusForbidden, "UPLOAD_FORBIDDEN", "You are not authorized to upload this video")
	case errors.Is(err, ErrInvalidInput):
		message := strings.TrimPrefix(err.Error(), ErrInvalidInput.Error()+": ")
		web.WriteError(w, http.StatusUnprocessableEntity, "UPLOAD_VALIDATION_FAILED", message)
	case errors.Is(err, ErrUploadConflict):
		web.WriteError(w, http.StatusConflict, "UPLOAD_METADATA_CONFLICT", "An upload already exists with different file metadata")
	case errors.Is(err, ErrInvalidState):
		web.WriteError(w, http.StatusConflict, "INVALID_UPLOAD_STATE", "The video is not in a state that permits this upload operation")
	case errors.Is(err, ErrObjectMissing):
		web.WriteError(w, http.StatusUnprocessableEntity, "UPLOAD_NOT_FOUND", "The source object has not been uploaded")
	case errors.Is(err, ErrObjectMismatch):
		web.WriteError(w, http.StatusUnprocessableEntity, "UPLOAD_METADATA_MISMATCH", "The uploaded object's size or content type does not match the upload request")
	case errors.Is(err, ErrStorageUnavailable):
		slog.ErrorContext(r.Context(), "object storage request failed", "error", err)
		web.WriteError(w, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "Object storage is temporarily unavailable")
	default:
		slog.ErrorContext(r.Context(), "upload request failed", "error", err)
		web.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred")
	}
}

func actorFromContext(r *http.Request) (Actor, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return Actor{}, false
	}
	return Actor{UserID: principal.UserID, Role: principal.Role}, true
}

func parseVideoID(w http.ResponseWriter, value string) (uuid.UUID, bool) {
	id, err := uuid.Parse(value)
	if err != nil {
		web.WriteError(w, http.StatusBadRequest, "INVALID_VIDEO_ID", "The video ID must be a valid UUID")
		return uuid.Nil, false
	}
	return id, true
}
