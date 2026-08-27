package service

import (
	"context"
	"errors"
	"testing"

	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/model"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/open5gs"
)

// fakeRepo is an in-memory Repo.
type fakeRepo struct {
	sims        map[string]*model.SIM
	provisioned map[string]bool
	seq         int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{sims: map[string]*model.SIM{}, provisioned: map[string]bool{}}
}

func (f *fakeRepo) NextMSIN(context.Context, string, string) (int64, error) {
	f.seq++
	return int64(f.seq), nil
}
func (f *fakeRepo) Create(_ context.Context, s *model.SIM) (*model.SIM, error) {
	if s.ID == "" {
		s.ID = "id-" + s.IMSI
	}
	cp := *s
	f.sims[s.ID] = &cp
	return s, nil
}
func (f *fakeRepo) GetByID(_ context.Context, id string) (*model.SIM, error) {
	s, ok := f.sims[id]
	if !ok {
		return nil, model.ErrSIMNotFound
	}
	cp := *s
	if f.provisioned[id] {
		now := cp.CreatedAt
		cp.ProvisionedAt = &now
	}
	return &cp, nil
}
func (f *fakeRepo) ListByOrg(context.Context, string) ([]*model.SIM, error) { return nil, nil }
func (f *fakeRepo) MarkProvisioned(_ context.Context, id string) error {
	f.provisioned[id] = true
	return nil
}
func (f *fakeRepo) UpdateStatus(_ context.Context, id, status string) error {
	if s, ok := f.sims[id]; ok {
		s.Status = status
	}
	return nil
}

// fakeProv is a Provisioner whose Upsert returns upsertErr.
type fakeProv struct {
	upsertErr error
	upserts   int
}

func (p *fakeProv) Upsert(context.Context, open5gs.Subscriber) error {
	p.upserts++
	return p.upsertErr
}
func (p *fakeProv) Delete(context.Context, string) error { return nil }

func newSvc(repo Repo, prov Provisioner) *SIM {
	return &SIM{repo: repo, open5gs: prov, plmnMcc: "999", plmnMnc: "70"}
}

func TestCreateProvisionsAndMarks(t *testing.T) {
	repo := newFakeRepo()
	prov := &fakeProv{}
	svc := newSvc(repo, prov)

	sim, err := svc.Create(context.Background(), "org-1", model.CreateSIMRequest{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if prov.upserts != 1 {
		t.Fatalf("expected 1 upsert into Open5GS, got %d", prov.upserts)
	}
	if sim.ProvisionedAt == nil {
		t.Fatalf("a successfully provisioned SIM must have provisioned_at set")
	}
}

// The bug: a failed Open5GS provision must NOT be reported as a clean success —
// the UE could never attach with such a SIM.
func TestCreateSurfacesProvisioningFailure(t *testing.T) {
	repo := newFakeRepo()
	prov := &fakeProv{upsertErr: errors.New("mongo unreachable")}
	svc := newSvc(repo, prov)

	sim, err := svc.Create(context.Background(), "org-1", model.CreateSIMRequest{})
	if err == nil {
		t.Fatal("provisioning failure must surface, not return a bogus active SIM")
	}
	if !errors.Is(err, model.ErrProvisioning) {
		t.Fatalf("error should wrap ErrProvisioning, got %v", err)
	}
	if sim != nil {
		t.Fatalf("on provisioning failure no SIM should be returned, got %+v", sim)
	}
	// Must not have been marked provisioned.
	if len(repo.provisioned) != 0 {
		t.Fatalf("SIM must not be marked provisioned after a failed upsert")
	}
}

// With no 5G core wired, Create still succeeds (no provisioning attempted).
func TestCreateWithoutCoreSucceeds(t *testing.T) {
	repo := newFakeRepo()
	svc := &SIM{repo: repo, open5gs: nil, plmnMcc: "999", plmnMnc: "70"}

	sim, err := svc.Create(context.Background(), "org-1", model.CreateSIMRequest{})
	if err != nil {
		t.Fatalf("create without core: %v", err)
	}
	if sim.ProvisionedAt != nil {
		t.Fatalf("without a core, SIM should not be marked provisioned")
	}
}

func TestCreateRejectsEmptyOrg(t *testing.T) {
	svc := newSvc(newFakeRepo(), &fakeProv{})
	if _, err := svc.Create(context.Background(), "  ", model.CreateSIMRequest{}); !errors.Is(err, model.ErrBadInput) {
		t.Fatalf("want ErrBadInput for empty org, got %v", err)
	}
}
