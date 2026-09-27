//go:build integration

package provision

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startPostgres spins a throwaway Postgres, applies the provision migrations, and
// returns a pool. Skips (not fails) when Docker is unavailable, so the suite
// degrades gracefully off-CI; every other error is fatal.
func startPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pg, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("aerial"),
		postgres.WithUsername("aerial_admin"),
		postgres.WithPassword("test_pass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Skipf("cannot start postgres container (docker not available?): %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connstr: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS provision;
		CREATE EXTENSION IF NOT EXISTS "uuid-ossp"; CREATE EXTENSION IF NOT EXISTS "pgcrypto";`); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	entries, err := os.ReadDir("../../migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	var ups []string
	for _, e := range entries {
		if n := e.Name(); len(n) > 7 && n[len(n)-7:] == ".up.sql" {
			ups = append(ups, n)
		}
	}
	sort.Strings(ups)
	for _, f := range ups {
		b, err := os.ReadFile(filepath.Join("../../migrations", f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if _, err := pool.Exec(ctx, string(b)); err != nil {
			t.Fatalf("apply %s: %v", f, err)
		}
	}
	return pool
}

// SECURITY REGRESSION (IDOR, commit 880b94c): Cancel must only cancel a
// subscription the caller's org owns. A cross-tenant Cancel must return
// ErrSubNotFound and leave the victim row untouched; the owner's Cancel must
// succeed. This exercises the real `AND org_id=$2` SQL scoping — a fake querier
// could only assert the argument was passed, not that the row is actually
// filtered, so this is an integration test on purpose.
func TestIntegrationCancelEnforcesOrgOwnership(t *testing.T) {
	pool := startPostgres(t)
	svc := New(pool)
	ctx := context.Background()
	orgA, orgB, user := uuid.NewString(), uuid.NewString(), uuid.NewString()

	sub, err := svc.CreateSubscription(ctx, orgA, CreateSubRequest{UserID: user, PlanID: "aerial-basic"})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if sub.Status != "active" {
		t.Fatalf("new subscription status = %q, want active", sub.Status)
	}

	// Cross-tenant cancel: must be rejected as not-found, row unchanged.
	if err := svc.Cancel(ctx, orgB, sub.ID); !errors.Is(err, ErrSubNotFound) {
		t.Fatalf("cross-tenant Cancel must be ErrSubNotFound (IDOR), got %v", err)
	}
	subs, err := svc.ListByOrg(ctx, orgA)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 1 || subs[0].Status != "active" {
		t.Fatalf("victim subscription mutated by cross-tenant cancel: %+v", subs)
	}

	// Owner cancel: succeeds and flips status.
	if err := svc.Cancel(ctx, orgA, sub.ID); err != nil {
		t.Fatalf("owner Cancel: %v", err)
	}
	subs, _ = svc.ListByOrg(ctx, orgA)
	if len(subs) != 1 || subs[0].Status != "cancelled" {
		t.Fatalf("owner cancel did not flip status: %+v", subs)
	}
}

// The money path: creating a subscription validates the plan, and reads are
// scoped to the org.
func TestIntegrationCreateSubscriptionValidatesPlan(t *testing.T) {
	pool := startPostgres(t)
	svc := New(pool)
	ctx := context.Background()
	org, user := uuid.NewString(), uuid.NewString()

	// Seeded plan resolves.
	if _, err := svc.GetPlan(ctx, "aerial-basic"); err != nil {
		t.Fatalf("seeded plan should resolve: %v", err)
	}
	// Unknown plan is a clean not-found, not a raw DB error.
	if _, err := svc.GetPlan(ctx, "does-not-exist"); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("unknown plan want ErrPlanNotFound, got %v", err)
	}
	// CreateSubscription rejects an unknown plan before inserting.
	if _, err := svc.CreateSubscription(ctx, org, CreateSubRequest{UserID: user, PlanID: "does-not-exist"}); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("create with unknown plan want ErrPlanNotFound, got %v", err)
	}
	// A valid plan creates an active subscription visible only to its org.
	if _, err := svc.CreateSubscription(ctx, org, CreateSubRequest{UserID: user, PlanID: "aerial-family"}); err != nil {
		t.Fatalf("create with valid plan: %v", err)
	}
	if subs, _ := svc.ListByOrg(ctx, org); len(subs) != 1 {
		t.Fatalf("org should see its 1 subscription, got %d", len(subs))
	}
	if subs, _ := svc.ListByOrg(ctx, uuid.NewString()); len(subs) != 0 {
		t.Fatalf("a different org must see none, got %d", len(subs))
	}
}
