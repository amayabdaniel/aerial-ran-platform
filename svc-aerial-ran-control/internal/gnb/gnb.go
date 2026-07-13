// Package gnb is a thin client over the wavekube GNodeB custom resource
// (ran.wavekube.io/v1alpha1). It lets the control plane declaratively request a
// GPU-accelerated cell; the wavekube operator (separate repo) reconciles it into
// an Aerial deployment + GPU scheduling + security policy.
//
// This is the Phase-3 bridge between aerial-ran-platform (business/control plane)
// and wavekube (the RAN lifecycle operator).
package gnb

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/apimachinery/pkg/api/errors"
)

// GVR for wavekube GNodeB resources.
var gvr = schema.GroupVersionResource{
	Group:    "ran.wavekube.io",
	Version:  "v1alpha1",
	Resource: "gnodebs",
}

const (
	apiVersion = "ran.wavekube.io/v1alpha1"
	kind       = "GNodeB"
)

// Client wraps a dynamic client scoped to one namespace.
type Client struct {
	dyn       dynamic.Interface
	namespace string
}

// CreateRequest is the API-facing shape; it maps to the GNodeB spec.
type CreateRequest struct {
	Name         string `json:"name"`
	Image        string `json:"image,omitempty"`
	GPUCount     int    `json:"gpu_count,omitempty"`
	GPUType      string `json:"gpu_type,omitempty"`
	EnableRDMA   bool   `json:"enable_rdma,omitempty"`
	Bandwidth    int    `json:"bandwidth,omitempty"`   // MHz: 5..100
	Numerology   int    `json:"numerology,omitempty"`  // 0..4
	Band         string `json:"band,omitempty"`        // e.g. n78
	MaxUEs       int    `json:"max_ues,omitempty"`
	Fronthaul    string `json:"fronthaul_interface,omitempty"`
	SecurityRef  string `json:"security_policy_ref,omitempty"`
}

// GNodeB is a trimmed view returned to callers.
type GNodeB struct {
	Name          string `json:"name"`
	Namespace     string `json:"namespace"`
	Band          string `json:"band"`
	Bandwidth     int    `json:"bandwidth"`
	GPUType       string `json:"gpu_type"`
	Phase         string `json:"phase"`
	ReadyReplicas int    `json:"ready_replicas"`
	Image         string `json:"image"`
}

// New builds a Client. It prefers in-cluster config, then falls back to the
// default kubeconfig loading rules (KUBECONFIG / ~/.kube/config) for local dev.
func New(namespace string) (*Client, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("no in-cluster config and no kubeconfig: %w", err)
		}
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	if namespace == "" {
		namespace = "aerial"
	}
	return &Client{dyn: dyn, namespace: namespace}, nil
}

// Create makes a GNodeB CR from the request, filling sensible defaults.
func (c *Client) Create(ctx context.Context, req CreateRequest) (*GNodeB, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name required")
	}
	def := func(s, d string) string { if s == "" { return d }; return s }
	defi := func(i, d int) int { if i == 0 { return d }; return i }

	spec := map[string]any{
		"image":    def(req.Image, "nvcr.io/nvidia/aerial/aerial-ran:24.3"),
		"replicas": int64(1),
		"gpuResources": map[string]any{
			"count":      int64(defi(req.GPUCount, 1)),
			"type":       def(req.GPUType, "L4"),
			"enableRDMA": req.EnableRDMA,
		},
		"phyConfig": map[string]any{
			"bandwidth":  int64(defi(req.Bandwidth, 100)),
			"numerology": int64(req.Numerology), // 0 is valid
			"band":       def(req.Band, "n78"),
			"maxUEs":     int64(defi(req.MaxUEs, 32)),
		},
		"network": map[string]any{
			"fronthaulInterface": def(req.Fronthaul, "eth0"),
		},
	}
	if req.SecurityRef != "" {
		spec["securityPolicyRef"] = req.SecurityRef
	}

	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name":      req.Name,
			"namespace": c.namespace,
		},
		"spec": spec,
	}}

	created, err := c.dyn.Resource(gvr).Namespace(c.namespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return toView(created), nil
}

// List returns all GNodeBs in the namespace.
func (c *Client) List(ctx context.Context) ([]*GNodeB, error) {
	l, err := c.dyn.Resource(gvr).Namespace(c.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]*GNodeB, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, toView(&l.Items[i]))
	}
	return out, nil
}

// Get fetches one GNodeB by name.
func (c *Client) Get(ctx context.Context, name string) (*GNodeB, error) {
	o, err := c.dyn.Resource(gvr).Namespace(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return toView(o), nil
}

// Delete removes a GNodeB by name.
func (c *Client) Delete(ctx context.Context, name string) error {
	return c.dyn.Resource(gvr).Namespace(c.namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// IsNotFound reports whether err is a k8s NotFound (for handler mapping).
func IsNotFound(err error) bool { return errors.IsNotFound(err) }

// IsAlreadyExists reports whether err is a k8s AlreadyExists.
func IsAlreadyExists(err error) bool { return errors.IsAlreadyExists(err) }

// Namespace returns the namespace the client manages.
func (c *Client) Namespace() string { return c.namespace }

func toView(o *unstructured.Unstructured) *GNodeB {
	g := &GNodeB{Name: o.GetName(), Namespace: o.GetNamespace()}
	g.Band, _, _ = unstructured.NestedString(o.Object, "spec", "phyConfig", "band")
	g.Image, _, _ = unstructured.NestedString(o.Object, "spec", "image")
	g.GPUType, _, _ = unstructured.NestedString(o.Object, "spec", "gpuResources", "type")
	if bw, ok, _ := unstructured.NestedInt64(o.Object, "spec", "phyConfig", "bandwidth"); ok {
		g.Bandwidth = int(bw)
	}
	g.Phase, _, _ = unstructured.NestedString(o.Object, "status", "phase")
	if g.Phase == "" {
		g.Phase = "Pending"
	}
	if rr, ok, _ := unstructured.NestedInt64(o.Object, "status", "readyReplicas"); ok {
		g.ReadyReplicas = int(rr)
	}
	return g
}
