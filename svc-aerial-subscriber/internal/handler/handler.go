// Package handler exposes SIM HTTP endpoints.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/respond"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/model"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/service"
)

type H struct{ svc *service.SIM }

func New(s *service.SIM) *H { return &H{svc: s} }

func (h *H) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/sims", h.create)
	mux.HandleFunc("GET /v1/sims", h.list)
	mux.HandleFunc("GET /v1/sims/{id}", h.get)
	mux.HandleFunc("POST /v1/sims/{id}/suspend", h.suspend)
	mux.HandleFunc("POST /v1/sims/{id}/resume", h.resume)
	mux.HandleFunc("DELETE /v1/sims/{id}", h.terminate)
}

func (h *H) create(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.FromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "no token")
		return
	}
	var req model.CreateSIMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	sim, err := h.svc.Create(r.Context(), claims.OrgID, req)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	respond.JSON(w, http.StatusCreated, sim)
}

func (h *H) list(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.FromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "no token")
		return
	}
	sims, err := h.svc.ListByOrg(r.Context(), claims.OrgID)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, sims)
}

func (h *H) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sim, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, sim)
}

func (h *H) suspend(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Suspend(r.Context(), r.PathValue("id")); err != nil {
		writeServiceErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *H) resume(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Resume(r.Context(), r.PathValue("id")); err != nil {
		writeServiceErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *H) terminate(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Terminate(r.Context(), r.PathValue("id")); err != nil {
		writeServiceErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeServiceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrSIMNotFound):
		respond.Error(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, model.ErrSIMExists):
		respond.Error(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, model.ErrBadInput):
		respond.Error(w, http.StatusBadRequest, "bad_request", err.Error())
	default:
		respond.DBError(w, err)
	}
}
