package user

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/streamforge/streamforge/internal/platform/web"
)

type CurrentUserID func(context.Context) (uuid.UUID, bool)

type Handler struct {
	service       *Service
	authenticate  func(http.Handler) http.Handler
	currentUserID CurrentUserID
}

func NewHandler(service *Service, authenticate func(http.Handler) http.Handler, currentUserID CurrentUserID) *Handler {
	return &Handler{service: service, authenticate: authenticate, currentUserID: currentUserID}
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	router.Get("/users/{username}", h.getPublicProfile)
	router.Group(func(protected chi.Router) {
		protected.Use(h.authenticate)
		protected.Get("/users/me", h.getMe)
		protected.Patch("/users/me", h.updateMe)
	})
}

type publicUserResponse struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Bio         string    `json:"bio"`
	CreatedAt   string    `json:"created_at"`
}

type privateUserResponse struct {
	publicUserResponse
	Email     string `json:"email"`
	Role      string `json:"role"`
	UpdatedAt string `json:"updated_at"`
}

func (h *Handler) getPublicProfile(w http.ResponseWriter, r *http.Request) {
	found, err := h.service.PublicProfile(r.Context(), chi.URLParam(r, "username"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, toPublicResponse(found))
}

func (h *Handler) getMe(w http.ResponseWriter, r *http.Request) {
	id, ok := h.currentUserID(r.Context())
	if !ok {
		web.WriteError(w, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required")
		return
	}
	found, err := h.service.Me(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, toPrivateResponse(found))
}

type updateProfileRequest struct {
	DisplayName *string `json:"display_name"`
	Bio         *string `json:"bio"`
}

func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) {
	var request updateProfileRequest
	if err := web.DecodeJSON(w, r, &request); err != nil {
		web.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	id, ok := h.currentUserID(r.Context())
	if !ok {
		web.WriteError(w, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required")
		return
	}
	updated, err := h.service.UpdateProfile(r.Context(), id, UpdateProfileParams(request))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, toPrivateResponse(updated))
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		web.WriteError(w, http.StatusNotFound, "USER_NOT_FOUND", "The requested user was not found")
	case errors.Is(err, ErrInvalidInput):
		web.WriteError(w, http.StatusBadRequest, "VALIDATION_FAILED", strings.TrimPrefix(err.Error(), ErrInvalidInput.Error()+": "))
	default:
		web.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred")
	}
}

func toPublicResponse(found User) publicUserResponse {
	return publicUserResponse{
		ID:          found.ID,
		Username:    found.Username,
		DisplayName: found.DisplayName,
		Bio:         found.Bio,
		CreatedAt:   found.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func toPrivateResponse(found User) privateUserResponse {
	return privateUserResponse{
		publicUserResponse: toPublicResponse(found),
		Email:              found.Email,
		Role:               found.Role,
		UpdatedAt:          found.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}
