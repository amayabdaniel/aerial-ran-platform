// Mock provider lets the platform run end-to-end without real eSIM credentials.
// Returns a deterministic LPA string that any GSMA SGP.22 LPA *will reject*
// (it points at our nonexistent test SM-DP+), but the data flow is real:
// catalog → order → LPA → QR rendered in the UI → status → usage.
package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Mock satisfies Provider with in-memory state.
type Mock struct {
	mu    sync.Mutex
	state map[string]*mockOrder // providerRef → state
}

type mockOrder struct {
	packageID string
	createdAt time.Time
	expiresAt time.Time
	dataMB    int
	usageMB   int
}

// NewMock returns a Mock provider.
func NewMock() *Mock { return &Mock{state: map[string]*mockOrder{}} }

// Name returns "mock".
func (*Mock) Name() string { return "mock" }

// Catalog returns a small synthetic catalog scoped by region.
// Region examples: "CO", "US", "EU".
func (*Mock) Catalog(_ context.Context, region string) ([]PackageOffering, error) {
	if region == "" {
		region = "GLOBAL"
	}
	return []PackageOffering{
		{ID: "mock-" + region + "-1g-7d", Label: "1 GB / 7 days (" + region + ")", Region: region, DataMB: 1024, ValidityDays: 7, PriceUSDCents: 450},
		{ID: "mock-" + region + "-3g-30d", Label: "3 GB / 30 days (" + region + ")", Region: region, DataMB: 3072, ValidityDays: 30, PriceUSDCents: 1100},
		{ID: "mock-" + region + "-5g-30d", Label: "5 GB / 30 days (" + region + ")", Region: region, DataMB: 5120, ValidityDays: 30, PriceUSDCents: 1900},
		{ID: "mock-" + region + "-20g-30d", Label: "20 GB / 30 days (" + region + ")", Region: region, DataMB: 20480, ValidityDays: 30, PriceUSDCents: 4900},
	}, nil
}

// Order returns a fake LPA that mirrors GSMA SGP.22 format so the UI can render a real QR.
func (m *Mock) Order(_ context.Context, packageID, _ string) (*OrderResult, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	matchingID := hex.EncodeToString(b)
	providerRef := "mock-ord-" + matchingID

	// pick a sensible default duration/data
	days := 30
	mb := 5120
	switch {
	case stringEndsWith(packageID, "-1g-7d"):
		days, mb = 7, 1024
	case stringEndsWith(packageID, "-3g-30d"):
		days, mb = 30, 3072
	case stringEndsWith(packageID, "-20g-30d"):
		days, mb = 30, 20480
	}

	exp := time.Now().Add(time.Duration(days) * 24 * time.Hour)
	m.mu.Lock()
	m.state[providerRef] = &mockOrder{packageID: packageID, createdAt: time.Now(), expiresAt: exp, dataMB: mb}
	m.mu.Unlock()

	iccid := "8900000000" + matchingID[:9] // 19-char synthetic ICCID
	lpa := "LPA:1$mock-smdp.aerial.local$" + matchingID
	installURL := "https://esimsetup.apple.com/esim_qrcode_provisioning?carddata=" + lpa

	return &OrderResult{
		ProviderRef: providerRef,
		ICCID:       iccid,
		LPAString:   lpa,
		InstallURL:  installURL,
		ExpiresAt:   &exp,
	}, nil
}

// UsageMB simulates monotonically-increasing usage (10% of cap per call, capped).
func (m *Mock) UsageMB(_ context.Context, providerRef string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.state[providerRef]
	if !ok {
		return 0, fmt.Errorf("unknown providerRef: %s", providerRef)
	}
	o.usageMB += o.dataMB / 10
	if o.usageMB > o.dataMB {
		o.usageMB = o.dataMB
	}
	return o.usageMB, nil
}

// Cancel drops the order from in-memory state.
func (m *Mock) Cancel(_ context.Context, providerRef string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.state, providerRef)
	return nil
}

func stringEndsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
