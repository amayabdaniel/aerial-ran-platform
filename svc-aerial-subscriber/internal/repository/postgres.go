// Package repository persists SIM records in Postgres.
package repository

import (
	"context"
	"errors"

	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

// NextMSIN returns the next-available 10-digit MSIN for a given PLMN.
// Strategy: count existing SIMs in the PLMN and return count+1 zero-padded.
func (p *Postgres) NextMSIN(ctx context.Context, mcc, mnc string) (int64, error) {
	var n int64
	err := p.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM subscriber.sims WHERE plmn_mcc = $1 AND plmn_mnc = $2`,
		mcc, mnc,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n + 1, nil
}

// Create inserts a new SIM. Returns ErrSIMExists if the IMSI is taken.
func (p *Postgres) Create(ctx context.Context, s *model.SIM) (*model.SIM, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO subscriber.sims(org_id, owner_user_id, imsi, msisdn, plmn_mcc, plmn_mnc, ki, opc, amf, apn, sst, sd, status)
		 VALUES ($1::uuid, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 RETURNING id::text, created_at, updated_at`,
		s.OrgID, ptrToStr(s.OwnerUserID), s.IMSI, ptrToStr(s.MSISDN), s.PLMNMcc, s.PLMNMnc,
		s.Ki, s.OPc, s.AMF, s.APN, s.SST, ptrToStr(s.SD), s.Status,
	).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		// duplicate IMSI
		if isUniqueViolation(err) {
			return nil, model.ErrSIMExists
		}
		return nil, err
	}
	return s, nil
}

// GetByID returns the SIM or ErrSIMNotFound.
func (p *Postgres) GetByID(ctx context.Context, id string) (*model.SIM, error) {
	s := &model.SIM{}
	var owner, msisdn, sd *string
	err := p.pool.QueryRow(ctx,
		`SELECT id::text, org_id::text, owner_user_id::text, imsi, msisdn, plmn_mcc, plmn_mnc, ki, opc,
		        amf, apn, sst, sd, status, provisioned_at, created_at, updated_at
		   FROM subscriber.sims WHERE id = $1::uuid`, id,
	).Scan(&s.ID, &s.OrgID, &owner, &s.IMSI, &msisdn, &s.PLMNMcc, &s.PLMNMnc, &s.Ki, &s.OPc,
		&s.AMF, &s.APN, &s.SST, &sd, &s.Status, &s.ProvisionedAt, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrSIMNotFound
	}
	if err != nil {
		return nil, err
	}
	s.OwnerUserID, s.MSISDN, s.SD = owner, msisdn, sd
	return s, nil
}

// ListByOrg returns up to 200 SIMs for an org.
func (p *Postgres) ListByOrg(ctx context.Context, orgID string) ([]*model.SIM, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id::text, org_id::text, owner_user_id::text, imsi, msisdn, plmn_mcc, plmn_mnc, ki, opc,
		        amf, apn, sst, sd, status, provisioned_at, created_at, updated_at
		   FROM subscriber.sims WHERE org_id = $1::uuid ORDER BY created_at DESC LIMIT 200`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*model.SIM, 0, 16)
	for rows.Next() {
		s := &model.SIM{}
		var owner, msisdn, sd *string
		if err := rows.Scan(&s.ID, &s.OrgID, &owner, &s.IMSI, &msisdn, &s.PLMNMcc, &s.PLMNMnc, &s.Ki, &s.OPc,
			&s.AMF, &s.APN, &s.SST, &sd, &s.Status, &s.ProvisionedAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.OwnerUserID, s.MSISDN, s.SD = owner, msisdn, sd
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkProvisioned records that the SIM is live in the 5GC subscriber DB.
func (p *Postgres) MarkProvisioned(ctx context.Context, id string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE subscriber.sims SET provisioned_at = now(), updated_at = now()
		   WHERE id = $1::uuid`, id)
	return err
}

// UpdateStatus sets the SIM status (active|suspended|terminated).
func (p *Postgres) UpdateStatus(ctx context.Context, id, status string) error {
	res, err := p.pool.Exec(ctx,
		`UPDATE subscriber.sims SET status = $2, updated_at = now()
		   WHERE id = $1::uuid`, id, status)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return model.ErrSIMNotFound
	}
	return nil
}

func ptrToStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// isUniqueViolation returns true if err is a Postgres unique-violation (23505).
func isUniqueViolation(err error) bool {
	type pgErr interface{ SQLState() string }
	if pe, ok := err.(pgErr); ok {
		return pe.SQLState() == "23505"
	}
	return false
}
