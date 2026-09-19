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

// fakeProv is a Provisioner whose Upsert/Delete return preset errors.
type fakeProv struct {
	upsertErr error
	deleteErr error
	upserts   int
	deletes   int
}

func (p *fakeProv) Upsert(context.Context, open5gs.Subscriber) error {
	p.upserts++
	return p.upsertErr
}
func (p *fakeProv) Delete(context.Context, string) error {
	p.deletes++
	return p.deleteErr
}

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

// seedSIM inserts a SIM (owned by "org-1") so Suspend/Terminate have a row to
// act on.
func seedSIM(repo *fakeRepo, id, imsi string) {
	repo.sims[id] = &model.SIM{ID: id, OrgID: "org-1", IMSI: imsi, Status: "active"}
}

// SECURITY (IDOR): the by-id endpoints must not let one tenant read or act on
// another tenant's SIM. A SIM owned by org-1, addressed with a different org's
// identity, must come back ErrSIMNotFound — not returned, and not
// suspended/resumed/terminated — and the victim SIM must be left untouched.
func TestByIDEndpointsEnforceTenantOwnership(t *testing.T) {
	repo := newFakeRepo()
	seedSIM(repo, "s1", "999700000000001") // owned by org-1
	svc := newSvc(repo, &fakeProv{})
	const attacker = "org-2"

	if _, err := svc.Get(context.Background(), attacker, "s1"); !errors.Is(err, model.ErrSIMNotFound) {
		t.Fatalf("Get across tenants must be ErrSIMNotFound (IDOR), got %v", err)
	}
	if err := svc.Suspend(context.Background(), attacker, "s1"); !errors.Is(err, model.ErrSIMNotFound) {
		t.Fatalf("Suspend across tenants must be ErrSIMNotFound (IDOR), got %v", err)
	}
	if err := svc.Resume(context.Background(), attacker, "s1"); !errors.Is(err, model.ErrSIMNotFound) {
		t.Fatalf("Resume across tenants must be ErrSIMNotFound (IDOR), got %v", err)
	}
	if err := svc.Terminate(context.Background(), attacker, "s1"); !errors.Is(err, model.ErrSIMNotFound) {
		t.Fatalf("Terminate across tenants must be ErrSIMNotFound (IDOR), got %v", err)
	}
	// No cross-tenant op may have mutated the victim SIM.
	if repo.sims["s1"].Status != "active" {
		t.Fatalf("cross-tenant op mutated the victim SIM: status=%q", repo.sims["s1"].Status)
	}
	// The legitimate owner still works.
	if _, err := svc.Get(context.Background(), "org-1", "s1"); err != nil {
		t.Fatalf("legitimate owner Get must succeed, got %v", err)
	}
}

// The bug: a failed 5G-core removal must NOT report a suspended line the UE can
// still attach with, and must NOT flip the stored status to "suspended".
func TestSuspendSurfacesDeprovisioningFailure(t *testing.T) {
	repo := newFakeRepo()
	seedSIM(repo, "s1", "999700000000001")
	prov := &fakeProv{deleteErr: errors.New("mongo unreachable")}
	svc := newSvc(repo, prov)

	err := svc.Suspend(context.Background(), "org-1", "s1")
	if err == nil {
		t.Fatal("suspend must fail when the SIM can't be removed from the 5G core")
	}
	if !errors.Is(err, model.ErrDeprovisioning) {
		t.Fatalf("want ErrDeprovisioning, got %v", err)
	}
	if repo.sims["s1"].Status == "suspended" {
		t.Fatal("status must NOT be flipped to suspended when de-provisioning failed")
	}
}

func TestSuspendSuccess(t *testing.T) {
	repo := newFakeRepo()
	seedSIM(repo, "s1", "999700000000001")
	prov := &fakeProv{}
	if err := newSvc(repo, prov).Suspend(context.Background(), "org-1", "s1"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if prov.deletes != 1 {
		t.Fatalf("expected 1 Open5GS delete, got %d", prov.deletes)
	}
	if repo.sims["s1"].Status != "suspended" {
		t.Fatalf("status want suspended got %q", repo.sims["s1"].Status)
	}
}

func TestTerminateSurfacesDeprovisioningFailure(t *testing.T) {
	repo := newFakeRepo()
	seedSIM(repo, "s2", "999700000000002")
	prov := &fakeProv{deleteErr: errors.New("mongo unreachable")}
	err := newSvc(repo, prov).Terminate(context.Background(), "org-1", "s2")
	if !errors.Is(err, model.ErrDeprovisioning) {
		t.Fatalf("want ErrDeprovisioning, got %v", err)
	}
	if repo.sims["s2"].Status == "terminated" {
		t.Fatal("status must NOT be flipped to terminated when de-provisioning failed")
	}
}
