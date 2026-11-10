//go:build integration

package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startPostgres boots a throwaway Postgres, applies the esim schema + migrations
// in order, and returns a connected pool. Skips if Docker is unavailable. The
// container is ephemeral and torn down in Cleanup — no real database is touched.
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

	// Bootstrap schema + extension, then run the esim migrations in order.
	mustExec(t, pool, `CREATE SCHEMA IF NOT EXISTS esim;
		CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`)
	applyMigrations(t, pool, "../../migrations")
	return pool
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql); err != nil {
		t.Fatalf("exec: %v\nSQL: %s", err, sql)
	}
}

func applyMigrations(t *testing.T, pool *pgxpool.Pool, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	var ups []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".sql" && len(e.Name()) > 7 && e.Name()[len(e.Name())-7:] == ".up.sql" {
			ups = append(ups, e.Name())
		}
	}
	sort.Strings(ups)
	for _, f := range ups {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		// each migration begins with `SET search_path TO esim, public;`
		mustExec(t, pool, string(b))
	}
}

// Migration 000003 adds CHECK (data_mb >= -1) to both esim columns that store
// an allowance. The floor has to get two independent things right, so each is
// its own test, verifiable in isolation:
//
//	SentinelAccepted — -1 (provider.DataUnlimited), NULL and 0 are accepted.
//	FloorRejects     — a value below the sentinel (-2) is refused by the DB.
//
// A CHECK (data_mb >= 0) — the tempting wrong floor — fails only the first; a
// dropped or lowered floor fails only the second. Both run against the throwaway
// testcontainer; the migration is never applied to a real database.

// TestIntegrationDataMBSentinelAccepted is GUARD 1: the unlimited sentinel, an
// unknown (NULL) allowance, and a genuine zero are all valid on both columns.
func TestIntegrationDataMBSentinelAccepted(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	org := uuid.NewString()

	accepted := []struct {
		name string
		sql  string
		args []any
	}{
		{
			"packages data_mb=-1 (unlimited)",
			`INSERT INTO esim.packages(id, provider, label, data_mb, validity_days, price_usd_cents)
			 VALUES ($1, 'mock', 'Unlimited', -1, 30, 0)`,
			[]any{"pkg-unl-" + uuid.NewString()[:8]},
		},
		{
			"packages data_mb=0 (no data)",
			`INSERT INTO esim.packages(id, provider, label, data_mb, validity_days, price_usd_cents)
			 VALUES ($1, 'mock', 'Zero', 0, 30, 0)`,
			[]any{"pkg-zero-" + uuid.NewString()[:8]},
		},
		{
			"esims data_mb=-1 (unlimited)",
			`INSERT INTO esim.esims(org_id, provider, data_mb) VALUES ($1, 'mock', -1)`,
			[]any{org},
		},
		{
			"esims data_mb=NULL (unknown allowance)",
			`INSERT INTO esim.esims(org_id, provider, data_mb) VALUES ($1, 'mock', NULL)`,
			[]any{org},
		},
	}
	for _, c := range accepted {
		if _, err := pool.Exec(ctx, c.sql, c.args...); err != nil {
			t.Fatalf("%s: want accepted, got %v", c.name, err)
		}
	}
}

// TestIntegrationDataMBFloorRejects is GUARD 2: a value below the sentinel is
// refused by the DB with check_violation (SQLSTATE 23514), on both columns.
func TestIntegrationDataMBFloorRejects(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	org := uuid.NewString()

	rejected := []struct {
		name string
		sql  string
		args []any
	}{
		{
			"packages data_mb=-2",
			`INSERT INTO esim.packages(id, provider, label, data_mb, validity_days, price_usd_cents)
			 VALUES ($1, 'mock', 'Bad', -2, 30, 0)`,
			[]any{"pkg-bad-" + uuid.NewString()[:8]},
		},
		{
			"esims data_mb=-2",
			`INSERT INTO esim.esims(org_id, provider, data_mb) VALUES ($1, 'mock', -2)`,
			[]any{org},
		},
	}
	for _, c := range rejected {
		_, err := pool.Exec(ctx, c.sql, c.args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("%s: want check_violation (SQLSTATE 23514), got %v", c.name, err)
		}
	}
}
