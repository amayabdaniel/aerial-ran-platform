package provider

import (
	"context"
	"strings"
	"testing"
)

func TestMockCatalogScopedByRegion(t *testing.T) {
	m := NewMock()
	offers, err := m.Catalog(context.Background(), "CO")
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if len(offers) == 0 {
		t.Fatal("expected non-empty catalog")
	}
	for _, o := range offers {
		if o.Region != "CO" {
			t.Fatalf("offer region %q != CO", o.Region)
		}
		if o.DataMB <= 0 || o.ValidityDays <= 0 || o.PriceUSDCents <= 0 {
			t.Fatalf("offer has nonpositive fields: %+v", o)
		}
		if !strings.HasPrefix(o.ID, "mock-CO-") {
			t.Fatalf("offer id %q missing region prefix", o.ID)
		}
	}
}

func TestMockCatalogDefaultsRegion(t *testing.T) {
	m := NewMock()
	offers, _ := m.Catalog(context.Background(), "")
	for _, o := range offers {
		if o.Region != "GLOBAL" {
			t.Fatalf("empty region should default to GLOBAL, got %q", o.Region)
		}
	}
}

func TestMockOrderProducesLPAAndICCID(t *testing.T) {
	m := NewMock()
	res, err := m.Order(context.Background(), "mock-CO-5g-30d", "ext-ref")
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	if !strings.HasPrefix(res.LPAString, "LPA:1$") {
		t.Fatalf("LPA string %q not in GSMA SGP.22 shape", res.LPAString)
	}
	if len(res.ICCID) < 18 {
		t.Fatalf("ICCID %q too short", res.ICCID)
	}
	if res.ProviderRef == "" {
		t.Fatal("expected a provider ref")
	}
	if res.ExpiresAt == nil {
		t.Fatal("expected an expiry")
	}
	if !strings.Contains(res.InstallURL, "esimsetup.apple.com") {
		t.Fatalf("install URL should be an Apple universal link, got %q", res.InstallURL)
	}
}

func TestMockOrdersAreUnique(t *testing.T) {
	m := NewMock()
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		res, _ := m.Order(context.Background(), "mock-US-1g-7d", "ref")
		if seen[res.LPAString] {
			t.Fatalf("duplicate LPA generated: %s", res.LPAString)
		}
		seen[res.LPAString] = true
	}
}

func TestMockUsageGrowsMonotonicallyAndCaps(t *testing.T) {
	m := NewMock()
	res, _ := m.Order(context.Background(), "mock-CO-1g-7d", "ref") // 1024 MB cap
	prev := -1
	for i := 0; i < 20; i++ {
		u, err := m.UsageMB(context.Background(), res.ProviderRef)
		if err != nil {
			t.Fatalf("usage: %v", err)
		}
		if u < prev {
			t.Fatalf("usage decreased: %d < %d", u, prev)
		}
		if u > 1024 {
			t.Fatalf("usage exceeded cap: %d > 1024", u)
		}
		prev = u
	}
	if prev != 1024 {
		t.Fatalf("usage should have capped at 1024, ended at %d", prev)
	}
}

func TestMockUsageUnknownRefErrors(t *testing.T) {
	m := NewMock()
	if _, err := m.UsageMB(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("expected error for unknown providerRef")
	}
}

func TestMockCancelRemovesOrder(t *testing.T) {
	m := NewMock()
	res, _ := m.Order(context.Background(), "mock-CO-5g-30d", "ref")
	if err := m.Cancel(context.Background(), res.ProviderRef); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := m.UsageMB(context.Background(), res.ProviderRef); err == nil {
		t.Fatal("usage after cancel should error")
	}
}
