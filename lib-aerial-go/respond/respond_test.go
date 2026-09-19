package respond

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestJSONWritesStatusAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	JSON(rec, http.StatusCreated, map[string]string{"hello": "world"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201 got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content-type %q", ct)
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body not valid json: %v", err)
	}
	if out["hello"] != "world" {
		t.Fatalf("body mismatch: %v", out)
	}
}

func TestErrorShape(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, http.StatusNotFound, "not_found", "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d", rec.Code)
	}
	var out map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["code"] != "not_found" || out["message"] != "nope" {
		t.Fatalf("error body mismatch: %v", out)
	}
}

func TestDBErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"deadline", context.DeadlineExceeded, http.StatusGatewayTimeout},
		{"canceled", context.Canceled, 499},
		{"other", errString("boom"), http.StatusInternalServerError},
		// A malformed value the DB could not parse (e.g. a non-UUID id cast via
		// $n::uuid) is the caller's mistake, not a server fault: 400, not 500.
		{"pg_invalid_text", &pgconn.PgError{Code: "22P02", Message: `invalid input syntax for type uuid: "not-a-uuid"`}, http.StatusBadRequest},
		// ...even when wrapped by the repository layer (errors.As must unwrap).
		{"pg_invalid_text_wrapped", fmt.Errorf("cancel subscription: %w", &pgconn.PgError{Code: "22P02"}), http.StatusBadRequest},
		// Integrity violations are NOT blanket-mapped here — a unique violation's
		// right code is resource-specific (409) and stays with the handler; the
		// shared sink must not swallow it into a 400.
		{"pg_unique_violation_stays_500", &pgconn.PgError{Code: "23505"}, http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			DBError(rec, c.err)
			if rec.Code != c.want {
				t.Fatalf("%s: want %d got %d", c.name, c.want, rec.Code)
			}
		})
	}
}

// A 500 must not leak internal error detail (schema names, SQL, driver text) to
// the client — that is reconnaissance for an attacker. The body message must be
// generic even though the underlying error names a table.
func TestDBErrorDoesNotLeakInternalDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	leaky := errString(`ERROR: relation "iam.refresh_tokens" does not exist (SQLSTATE 42P01)`)
	DBError(rec, leaky)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 got %d", rec.Code)
	}
	var out map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if strings.Contains(out["message"], "refresh_tokens") || strings.Contains(out["message"], "SQLSTATE") || strings.Contains(out["message"], "relation") {
		t.Fatalf("500 body leaked internal error detail: %q", out["message"])
	}
}

type errString string

func (e errString) Error() string { return string(e) }
