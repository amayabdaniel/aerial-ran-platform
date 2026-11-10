// Package env reads environment variables with fallback defaults. It is a leaf
// package (standard library only) so configuration code can depend on it without
// importing the service runner — the composition root — just to read an env var.
package env

import "os"

// Or returns the value of environment variable k, or def when k is unset or
// empty. An explicitly empty variable is treated as unset, matching the
// behavior the services relied on when this lived in the runner package.
func Or(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
