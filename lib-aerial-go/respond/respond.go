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

// DBError maps context cancellation and deadline to 499/504; falls back to 500.
func DBError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		Error(w, http.StatusGatewayTimeout, "deadline_exceeded", "request timed out")
	case errors.Is(err, context.Canceled):
		// nginx uses 499 for client closed request; we mirror that here.
		Error(w, 499, "canceled", "request canceled")
	default:
		Error(w, http.StatusInternalServerError, "internal", err.Error())
	}
}
