package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// fakeRow is a pgx.Row whose Scan returns a preset error.
type fakeRow struct{ err error }

func (r fakeRow) Scan(dest ...any) error { return r.err }

// fakeQuerier implements the billing querier; QueryRow returns a row with scanErr.
type fakeQuerier struct{ scanErr error }

func (f fakeQuerier) Begin(context.Context) (pgx.Tx, error) { return nil, errors.New("unused") }
func (f fakeQuerier) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeRow{err: f.scanErr}
}
func (f fakeQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unused")
}

// No rollup rows this month is genuinely zero usage — not an error.
func TestMyMonthNoRowsReturnsZeroRollup(t *testing.T) {
	s := &Service{db: fakeQuerier{scanErr: pgx.ErrNoRows}}
	r, err := s.MyMonth(context.Background(), "org-1", "user-1")
	if err != nil {
		t.Fatalf("ErrNoRows should be treated as zero rollup, got err: %v", err)
	}
	if r == nil || r.DataMB != 0 || r.Cents != 0 {
		t.Fatalf("want zero rollup, got %+v", r)
	}
	if r.OrgID != "org-1" || r.UserID == nil || *r.UserID != "user-1" {
		t.Fatalf("rollup identity wrong: %+v", r)
	}
}

// A real DB error must propagate, NOT be masked as "$0.00 usage".
func TestMyMonthRealErrorPropagates(t *testing.T) {
	dbErr := errors.New("connection refused")
	s := &Service{db: fakeQuerier{scanErr: dbErr}}
	r, err := s.MyMonth(context.Background(), "org-1", "user-1")
	if err == nil {
		t.Fatal("a real DB error must surface, not be swallowed as zero usage")
	}
	if !errors.Is(err, dbErr) {
		t.Fatalf("want the underlying db error, got %v", err)
	}
	if r != nil {
		t.Fatalf("on error the rollup must be nil, got %+v", r)
	}
}
