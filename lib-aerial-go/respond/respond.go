// Package respond writes JSON HTTP responses with a pooled encoder buffer.
// It also maps common errors (context cancel/deadline) to canonical codes.
package respond

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
)

var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// JSON writes v as JSON with the given status code.
func JSON(w http.ResponseWriter, status int, v any) {
	buf := bufPool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		bufPool.Put(buf)
	}()
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		http.Error(w, `{"error":"encode_failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// Error writes a JSON error body.
func Error(w http.ResponseWriter, status int, code, msg string) {
	JSON(w, status, map[string]string{"code": code, "message": msg})
}

// DBError maps context cancellation and deadline to 499/504, a malformed-input
// database error to 400, and falls back to 500.
func DBError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		Error(w, http.StatusGatewayTimeout, "deadline_exceeded", "request timed out")
	case errors.Is(err, context.Canceled):
		// nginx uses 499 for client closed request; we mirror that here.
		Error(w, 499, "canceled", "request canceled")
	case errors.As(err, &pgErr) && pgErr.Code == "22P02":
		// 22P02 invalid_text_representation: a value the column's type could not
		// parse (e.g. a non-UUID id in the path, cast via $n::uuid). Every such
		// cast in this codebase is of a client-supplied value, so this is the
		// caller's malformed input, not a server fault — answer 400, not 500, so
		// it does not read as an outage or add noise to 5xx alerting. Don't echo
		// the raw driver message. Integrity violations (23xxx) are deliberately
		// left to the default: their right code is resource-specific (409/404)
		// and belongs in the handler, not this shared sink.
		Error(w, http.StatusBadRequest, "bad_request", "malformed value in request")
	default:
		Error(w, http.StatusInternalServerError, "internal", err.Error())
	}
}
