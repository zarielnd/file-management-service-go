package auth

import (
	"encoding/json"
	"net/http"

	"github.com/zarielnd/file-management-service-go/services/file-server/internal/apperror"
	"github.com/zarielnd/file-management-service-go/services/file-server/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(s *Service) *Handler {
	return &Handler{service: s}
}

type signupReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type signinReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) SignUp(w http.ResponseWriter, r *http.Request) {
	var req signupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperror.Invalid("invalid json"))
		return
	}
	if err := h.service.SignUp(r.Context(), req.Email, req.Password); err != nil {
		httpx.WriteError(w, apperror.Invalid(err.Error()))
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) SignIn(w http.ResponseWriter, r *http.Request) {
	var req signinReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperror.Invalid("invalid json"))
		return
	}
	access, refresh, err := h.service.SignIn(r.Context(), req.Email, req.Password)
	if err != nil {
		httpx.WriteError(w, apperror.Unauthorized("invalid credentials"))
		return
	}
	setAuthCookies(w, access, refresh)
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	// Blacklist old access token before rotation
	if c, err := r.Cookie("access_token"); err == nil && c.Value != "" {
		_ = h.service.BlacklistToken(r.Context(), c.Value)
	}

	cookie, err := r.Cookie("refresh_token")
	if err != nil {
		httpx.WriteError(w, apperror.Unauthorized("missing refresh token"))
		return
	}
	access, refresh, err := h.service.Refresh(r.Context(), cookie.Value)
	if err != nil {
		httpx.WriteError(w, apperror.Unauthorized("invalid refresh token"))
		return
	}
	setAuthCookies(w, access, refresh)
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var accessToken string
	if c, err := r.Cookie("access_token"); err == nil {
		accessToken = c.Value
	}
	if accessToken != "" {
		_ = h.service.Logout(r.Context(), accessToken)
	}
	clearAuthCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value("user_id").(string)
	if userID == "" {
		httpx.WriteError(w, apperror.Unauthorized("missing user"))
		return
	}
	if err := h.service.LogoutAll(r.Context(), userID); err != nil {
		httpx.WriteError(w, apperror.Internal("logout failed"))
		return
	}
	clearAuthCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

func setAuthCookies(w http.ResponseWriter, access, refresh string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    access,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
		MaxAge:   900,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refresh,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
		MaxAge:   604800,
	})
}

func clearAuthCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "", Path: "/", MaxAge: -1})
}
