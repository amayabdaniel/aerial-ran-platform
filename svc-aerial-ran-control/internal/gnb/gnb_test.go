package gnb

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
)

// newTestClient wires a Client onto an in-memory fake dynamic client so the CRUD
// logic can be exercised without a real cluster.
func newTestClient(objs ...runtime.Object) *Client {
	scheme := runtime.NewScheme()
	listKinds := map[schema.GroupVersionResource]string{gvr: "GNodeBList"}
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listKinds, objs...)
	return &Client{dyn: dyn, namespace: "aerial"}
}

func TestCreateAppliesDefaults(t *testing.T) {
	c := newTestClient()
	g, err := c.Create(context.Background(), CreateRequest{Name: "cell-1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if g.Name != "cell-1" || g.Namespace != "aerial" {
		t.Fatalf("identity wrong: %+v", g)
	}
	// Defaults from Create().
	if g.Band != "n78" {
		t.Fatalf("band default want n78 got %q", g.Band)
	}
	if g.Bandwidth != 100 {
		t.Fatalf("bandwidth default want 100 got %d", g.Bandwidth)
	}
	if g.GPUType != "L4" {
		t.Fatalf("gpu type default want L4 got %q", g.GPUType)
	}
	// No status yet → Pending.
	if g.Phase != "Pending" {
		t.Fatalf("phase want Pending got %q", g.Phase)
	}

	// The stored object must carry the full spec the operator reconciles.
	raw, err := c.dyn.Resource(gvr).Namespace("aerial").Get(context.Background(), "cell-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("raw get: %v", err)
	}
	assertNestedInt64(t, raw, 100, "spec", "phyConfig", "bandwidth")
	assertNestedInt64(t, raw, 0, "spec", "phyConfig", "numerology") // 0 is valid, not defaulted
	assertNestedInt64(t, raw, 32, "spec", "phyConfig", "maxUEs")
	assertNestedInt64(t, raw, 1, "spec", "gpuResources", "count")
	assertNestedString(t, raw, "eth0", "spec", "network", "fronthaulInterface")
	assertNestedString(t, raw, "nvcr.io/nvidia/aerial/aerial-ran:24.3", "spec", "image")
}

func TestCreatePropagatesExplicitValues(t *testing.T) {
	c := newTestClient()
	g, err := c.Create(context.Background(), CreateRequest{
		Name: "cell-2", Band: "n41", Bandwidth: 40, Numerology: 1, GPUType: "H100",
		GPUCount: 2, MaxUEs: 64, Fronthaul: "eth1", SecurityRef: "strict",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if g.Band != "n41" || g.Bandwidth != 40 || g.GPUType != "H100" {
		t.Fatalf("view mismatch: %+v", g)
	}
	raw, _ := c.dyn.Resource(gvr).Namespace("aerial").Get(context.Background(), "cell-2", metav1.GetOptions{})
	assertNestedInt64(t, raw, 1, "spec", "phyConfig", "numerology")
	assertNestedInt64(t, raw, 2, "spec", "gpuResources", "count")
	assertNestedString(t, raw, "strict", "spec", "securityPolicyRef")
}

func TestCreateEmptyNameIsInvalidRequest(t *testing.T) {
	c := newTestClient()
	_, err := c.Create(context.Background(), CreateRequest{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	// The bug fix: this must be classifiable as a client input error (→ HTTP 400),
	// not a generic error that the handler would surface as a 502 gateway error.
	if !IsInvalidRequest(err) {
		t.Fatalf("empty-name error should be IsInvalidRequest, got %v", err)
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty-name error should wrap ErrInvalidRequest, got %v", err)
	}
}

func TestGetListDeleteRoundTrip(t *testing.T) {
	c := newTestClient()
	if _, err := c.Create(context.Background(), CreateRequest{Name: "a"}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if _, err := c.Create(context.Background(), CreateRequest{Name: "b"}); err != nil {
		t.Fatalf("create b: %v", err)
	}

	list, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list want 2 got %d", len(list))
	}

	got, err := c.Get(context.Background(), "a")
	if err != nil || got.Name != "a" {
		t.Fatalf("get a: %v %+v", err, got)
	}

	if err := c.Delete(context.Background(), "a"); err != nil {
		t.Fatalf("delete a: %v", err)
	}
	if _, err := c.Get(context.Background(), "a"); !IsNotFound(err) {
		t.Fatalf("get after delete want NotFound, got %v", err)
	}
}

func TestCreateDuplicateIsAlreadyExists(t *testing.T) {
	c := newTestClient()
	if _, err := c.Create(context.Background(), CreateRequest{Name: "dup"}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := c.Create(context.Background(), CreateRequest{Name: "dup"})
	if !IsAlreadyExists(err) {
		t.Fatalf("duplicate create want AlreadyExists, got %v", err)
	}
}

func TestGetMissingIsNotFound(t *testing.T) {
	c := newTestClient()
	if _, err := c.Get(context.Background(), "ghost"); !IsNotFound(err) {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestToViewReadsStatus(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]any{"name": "s", "namespace": "aerial"},
		"spec":       map[string]any{"phyConfig": map[string]any{"band": "n78", "bandwidth": int64(50)}},
		"status":     map[string]any{"phase": "Running", "readyReplicas": int64(1)},
	}}
	g := toView(obj)
	if g.Phase != "Running" {
		t.Fatalf("phase want Running got %q", g.Phase)
	}
	if g.ReadyReplicas != 1 {
		t.Fatalf("readyReplicas want 1 got %d", g.ReadyReplicas)
	}
	if g.Bandwidth != 50 {
		t.Fatalf("bandwidth want 50 got %d", g.Bandwidth)
	}
}

// ── helpers ──────────────────────────────────────────────────────────

func assertNestedInt64(t *testing.T, o *unstructured.Unstructured, want int64, path ...string) {
	t.Helper()
	got, ok, err := unstructured.NestedInt64(o.Object, path...)
	if err != nil || !ok {
		t.Fatalf("nested int64 %v: ok=%v err=%v", path, ok, err)
	}
	if got != want {
		t.Fatalf("nested int64 %v want %d got %d", path, want, got)
	}
}

func assertNestedString(t *testing.T, o *unstructured.Unstructured, want string, path ...string) {
	t.Helper()
	got, ok, err := unstructured.NestedString(o.Object, path...)
	if err != nil || !ok {
		t.Fatalf("nested string %v: ok=%v err=%v", path, ok, err)
	}
	if got != want {
		t.Fatalf("nested string %v want %q got %q", path, want, got)
	}
}
