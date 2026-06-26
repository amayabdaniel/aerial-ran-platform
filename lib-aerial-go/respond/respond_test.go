package respond

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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

type errString string

func (e errString) Error() string { return string(e) }
