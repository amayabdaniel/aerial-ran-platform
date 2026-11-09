// Package bodylimit caps the size of an incoming request body so a single
// connection cannot force a service to buffer or parse an unbounded payload —
// a cheap memory-exhaustion DoS. Every JSON handler decodes r.Body; without
// this cap json.Decode would read as much as the client sends.
//
// It is the single shared implementation of that cap. runner.Run applies it for
// services built on the runner; services that hand-roll their middleware chain
// (iam, esim, subscriber) apply it the same way. Keeping one Middleware and one
// DefaultMaxBytes here means the limit cannot drift between services or go
// missing in a copy — a gap that already reached production once, when three
// hand-rolled chains shipped without any body limit at all.
package bodylimit

import "net/http"

// DefaultMaxBytes is the request-body cap for the control-plane JSON APIs.
// 4 MiB is generous for these endpoints; on overflow the wrapped read returns a
// *http.MaxBytesError, which handlers surface as 400/413.
const DefaultMaxBytes int64 = 4 << 20 // 4 MiB

// Middleware wraps r.Body in an http.MaxBytesReader so reads past max fail
// instead of allocating without bound.
func Middleware(max int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}
