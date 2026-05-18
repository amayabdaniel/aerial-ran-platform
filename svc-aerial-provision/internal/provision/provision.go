// Package provision is the single-package implementation of the provisioning
// service: model + repository + service + handler, kept compact because the
// surface area is small.
package provision

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/respond"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ───────── model ─────────

var (
	ErrPlanNotFound     = errors.New("plan not found")
	ErrSubNotFound      = errors.New("subscription not found")
	ErrSubExists        = errors.New("subscription already exists for user+plan")
)

type Plan struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	MonthlyCents int       `json:"monthly_cents"`
	DataCapMB    *int      `json:"data_cap_mb,omitempty"`
	Description  string    `json:"description"`
	CreatedAt    time.Time `json:"created_at"`
}

type Subscription struct {
	ID          string     `json:"id"`
	OrgID       string     `json:"org_id"`
	UserID      string     `json:"user_id"`
	PlanID      string     `json:"plan_id"`
	SIMID       *string    `json:"sim_id,omitempty"`
	ESIMID      *string    `json:"esim_id,omitempty"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CreateSubRequest struct {
	UserID string  `json:"user_id"`
	PlanID string  `json:"plan_id"`
	SIMID  *string `json:"sim_id,omitempty"`
	ESIMID *string `json:"esim_id,omitempty"`
}

// ───────── service ─────────

type Service struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func (s *Service) ListPlans(ctx context.Context) ([]Plan, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, monthly_cents, data_cap_mb, COALESCE(description,''), created_at
		   FROM provision.plans ORDER BY monthly_cents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		var p Plan
		if err := rows.Scan(&p.ID, &p.Name, &p.MonthlyCents, &p.DataCapMB, &p.Description, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) GetPlan(ctx context.Context, id string) (*Plan, error) {
	p := &Plan{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, monthly_cents, data_cap_mb, COALESCE(description,''), created_at
		   FROM provision.plans WHERE id = $1`, id,
	).Scan(&p.ID, &p.Name, &p.MonthlyCents, &p.DataCapMB, &p.Description, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlanNotFound
	}
	return p, err
}

func (s *Service) CreateSubscription(ctx context.Context, orgID string, req CreateSubRequest) (*Subscription, error) {
	if _, err := s.GetPlan(ctx, req.PlanID); err != nil {
		return nil, err
	}
	sub := &Subscription{OrgID: orgID, UserID: req.UserID, PlanID: req.PlanID, SIMID: req.SIMID, ESIMID: req.ESIMID, Status: "active"}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO provision.subscriptions(org_id, user_id, plan_id, sim_id, esim_id, status)
		 VALUES ($1::uuid, $2::uuid, $3, NULLIF($4,'')::uuid, NULLIF($5,'')::uuid, 'active')
		 RETURNING id::text, started_at, created_at, updated_at`,
		sub.OrgID, sub.UserID, sub.PlanID, ptr(sub.SIMID), ptr(sub.ESIMID),
	).Scan(&sub.ID, &sub.StartedAt, &sub.CreatedAt, &sub.UpdatedAt)
	return sub, err
}

func (s *Service) ListByOrg(ctx context.Context, orgID string) ([]Subscription, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, org_id::text, user_id::text, plan_id, sim_id::text, esim_id::text, status,
		        started_at, cancelled_at, created_at, updated_at
		   FROM provision.subscriptions WHERE org_id = $1::uuid ORDER BY created_at DESC LIMIT 200`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		var sub Subscription
		if err := rows.Scan(&sub.ID, &sub.OrgID, &sub.UserID, &sub.PlanID, &sub.SIMID, &sub.ESIMID, &sub.Status,
			&sub.StartedAt, &sub.CancelledAt, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

func (s *Service) Cancel(ctx context.Context, id string) error {
	res, err := s.pool.Exec(ctx,
		`UPDATE provision.subscriptions SET status='cancelled', cancelled_at=now(), updated_at=now()
		   WHERE id=$1::uuid AND status<>'cancelled'`, id)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrSubNotFound
	}
	return nil
}

// ───────── handler ─────────

type Handler struct{ svc *Service }

func NewHandler(s *Service) *Handler { return &Handler{svc: s} }

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/plans", h.listPlans)
	mux.HandleFunc("GET /v1/plans/{id}", h.getPlan)
	mux.HandleFunc("POST /v1/subscriptions", h.createSub)
	mux.HandleFunc("GET /v1/subscriptions", h.listSubs)
	mux.HandleFunc("DELETE /v1/subscriptions/{id}", h.cancelSub)
}

func (h *Handler) listPlans(w http.ResponseWriter, r *http.Request) {
	ps, err := h.svc.ListPlans(r.Context())
	if err != nil {
		respond.DBError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, ps)
}

func (h *Handler) getPlan(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetPlan(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrPlanNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "plan not found")
		return
	}
	if err != nil {
		respond.DBError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, p)
}

func (h *Handler) createSub(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.FromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "no token")
		return
	}
	var req CreateSubRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	if req.UserID == "" {
		req.UserID = claims.UserID
	}
	sub, err := h.svc.CreateSubscription(r.Context(), claims.OrgID, req)
	if errors.Is(err, ErrPlanNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "plan not found")
		return
	}
	if err != nil {
		respond.DBError(w, err)
		return
	}
	respond.JSON(w, http.StatusCreated, sub)
}

func (h *Handler) listSubs(w http.ResponseWriter, r *http.Request) {
	claims, _ := jwt.FromContext(r.Context())
	subs, err := h.svc.ListByOrg(r.Context(), claims.OrgID)
	if err != nil {
		respond.DBError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, subs)
}

func (h *Handler) cancelSub(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Cancel(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, ErrSubNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		respond.DBError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func ptr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
