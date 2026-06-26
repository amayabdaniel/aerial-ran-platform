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

	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

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

	dsn, _ := pg.ConnectionString(ctx, "sslmode=disable")
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS subscriber;
		CREATE EXTENSION IF NOT EXISTS "uuid-ossp"; CREATE EXTENSION IF NOT EXISTS "pgcrypto";`); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	entries, _ := os.ReadDir("../../migrations")
	var ups []string
	for _, e := range entries {
		n := e.Name()
		if len(n) > 7 && n[len(n)-7:] == ".up.sql" {
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

func TestIntegrationSIMCRUDAndStatus(t *testing.T) {
	pool := startPostgres(t)
	repo := New(pool)
	ctx := context.Background()
	org := uuid.NewString()

	// NextMSIN on an empty PLMN is 1.
	n, err := repo.NextMSIN(ctx, "999", "70")
	if err != nil || n != 1 {
		t.Fatalf("NextMSIN want 1 got %d err=%v", n, err)
	}

	sim := &model.SIM{
		OrgID: org, IMSI: "999700000000001", PLMNMcc: "999", PLMNMnc: "70",
		Ki: "00112233445566778899AABBCCDDEEFF", OPc: "FFEEDDCCBBAA99887766554433221100",
		AMF: "8000", APN: "internet", SST: 1, Status: "active",
	}
	created, err := repo.Create(ctx, sim)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("sim id not set")
	}

	// NextMSIN now 2.
	if n, _ := repo.NextMSIN(ctx, "999", "70"); n != 2 {
		t.Fatalf("NextMSIN should be 2 after 1 insert, got %d", n)
	}

	// Duplicate IMSI rejected.
	if _, err := repo.Create(ctx, sim); !errors.Is(err, model.ErrSIMExists) {
		t.Fatalf("want ErrSIMExists, got %v", err)
	}

	// Get + list.
	got, err := repo.GetByID(ctx, created.ID)
	if err != nil || got.IMSI != "999700000000001" {
		t.Fatalf("get: %v", err)
	}
	if got.Ki != sim.Ki {
		t.Fatalf("Ki round-trip mismatch")
	}
	list, err := repo.ListByOrg(ctx, org)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}

	// Provision + status transitions.
	if err := repo.MarkProvisioned(ctx, created.ID); err != nil {
		t.Fatalf("mark provisioned: %v", err)
	}
	got, _ = repo.GetByID(ctx, created.ID)
	if got.ProvisionedAt == nil {
		t.Fatal("provisioned_at should be set")
	}
	if err := repo.UpdateStatus(ctx, created.ID, "suspended"); err != nil {
		t.Fatalf("update status: %v", err)
	}
	got, _ = repo.GetByID(ctx, created.ID)
	if got.Status != "suspended" {
		t.Fatalf("status want suspended got %q", got.Status)
	}
}

func TestIntegrationMissingSIMNotFound(t *testing.T) {
	pool := startPostgres(t)
	repo := New(pool)
	if _, err := repo.GetByID(context.Background(), uuid.NewString()); !errors.Is(err, model.ErrSIMNotFound) {
		t.Fatalf("want ErrSIMNotFound, got %v", err)
	}
	if err := repo.UpdateStatus(context.Background(), uuid.NewString(), "active"); !errors.Is(err, model.ErrSIMNotFound) {
		t.Fatalf("update missing want ErrSIMNotFound, got %v", err)
	}
}
