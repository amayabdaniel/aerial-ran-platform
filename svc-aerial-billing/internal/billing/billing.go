// Package billing accepts usage events, recomputes monthly rollups, exposes
// /v1/usage (per-user) and /v1/usage/org (org totals).
package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/respond"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UsageEvent struct {
	OrgID      string    `json:"org_id"`
	UserID     *string   `json:"user_id,omitempty"`
	Source     string    `json:"source"`
	ResourceID *string   `json:"resource_id,omitempty"`
	DataMB     int       `json:"data_mb"`
	Minutes    int       `json:"minutes"`
	SMSCount   int       `json:"sms_count"`
	Cents      int       `json:"cents"`
	OccurredAt time.Time `json:"occurred_at"`
}

type Rollup struct {
	OrgID     string    `json:"org_id"`
	UserID    *string   `json:"user_id,omitempty"`
	Month     time.Time `json:"month"`
	DataMB    int       `json:"data_mb"`
	Minutes   int       `json:"minutes"`
	SMSCount  int       `json:"sms_count"`
	Cents     int       `json:"cents"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Service struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// Ingest writes a raw event and updates the rollup row in the same transaction.
func (s *Service) Ingest(ctx context.Context, e UsageEvent) error {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`INSERT INTO billing.usage_events(org_id, user_id, source, resource_id, data_mb, minutes, sms_count, cents, occurred_at)
		 VALUES ($1::uuid, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8, $9)`,
		e.OrgID, ptr(e.UserID), e.Source, ptr(e.ResourceID), e.DataMB, e.Minutes, e.SMSCount, e.Cents, e.OccurredAt,
	); err != nil {
		return err
	}
	monthStart := time.Date(e.OccurredAt.Year(), e.OccurredAt.Month(), 1, 0, 0, 0, 0, time.UTC)
	rollupUser := ptr(e.UserID)
	if rollupUser == "" {
		rollupUser = "00000000-0000-0000-0000-000000000000"
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO billing.usage_rollups(org_id, user_id, month, data_mb, minutes, sms_count, cents)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7)
		 ON CONFLICT (org_id, user_id, month) DO UPDATE
		   SET data_mb   = billing.usage_rollups.data_mb   + EXCLUDED.data_mb,
		       minutes   = billing.usage_rollups.minutes   + EXCLUDED.minutes,
		       sms_count = billing.usage_rollups.sms_count + EXCLUDED.sms_count,
		       cents     = billing.usage_rollups.cents     + EXCLUDED.cents,
		       updated_at = now()`,
		e.OrgID, rollupUser, monthStart, e.DataMB, e.Minutes, e.SMSCount, e.Cents,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) MyMonth(ctx context.Context, orgID, userID string) (*Rollup, error) {
	monthStart := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.UTC)
	r := &Rollup{OrgID: orgID, UserID: &userID, Month: monthStart}
	err := s.pool.QueryRow(ctx,
		`SELECT data_mb, minutes, sms_count, cents, updated_at
		   FROM billing.usage_rollups
		  WHERE org_id = $1::uuid AND user_id = $2::uuid AND month = $3`,
		orgID, userID, monthStart,
	).Scan(&r.DataMB, &r.Minutes, &r.SMSCount, &r.Cents, &r.UpdatedAt)
	if err != nil {
		// Treat empty as zero rollup (not an error).
		r.UpdatedAt = time.Now()
		return r, nil
	}
	return r, nil
}

func (s *Service) OrgMonth(ctx context.Context, orgID string) ([]Rollup, error) {
	monthStart := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.UTC)
	rows, err := s.pool.Query(ctx,
		`SELECT org_id::text, user_id::text, month, data_mb, minutes, sms_count, cents, updated_at
		   FROM billing.usage_rollups WHERE org_id = $1::uuid AND month = $2
		   ORDER BY cents DESC`,
		orgID, monthStart,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rollup{}
	for rows.Next() {
		var r Rollup
		var u *string
		if err := rows.Scan(&r.OrgID, &u, &r.Month, &r.DataMB, &r.Minutes, &r.SMSCount, &r.Cents, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.UserID = u
		out = append(out, r)
	}
	return out, rows.Err()
}

type Handler struct{ svc *Service }

func NewHandler(s *Service) *Handler { return &Handler{svc: s} }

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/usage/events", h.ingest)
	mux.HandleFunc("GET /v1/usage", h.me)
	mux.HandleFunc("GET /v1/usage/org", h.org)
}

func (h *Handler) ingest(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.FromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "no token")
		return
	}
	var e UsageEvent
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	if e.OrgID == "" {
		e.OrgID = claims.OrgID
	}
	if e.UserID == nil {
		uid := claims.UserID
		e.UserID = &uid
	}
	if err := h.svc.Ingest(r.Context(), e); err != nil {
		respond.DBError(w, err)
		return
	}
	respond.JSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	claims, _ := jwt.FromContext(r.Context())
	r1, err := h.svc.MyMonth(r.Context(), claims.OrgID, claims.UserID)
	if err != nil {
		respond.DBError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, r1)
}

func (h *Handler) org(w http.ResponseWriter, r *http.Request) {
	claims, _ := jwt.FromContext(r.Context())
	rs, err := h.svc.OrgMonth(r.Context(), claims.OrgID)
	if err != nil {
		respond.DBError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, rs)
}

func ptr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// silence unused import
var _ = errors.New
