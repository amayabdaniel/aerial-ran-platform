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

// Repo is the persistence surface the service needs (consumer-defined so tests
// can substitute a fake). *repository.Postgres satisfies it.
type Repo interface {
	NextMSIN(ctx context.Context, mcc, mnc string) (int64, error)
	Create(ctx context.Context, s *model.SIM) (*model.SIM, error)
	GetByID(ctx context.Context, id string) (*model.SIM, error)
	ListByOrg(ctx context.Context, orgID string) ([]*model.SIM, error)
	MarkProvisioned(ctx context.Context, id string) error
	UpdateStatus(ctx context.Context, id, status string) error
}

// Provisioner writes/removes subscribers in the 5G core (Open5GS MongoDB).
// *open5gs.Client satisfies it. May be nil when no 5GC is reachable.
type Provisioner interface {
	Upsert(ctx context.Context, s open5gs.Subscriber) error
	Delete(ctx context.Context, imsi string) error
}

// SIM is the service.
type SIM struct {
	repo     Repo
	open5gs  Provisioner
	plmnMcc  string
	plmnMnc  string
}

// New wires the service. A nil mongo client is stored as a nil Provisioner
// (not a typed-nil interface) so the s.open5gs != nil guards behave correctly.
func New(repo *repository.Postgres, mongo *open5gs.Client, mcc, mnc string) *SIM {
	s := &SIM{repo: repo, plmnMcc: mcc, plmnMnc: mnc}
	if mongo != nil {
		s.open5gs = mongo
	}
	return s
}

// Create generates Ki/OPc and inserts the SIM into Postgres, then provisions it
// into Open5GS MongoDB. If the Mongo write fails it returns ErrProvisioning
// (the SIM row is kept and can be re-provisioned via Resume) rather than
// reporting a success for a SIM the UE cannot attach with.
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

	// Provision into Open5GS MongoDB so UEs can attach with this SIM. If this
	// fails, the SIM row is kept (durable, re-provisionable via Resume) but we
	// surface the error — returning a "provisioned" success for a SIM the UE
	// can't actually attach with would be a lie.
	if s.open5gs != nil {
		if err := s.open5gs.Upsert(ctx, open5gs.Subscriber{
			IMSI: created.IMSI,
			APN:  created.APN,
			Ki:   created.Ki,
			OPc:  created.OPc,
			AMF:  created.AMF,
			SST:  created.SST,
		}); err != nil {
			return nil, errors.Join(model.ErrProvisioning, err)
		}
		if err := s.repo.MarkProvisioned(ctx, created.ID); err != nil {
			return nil, err
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
// If the 5G-core removal fails, the status is NOT changed and the error is
// surfaced — otherwise we'd report a suspended line that can still attach.
func (s *SIM) Suspend(ctx context.Context, id string) error {
	sim, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if s.open5gs != nil {
		if err := s.open5gs.Delete(ctx, sim.IMSI); err != nil {
			return errors.Join(model.ErrDeprovisioning, err)
		}
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
// Like Suspend, a failed 5G-core removal is surfaced rather than reporting a
// terminated line the UE can still attach with.
func (s *SIM) Terminate(ctx context.Context, id string) error {
	sim, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if s.open5gs != nil {
		if err := s.open5gs.Delete(ctx, sim.IMSI); err != nil {
			return errors.Join(model.ErrDeprovisioning, err)
		}
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
