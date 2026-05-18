// Package service holds SIM lifecycle business logic.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/model"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/open5gs"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/repository"
)

// SIM is the service.
type SIM struct {
	repo     *repository.Postgres
	open5gs  *open5gs.Client
	plmnMcc  string
	plmnMnc  string
}

// New wires the service.
func New(repo *repository.Postgres, mongo *open5gs.Client, mcc, mnc string) *SIM {
	return &SIM{repo: repo, open5gs: mongo, plmnMcc: mcc, plmnMnc: mnc}
}

// Create generates Ki/OPc and inserts both into Postgres + Open5GS MongoDB
// in that order. If the Mongo write fails the SIM is created but unprovisioned
// (status = active, provisioned_at = null) — the operator can retry.
func (s *SIM) Create(ctx context.Context, orgID string, req model.CreateSIMRequest) (*model.SIM, error) {
	if strings.TrimSpace(orgID) == "" {
		return nil, errors.Join(model.ErrBadInput, errors.New("org_id required"))
	}

	imsi := strings.TrimSpace(req.IMSI)
	if imsi == "" {
		n, err := s.repo.NextMSIN(ctx, s.plmnMcc, s.plmnMnc)
		if err != nil {
			return nil, err
		}
		imsi = fmt.Sprintf("%s%s%010d", s.plmnMcc, s.plmnMnc, n)
	} else if len(imsi) < 14 || len(imsi) > 15 {
		return nil, errors.Join(model.ErrBadInput, fmt.Errorf("imsi must be 14-15 digits, got %d", len(imsi)))
	}

	ki, err := randHex(16)
	if err != nil {
		return nil, err
	}
	opc, err := randHex(16)
	if err != nil {
		return nil, err
	}

	apn := req.APN
	if apn == "" {
		apn = "internet"
	}
	sst := req.SST
	if sst == 0 {
		sst = 1
	}

	sim := &model.SIM{
		OrgID:       orgID,
		OwnerUserID: req.OwnerUserID,
		IMSI:        imsi,
		MSISDN:      req.MSISDN,
		PLMNMcc:     s.plmnMcc,
		PLMNMnc:     s.plmnMnc,
		Ki:          ki,
		OPc:         opc,
		AMF:         "8000",
		APN:         apn,
		SST:         sst,
		Status:      "active",
	}

	created, err := s.repo.Create(ctx, sim)
	if err != nil {
		return nil, err
	}

	// Provision into Open5GS MongoDB so UEs can attach with this SIM.
	if s.open5gs != nil {
		err := s.open5gs.Upsert(ctx, open5gs.Subscriber{
			IMSI: created.IMSI,
			APN:  created.APN,
			Ki:   created.Ki,
			OPc:  created.OPc,
			AMF:  created.AMF,
			SST:  created.SST,
		})
		if err == nil {
			_ = s.repo.MarkProvisioned(ctx, created.ID)
		}
	}

	return s.repo.GetByID(ctx, created.ID)
}

// Get returns a SIM.
func (s *SIM) Get(ctx context.Context, id string) (*model.SIM, error) {
	return s.repo.GetByID(ctx, id)
}

// ListByOrg returns the org's SIMs.
func (s *SIM) ListByOrg(ctx context.Context, orgID string) ([]*model.SIM, error) {
	return s.repo.ListByOrg(ctx, orgID)
}

// Suspend marks the SIM suspended and removes it from Open5GS so it cannot attach.
func (s *SIM) Suspend(ctx context.Context, id string) error {
	sim, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if s.open5gs != nil {
		_ = s.open5gs.Delete(ctx, sim.IMSI)
	}
	return s.repo.UpdateStatus(ctx, id, "suspended")
}

// Resume re-provisions the SIM in Open5GS.
func (s *SIM) Resume(ctx context.Context, id string) error {
	sim, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if s.open5gs != nil {
		err := s.open5gs.Upsert(ctx, open5gs.Subscriber{
			IMSI: sim.IMSI, APN: sim.APN, Ki: sim.Ki, OPc: sim.OPc, AMF: sim.AMF, SST: sim.SST,
		})
		if err != nil {
			return err
		}
	}
	return s.repo.UpdateStatus(ctx, id, "active")
}

// Terminate removes the SIM from Open5GS and marks it terminated (record retained).
func (s *SIM) Terminate(ctx context.Context, id string) error {
	sim, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if s.open5gs != nil {
		_ = s.open5gs.Delete(ctx, sim.IMSI)
	}
	return s.repo.UpdateStatus(ctx, id, "terminated")
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(b)), nil
}
