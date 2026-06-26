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

	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-iam/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startPostgres boots a throwaway Postgres, applies the iam schema + migrations,
// and returns a connected pool. Skips if Docker is unavailable.
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

	// Bootstrap schema + extensions, then run the iam migrations in order.
	mustExec(t, pool, `CREATE SCHEMA IF NOT EXISTS iam;
		CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
		CREATE EXTENSION IF NOT EXISTS "pgcrypto";`)
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
		// migrations begin with `SET search_path TO iam, public;`
		mustExec(t, pool, string(b))
	}
}

func TestIntegrationUserAndTokenLifecycle(t *testing.T) {
	pool := startPostgres(t)
	repo := New(pool)
	ctx := context.Background()

	org, err := repo.CreateOrg(ctx, "Integration Org", "integration-"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	u := &model.User{OrgID: org.ID, Email: "int@home.local", DisplayName: "Int", PasswordHash: "x", Role: model.RoleSuperadmin}
	u, err = repo.CreateUser(ctx, u)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if u.ID == "" {
		t.Fatal("user id not set")
	}

	got, err := repo.GetUserByEmail(ctx, "int@home.local")
	if err != nil || got.ID != u.ID {
		t.Fatalf("get by email: %v got=%+v", err, got)
	}
	got, err = repo.GetUserByID(ctx, u.ID)
	if err != nil || got.Email != "int@home.local" {
		t.Fatalf("get by id: %v", err)
	}

	// device
	dev, err := repo.UpsertDevice(ctx, u.ID, "fp1", "laptop")
	if err != nil {
		t.Fatalf("upsert device: %v", err)
	}

	// refresh token store + find + revoke family
	fam := uuid.NewString()
	rt := &model.RefreshToken{UserID: u.ID, DeviceID: dev.ID, FamilyID: fam, TokenHash: "hash1", ExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.StoreRefreshToken(ctx, rt); err != nil {
		t.Fatalf("store token: %v", err)
	}
	found, err := repo.FindRefreshToken(ctx, "hash1")
	if err != nil || found.FamilyID != fam {
		t.Fatalf("find token: %v", err)
	}

	if err := repo.RevokeFamily(ctx, fam); err != nil {
		t.Fatalf("revoke family: %v", err)
	}
	if _, err := repo.FindRefreshToken(ctx, "hash1"); !errors.Is(err, model.ErrTokenRevoked) {
		t.Fatalf("want ErrTokenRevoked after family revoke, got %v", err)
	}
}

func TestIntegrationDuplicateEmailRejected(t *testing.T) {
	pool := startPostgres(t)
	repo := New(pool)
	ctx := context.Background()
	org, _ := repo.CreateOrg(ctx, "Org", "org-"+uuid.NewString()[:8])
	u := &model.User{OrgID: org.ID, Email: "dup@home.local", PasswordHash: "x", Role: "user"}
	if _, err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("first create: %v", err)
	}
	u2 := &model.User{OrgID: org.ID, Email: "dup@home.local", PasswordHash: "y", Role: "user"}
	if _, err := repo.CreateUser(ctx, u2); err == nil {
		t.Fatal("expected duplicate email to fail at the DB unique constraint")
	}
}

func TestIntegrationMissingUserIsNotFound(t *testing.T) {
	pool := startPostgres(t)
	repo := New(pool)
	if _, err := repo.GetUserByEmail(context.Background(), "ghost@home.local"); !errors.Is(err, model.ErrUserNotFound) {
		t.Fatalf("want ErrUserNotFound, got %v", err)
	}
}
