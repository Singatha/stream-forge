package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/streamforge/streamforge/internal/platform/web"
	"github.com/streamforge/streamforge/internal/user"
)

type Handler struct {
	service       *Service
	loginLimit    func(http.Handler) http.Handler
	registerLimit func(http.Handler) http.Handler
}

func NewHandler(service *Service, loginLimit, registerLimit func(http.Handler) http.Handler) *Handler {
	return &Handler{service: service, loginLimit: loginLimit, registerLimit: registerLimit}
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	router.Route("/auth", func(authRouter chi.Router) {
		authRouter.With(h.registerLimit).Post("/register", h.register)
		authRouter.With(h.loginLimit).Post("/login", h.login)
		authRouter.Post("/refresh", h.refresh)
		authRouter.Post("/logout", h.logout)
	})
}

type registerRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type userResponse struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	Role        string    `json:"role"`
	DisplayName string    `json:"display_name"`
	Bio         string    `json:"bio"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type registrationResponse struct {
	User userResponse `json:"user"`
}

type sessionResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	TokenType    string       `json:"token_type"`
	ExpiresIn    int64        `json:"expires_in"`
	User         userResponse `json:"user"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var request registerRequest
	if err := web.DecodeJSON(w, r, &request); err != nil {
		web.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	created, err := h.service.Register(r.Context(), RegisterInput(request))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	web.WriteJSON(w, http.StatusCreated, registrationResponse{User: toUserResponse(created)})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := web.DecodeJSON(w, r, &request); err != nil {
		web.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	session, err := h.service.Login(r.Context(), LoginInput(request))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, toSessionResponse(session))
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var request refreshRequest
	if err := web.DecodeJSON(w, r, &request); err != nil {
		web.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	session, err := h.service.Refresh(r.Context(), request.RefreshToken)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, toSessionResponse(session))
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var request refreshRequest
	if err := web.DecodeJSON(w, r, &request); err != nil {
		web.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if err := h.service.Logout(r.Context(), request.RefreshToken); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		web.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", strings.TrimPrefix(err.Error(), ErrInvalidInput.Error()+": "))
	case errors.Is(err, user.ErrEmailTaken):
		web.WriteError(w, http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "An account with that email already exists")
	case errors.Is(err, user.ErrUsernameTaken):
		web.WriteError(w, http.StatusConflict, "USERNAME_ALREADY_REGISTERED", "That username is already in use")
	case errors.Is(err, ErrInvalidCredentials):
		web.WriteError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "The email or password is incorrect")
	case errors.Is(err, ErrInvalidRefreshToken):
		web.WriteError(w, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "The refresh token is invalid or expired")
	case errors.Is(err, ErrRefreshTokenReuse):
		web.WriteError(w, http.StatusUnauthorized, "REFRESH_TOKEN_REUSED", "Refresh token reuse was detected and the session was revoked")
	default:
		slog.ErrorContext(r.Context(), "authentication request failed", "error", err)
		web.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred")
	}
}

func toUserResponse(account user.User) userResponse {
	return userResponse{
		ID: account.ID, Username: account.Username, Email: account.Email, Role: account.Role,
		DisplayName: account.DisplayName, Bio: account.Bio,
		CreatedAt: account.CreatedAt.UTC(), UpdatedAt: account.UpdatedAt.UTC(),
	}
}

func toSessionResponse(session Session) sessionResponse {
	expiresIn := int64(time.Until(session.ExpiresAt).Seconds())
	if expiresIn < 0 {
		expiresIn = 0
	}
	return sessionResponse{
		AccessToken: session.AccessToken, RefreshToken: session.RefreshToken,
		TokenType: "Bearer", ExpiresIn: expiresIn, User: toUserResponse(session.User),
	}
}
