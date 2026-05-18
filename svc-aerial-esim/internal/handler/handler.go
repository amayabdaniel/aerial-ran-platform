// Package handler exposes eSIM HTTP endpoints.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/respond"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-esim/internal/model"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-esim/internal/service"
)

type H struct{ svc *service.ESIM }

func New(s *service.ESIM) *H { return &H{svc: s} }

func (h *H) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/catalog/refresh", h.refreshCatalog)
	mux.HandleFunc("GET /v1/packages", h.listPackages)
	mux.HandleFunc("POST /v1/esims", h.order)
	mux.HandleFunc("GET /v1/esims", h.list)
	mux.HandleFunc("GET /v1/esims/{id}", h.get)
	mux.HandleFunc("POST /v1/esims/{id}/usage/refresh", h.refreshUsage)
	mux.HandleFunc("POST /v1/esims/{id}/activate", h.activate)
	mux.HandleFunc("DELETE /v1/esims/{id}", h.cancel)
}

func (h *H) refreshCatalog(w http.ResponseWriter, r *http.Request) {
	region := r.URL.Query().Get("region")
	pkgs, err := h.svc.RefreshCatalog(r.Context(), region)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, pkgs)
}

func (h *H) listPackages(w http.ResponseWriter, r *http.Request) {
	region := r.URL.Query().Get("region")
	pkgs, err := h.svc.ListPackages(r.Context(), region)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, pkgs)
}

func (h *H) order(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.FromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "no token")
		return
	}
	var req model.OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	e, err := h.svc.Order(r.Context(), claims.OrgID, req)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	respond.JSON(w, http.StatusCreated, e)
}

func (h *H) list(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.FromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "no token")
		return
	}
	es, err := h.svc.ListByOrg(r.Context(), claims.OrgID)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, es)
}

func (h *H) get(w http.ResponseWriter, r *http.Request) {
	e, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, e)
}

func (h *H) refreshUsage(w http.ResponseWriter, r *http.Request) {
	e, err := h.svc.RefreshUsage(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, e)
}

func (h *H) activate(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.MarkActivated(r.Context(), r.PathValue("id")); err != nil {
		writeServiceErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *H) cancel(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Cancel(r.Context(), r.PathValue("id")); err != nil {
		writeServiceErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeServiceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrESIMNotFound), errors.Is(err, model.ErrPackageNotFound):
		respond.Error(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, model.ErrBadInput):
		respond.Error(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, model.ErrProvider):
		respond.Error(w, http.StatusBadGateway, "provider_error", err.Error())
	default:
		respond.DBError(w, err)
	}
}
