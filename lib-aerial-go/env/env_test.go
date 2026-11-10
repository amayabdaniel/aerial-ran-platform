package env

import (
	"os"
	"testing"
)

func TestOr(t *testing.T) {
	const key = "AERIAL_ENV_OR_TEST"

	// Unset → default.
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}
	if got := Or(key, "fallback"); got != "fallback" {
		t.Fatalf("unset: got %q, want fallback", got)
	}

	// Empty → default (an explicitly empty var is treated as unset).
	t.Setenv(key, "")
	if got := Or(key, "fallback"); got != "fallback" {
		t.Fatalf("empty: got %q, want fallback", got)
	}

	// Set → value.
	t.Setenv(key, "configured")
	if got := Or(key, "fallback"); got != "configured" {
		t.Fatalf("set: got %q, want configured", got)
	}
}
