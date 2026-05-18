// Package handler exposes HTTP endpoints for the IAM service.
// Handlers are thin: parse → validate → service → respond.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/respond"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-iam/internal/model"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-iam/internal/service"
)

// H wraps the service.
type H struct {
	svc *service.IAM
}

// New returns a handler set.
func New(s *service.IAM) *H { return &H{svc: s} }

// Mount installs routes onto mux.
func (h *H) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/signup", h.signup)
	mux.HandleFunc("POST /v1/auth/login", h.login)
	mux.HandleFunc("POST /v1/auth/refresh", h.refresh)
	mux.HandleFunc("POST /v1/auth/logout", h.logout)
	mux.HandleFunc("GET /v1/me", h.me)
}

func (h *H) signup(w http.ResponseWriter, r *http.Request) {
	var req model.SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	u, err := h.svc.Signup(r.Context(), req)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	respond.JSON(w, http.StatusCreated, u)
}

func (h *H) login(w http.ResponseWriter, r *http.Request) {
	var req model.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	tp, err := h.svc.Login(r.Context(), req)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, tp)
}

func (h *H) refresh(w http.ResponseWriter, r *http.Request) {
	var req model.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	tp, err := h.svc.Refresh(r.Context(), req.RefreshToken, r.Header.Get("X-Device-ID"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, tp)
}

func (h *H) logout(w http.ResponseWriter, r *http.Request) {
	var req model.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	if err := h.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		writeServiceErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *H) me(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.FromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "no token")
		return
	}
	u, err := h.svc.Me(r.Context(), claims.UserID)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, u)
}

// writeServiceErr maps sentinel errors to HTTP status codes.
func writeServiceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrUserNotFound), errors.Is(err, model.ErrOrgNotFound), errors.Is(err, model.ErrTokenNotFound):
		respond.Error(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, model.ErrUserExists):
		respond.Error(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, model.ErrBadCredentials), errors.Is(err, model.ErrTokenRevoked), errors.Is(err, model.ErrTokenExpired), errors.Is(err, model.ErrTokenReuse):
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
	case errors.Is(err, model.ErrInvalidArgument):
		respond.Error(w, http.StatusBadRequest, "bad_request", err.Error())
	default:
		respond.DBError(w, err)
	}
}
