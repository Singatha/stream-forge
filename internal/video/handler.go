package video

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/streamforge/streamforge/internal/auth"
	"github.com/streamforge/streamforge/internal/platform/web"
)

type Handler struct {
	service              *Service
	authenticate         func(http.Handler) http.Handler
	optionalAuthenticate func(http.Handler) http.Handler
}

func NewHandler(
	service *Service,
	authenticate func(http.Handler) http.Handler,
	optionalAuthenticate func(http.Handler) http.Handler,
) *Handler {
	return &Handler{service: service, authenticate: authenticate, optionalAuthenticate: optionalAuthenticate}
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	router.Get("/videos", h.listPublic)
	router.With(h.authenticate).Post("/videos", h.create)
	router.With(h.optionalAuthenticate).Get("/videos/{id}", h.get)
	router.With(h.authenticate).Patch("/videos/{id}", h.update)
	router.With(h.authenticate).Delete("/videos/{id}", h.delete)
	router.With(h.authenticate).Get("/users/me/videos", h.listMine)
}

type createRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
}

type updateRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Visibility  *string `json:"visibility"`
}

type response struct {
	ID               uuid.UUID  `json:"id"`
	OwnerID          uuid.UUID  `json:"owner_id"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Visibility       string     `json:"visibility"`
	ProcessingStatus string     `json:"processing_status"`
	ViewCount        int64      `json:"view_count"`
	PublishedAt      *time.Time `json:"published_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type pageResponse struct {
	Items      []response `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var request createRequest
	if err := web.DecodeJSON(w, r, &request); err != nil {
		web.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	actor, ok := actorFromContext(r)
	if !ok {
		web.WriteError(w, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required")
		return
	}
	created, err := h.service.Create(r.Context(), actor, CreateInput(request))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	web.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var actor *Actor
	if current, authenticated := actorFromContext(r); authenticated {
		actor = &current
	}
	found, err := h.service.Get(r.Context(), id, actor)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, toResponse(found))
}

func (h *Handler) listPublic(w http.ResponseWriter, r *http.Request) {
	limit, cursor, ok := parsePage(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListPublic(r.Context(), limit, cursor)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, toPageResponse(page))
}

func (h *Handler) listMine(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r)
	if !ok {
		web.WriteError(w, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required")
		return
	}
	limit, cursor, ok := parsePage(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListOwned(r.Context(), actor, limit, cursor)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, toPageResponse(page))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var request updateRequest
	if err := web.DecodeJSON(w, r, &request); err != nil {
		web.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	actor, ok := actorFromContext(r)
	if !ok {
		web.WriteError(w, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required")
		return
	}
	updated, err := h.service.Update(r.Context(), id, actor, UpdateInput(request))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, toResponse(updated))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	actor, ok := actorFromContext(r)
	if !ok {
		web.WriteError(w, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required")
		return
	}
	if err := h.service.Delete(r.Context(), id, actor); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		web.WriteError(w, http.StatusNotFound, "VIDEO_NOT_FOUND", "The requested video was not found")
	case errors.Is(err, ErrForbidden):
		web.WriteError(w, http.StatusForbidden, "VIDEO_FORBIDDEN", "You are not authorized to modify this video")
	case IsValidationError(err):
		message := strings.TrimPrefix(err.Error(), ErrInvalidInput.Error()+": ")
		message = strings.TrimPrefix(message, ErrInvalidCursor.Error()+": ")
		web.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", message)
	default:
		slog.ErrorContext(r.Context(), "video request failed", "error", err)
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

func parseID(w http.ResponseWriter, value string) (uuid.UUID, bool) {
	id, err := uuid.Parse(value)
	if err != nil {
		web.WriteError(w, http.StatusBadRequest, "INVALID_VIDEO_ID", "The video ID must be a valid UUID")
		return uuid.Nil, false
	}
	return id, true
}

func parsePage(w http.ResponseWriter, r *http.Request) (int, *Cursor, bool) {
	limit := DefaultPageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > MaxPageSize {
			web.WriteError(w, http.StatusBadRequest, "INVALID_PAGINATION", "limit must be an integer between 1 and 100")
			return 0, nil, false
		}
		limit = parsed
	}
	cursor, err := DecodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		web.WriteError(w, http.StatusBadRequest, "INVALID_CURSOR", "The pagination cursor is invalid")
		return 0, nil, false
	}
	return limit, cursor, true
}

func toResponse(found Video) response {
	return response{
		ID: found.ID, OwnerID: found.OwnerID, Title: found.Title, Description: found.Description,
		Visibility: found.Visibility, ProcessingStatus: found.ProcessingStatus, ViewCount: found.ViewCount,
		PublishedAt: found.PublishedAt, CreatedAt: found.CreatedAt.UTC(), UpdatedAt: found.UpdatedAt.UTC(),
	}
}

func toPageResponse(page Page) pageResponse {
	items := make([]response, 0, len(page.Items))
	for _, found := range page.Items {
		items = append(items, toResponse(found))
	}
	return pageResponse{Items: items, NextCursor: page.NextCursor}
}
