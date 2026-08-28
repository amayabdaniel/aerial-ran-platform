package ranctl

import (
	"context"
	"errors"
	"testing"
)

type fakeCounter struct {
	n   int64
	err error
}

func (f fakeCounter) Count(context.Context) (int64, error) { return f.n, f.err }

func newStatusSvc(c subscriberCounter) *Service {
	// empty nfEndpoints → the NF probe loop is skipped, so no HTTP is needed.
	return &Service{plmn: "999/70", nfEndpoints: map[string]string{}, counter: c}
}

// The bug: a failed subscriber count must not read as "0 subscribers".
func TestStatusSurfacesCountError(t *testing.T) {
	s := newStatusSvc(fakeCounter{err: errors.New("mongo unreachable")})
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("Status should not hard-fail on a count error: %v", err)
	}
	if st.SubscribersError == "" {
		t.Fatal("count failure must be surfaced in SubscribersError, not swallowed to 0")
	}
	if st.Subscribers != 0 {
		t.Fatalf("Subscribers should stay 0 on error, got %d", st.Subscribers)
	}
}

func TestStatusReportsCount(t *testing.T) {
	s := newStatusSvc(fakeCounter{n: 5})
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Subscribers != 5 {
		t.Fatalf("Subscribers want 5 got %d", st.Subscribers)
	}
	if st.SubscribersError != "" {
		t.Fatalf("no error expected, got %q", st.SubscribersError)
	}
}

func TestStatusNoCounter(t *testing.T) {
	s := newStatusSvc(nil) // no 5G core reachable
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Subscribers != 0 || st.SubscribersError != "" {
		t.Fatalf("nil counter: want 0/empty, got %d/%q", st.Subscribers, st.SubscribersError)
	}
	if st.PLMN != "999/70" {
		t.Fatalf("plmn passthrough wrong: %q", st.PLMN)
	}
}
