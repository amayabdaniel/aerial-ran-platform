package provision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These are fast, Docker-free unit tests for the provision service's branching
// logic. The integration suite (postgres_integration_test.go) skips entirely
// when Docker is unavailable, so before this file the error-mapping that the
// HTTP layer depends on — ErrPlanNotFound, ErrSubExists (409), ErrSubNotFound
// (404) — had no coverage on a plain `make test-unit`. The seam is the querier
// interface on Service, the same one svc-aerial-billing already uses.

// scanRow is a pgx.Row whose Scan delegates to fn, so a queued row can either
// populate the destinations (a found row) or return an error (ErrNoRows, a
// *pgconn.PgError).
type scanRow struct{ fn func(dest ...any) error }

func (r scanRow) Scan(dest ...any) error { return r.fn(dest...) }

// errRow is a row whose Scan always returns err, ignoring the destinations.
func errRow(err error) scanRow { return scanRow{fn: func(...any) error { return err }} }

// planFoundRow fills the 6 destinations GetPlan scans, in order:
// id, name, monthly_cents, data_cap_mb (*int, left NULL), description, created_at.
func planFoundRow() scanRow {
	return scanRow{fn: func(dest ...any) error {
		if len(dest) != 6 {
			return errors.New("plan scan: unexpected destination count")
		}
		*dest[0].(*string) = "aerial-basic"
		*dest[1].(*string) = "Aerial Basic"
		*dest[2].(*int) = 1500
		// dest[3] is **int (Plan.DataCapMB is *int) — leave nil, i.e. NULL cap.
		*dest[4].(*string) = ""
		// dest[5] is *time.Time — leave zero.
		return nil
	}}
}

// insertOKRow fills the 4 destinations the CreateSubscription INSERT scans:
// id, started_at, created_at, updated_at. Only id is asserted on by tests.
func insertOKRow() scanRow {
	return scanRow{fn: func(dest ...any) error {
		if len(dest) > 0 {
			if p, ok := dest[0].(*string); ok {
				*p = "11111111-1111-1111-1111-111111111111"
			}
		}
		return nil
	}}
}

// fakeQuerier serves QueryRow results from a queue in call order, Exec from a
// preset tag/err, and records every SQL string it was handed so a test can
// assert on what did (and did not) run — e.g. that no INSERT follows a failed
// plan lookup.
type fakeQuerier struct {
	rows    []pgx.Row
	i       int
	sqls    []string
	execTag pgconn.CommandTag
	execErr error
}

func (f *fakeQuerier) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	f.sqls = append(f.sqls, sql)
	if f.i >= len(f.rows) {
		return errRow(errors.New("fakeQuerier: unexpected QueryRow"))
	}
	r := f.rows[f.i]
	f.i++
	return r
}

func (f *fakeQuerier) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	f.sqls = append(f.sqls, sql)
	return nil, errors.New("fakeQuerier: Query not exercised by these tests")
}

func (f *fakeQuerier) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.sqls = append(f.sqls, sql)
	return f.execTag, f.execErr
}

func anyContains(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// An unknown plan must surface as ErrPlanNotFound AND short-circuit before the
// INSERT — attempting the insert would otherwise hit a plan_id foreign key and
// return an opaque 23503 instead of the clean 404 the handler maps.
func TestCreateSubscriptionUnknownPlanShortCircuits(t *testing.T) {
	f := &fakeQuerier{rows: []pgx.Row{errRow(pgx.ErrNoRows)}}
	s := &Service{db: f}

	_, err := s.CreateSubscription(context.Background(), "org-1",
		CreateSubRequest{UserID: "user-1", PlanID: "does-not-exist"})
	if !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("unknown plan: want ErrPlanNotFound, got %v", err)
	}
	if len(f.sqls) != 1 {
		t.Fatalf("expected exactly the plan lookup to run, got %d statements: %v", len(f.sqls), f.sqls)
	}
	if anyContains(f.sqls, "INSERT") {
		t.Fatalf("no INSERT must be attempted once the plan is missing, saw: %v", f.sqls)
	}
}

// A 23505 unique_violation from the partial index is the one DB error the INSERT
// can raise that means "already subscribed"; it must become ErrSubExists (→ 409).
func TestCreateSubscriptionDuplicateMapsToErrSubExists(t *testing.T) {
	f := &fakeQuerier{rows: []pgx.Row{planFoundRow(), errRow(&pgconn.PgError{Code: "23505"})}}
	s := &Service{db: f}

	_, err := s.CreateSubscription(context.Background(), "org-1",
		CreateSubRequest{UserID: "user-1", PlanID: "aerial-basic"})
	if !errors.Is(err, ErrSubExists) {
		t.Fatalf("duplicate active subscription: want ErrSubExists, got %v", err)
	}
}

// The 23505 mapping must be specific: a different PgError (here 23503, foreign
// key) must NOT be laundered into ErrSubExists — it propagates raw so the caller
// gets a real DB error, not a misleading 409.
func TestCreateSubscriptionForeignKeyErrorPropagatesRaw(t *testing.T) {
	f := &fakeQuerier{rows: []pgx.Row{planFoundRow(), errRow(&pgconn.PgError{Code: "23503"})}}
	s := &Service{db: f}

	_, err := s.CreateSubscription(context.Background(), "org-1",
		CreateSubRequest{UserID: "user-1", PlanID: "aerial-basic"})
	if errors.Is(err, ErrSubExists) {
		t.Fatal("a 23503 FK error must not be mapped to ErrSubExists")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("want the raw 23503 PgError to propagate, got %v", err)
	}
}

// Happy path: a valid plan yields an active subscription carrying the caller's
// org and the id returned by the INSERT.
func TestCreateSubscriptionSuccess(t *testing.T) {
	f := &fakeQuerier{rows: []pgx.Row{planFoundRow(), insertOKRow()}}
	s := &Service{db: f}

	sub, err := s.CreateSubscription(context.Background(), "org-7",
		CreateSubRequest{UserID: "user-1", PlanID: "aerial-basic"})
	if err != nil {
		t.Fatalf("valid create: unexpected error %v", err)
	}
	if sub == nil {
		t.Fatal("valid create returned a nil subscription")
	}
	if sub.Status != "active" {
		t.Fatalf("new subscription status = %q, want active", sub.Status)
	}
	if sub.OrgID != "org-7" {
		t.Fatalf("org not threaded through: got %q, want org-7", sub.OrgID)
	}
	if sub.ID == "" {
		t.Fatal("subscription id from INSERT RETURNING was not populated")
	}
}

func TestGetPlanUnknownReturnsNotFound(t *testing.T) {
	f := &fakeQuerier{rows: []pgx.Row{errRow(pgx.ErrNoRows)}}
	s := &Service{db: f}

	if _, err := s.GetPlan(context.Background(), "does-not-exist"); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("unknown plan: want ErrPlanNotFound, got %v", err)
	}
}

func TestGetPlanFound(t *testing.T) {
	f := &fakeQuerier{rows: []pgx.Row{planFoundRow()}}
	s := &Service{db: f}

	p, err := s.GetPlan(context.Background(), "aerial-basic")
	if err != nil {
		t.Fatalf("found plan: unexpected error %v", err)
	}
	if p.ID != "aerial-basic" || p.MonthlyCents != 1500 {
		t.Fatalf("plan not scanned as expected: %+v", p)
	}
}

// Cancel scopes its UPDATE by org (the IDOR fix); a row that matches nothing —
// wrong org, missing id, or already cancelled — reports zero rows affected and
// must surface as ErrSubNotFound, never a silent success.
func TestCancelNoRowsAffectedIsNotFound(t *testing.T) {
	f := &fakeQuerier{execTag: pgconn.NewCommandTag("UPDATE 0")}
	s := &Service{db: f}

	if err := s.Cancel(context.Background(), "org-1", "sub-1"); !errors.Is(err, ErrSubNotFound) {
		t.Fatalf("no rows affected: want ErrSubNotFound, got %v", err)
	}
}

func TestCancelSuccess(t *testing.T) {
	f := &fakeQuerier{execTag: pgconn.NewCommandTag("UPDATE 1")}
	s := &Service{db: f}

	if err := s.Cancel(context.Background(), "org-1", "sub-1"); err != nil {
		t.Fatalf("one row affected: want nil, got %v", err)
	}
}

// A transport/DB error from Exec must propagate — it must not be collapsed into
// the ErrSubNotFound "nothing matched" case, which would hide an outage.
func TestCancelDBErrorPropagates(t *testing.T) {
	dbErr := errors.New("connection refused")
	f := &fakeQuerier{execErr: dbErr}
	s := &Service{db: f}

	err := s.Cancel(context.Background(), "org-1", "sub-1")
	if !errors.Is(err, dbErr) {
		t.Fatalf("want the underlying db error to propagate, got %v", err)
	}
	if errors.Is(err, ErrSubNotFound) {
		t.Fatal("a real DB error must not be reported as ErrSubNotFound")
	}
}
