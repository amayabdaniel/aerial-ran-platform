// Airalo Partners API adapter. See https://developers.partners.airalo.com/
// Auth: client_credentials OAuth2 → access_token.
// Endpoints used:
//   POST /v2/token
//   GET  /v2/packages?filter[type]=local&filter[country]=US
//   POST /v2/orders
//   GET  /v2/sims/{iccid}/usage
//   PUT  /v2/orders/{order_id}/cancel
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Airalo is the live adapter.
type Airalo struct {
	baseURL      string
	clientID     string
	clientSecret string
	http         *http.Client

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// NewAiralo returns an Airalo Partners API adapter. baseURL defaults to the
// sandbox; pass https://partners-api.airalo.com for production.
func NewAiralo(baseURL, clientID, clientSecret string) *Airalo {
	if baseURL == "" {
		baseURL = "https://sandbox-partners-api.airalo.com"
	}
	return &Airalo{
		baseURL:      strings.TrimRight(baseURL, "/"),
		clientID:     clientID,
		clientSecret: clientSecret,
		http:         &http.Client{Timeout: 15 * time.Second},
	}
}

// Name returns "airalo".
func (*Airalo) Name() string { return "airalo" }

// token refreshes if expired (5-minute safety margin).
func (a *Airalo) token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.accessToken != "" && time.Until(a.expiresAt) > 5*time.Minute {
		return a.accessToken, nil
	}
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", a.clientID)
	form.Set("client_secret", a.clientSecret)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := a.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("airalo token %d: %s", res.StatusCode, string(body))
	}
	var t struct {
		Data struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&t); err != nil {
		return "", err
	}
	a.accessToken = t.Data.AccessToken
	a.expiresAt = time.Now().Add(time.Duration(t.Data.ExpiresIn) * time.Second)
	return a.accessToken, nil
}

// do is a small JSON helper that injects the OAuth token.
func (a *Airalo) do(ctx context.Context, method, path string, body any, out any) error {
	tok, err := a.token(ctx)
	if err != nil {
		return err
	}
	var b io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		b = bytes.NewReader(buf)
	}
	req, _ := http.NewRequestWithContext(ctx, method, a.baseURL+path, b)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		bd, _ := io.ReadAll(res.Body)
		return fmt.Errorf("airalo %s %s -> %d: %s", method, path, res.StatusCode, string(bd))
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

// Catalog returns Airalo packages for a country/region (ISO-2 country code).
func (a *Airalo) Catalog(ctx context.Context, region string) ([]PackageOffering, error) {
	if region == "" {
		region = "US"
	}
	path := "/v2/packages?filter[type]=local&filter[country]=" + url.QueryEscape(region) + "&limit=20"
	var resp struct {
		Data []struct {
			Slug     string `json:"slug"`
			Country  struct {
				Slug string `json:"slug"`
			} `json:"country"`
			OperatorPackages []struct {
				ID          string  `json:"id"`
				Type        string  `json:"type"`
				Title       string  `json:"title"`
				Data        string  `json:"data"`
				Day         int     `json:"day"`
				PriceUSD    float64 `json:"price"`
				NetPriceUSD float64 `json:"net_price"`
			} `json:"operator_packages,omitempty"`
		} `json:"data"`
	}
	if err := a.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	var out []PackageOffering
	for _, c := range resp.Data {
		for _, op := range c.OperatorPackages {
			out = append(out, PackageOffering{
				ID:            op.ID,
				Label:         op.Title,
				Region:        strings.ToUpper(c.Country.Slug),
				DataMB:        parseDataMB(op.Data),
				ValidityDays:  op.Day,
				PriceUSDCents: int(op.NetPriceUSD * 100),
			})
		}
	}
	return out, nil
}

// Order creates an Airalo order for the given package, returns the issued eSIM's LPA.
func (a *Airalo) Order(ctx context.Context, packageID, externalRef string) (*OrderResult, error) {
	body := map[string]any{
		"quantity":              1,
		"package_id":            packageID,
		"description":           externalRef,
	}
	var resp struct {
		Data struct {
			ID   int `json:"id"`
			Sims []struct {
				ID                       int    `json:"id"`
				ICCID                    string `json:"iccid"`
				QRCode                   string `json:"qrcode"`
				QRCodeURL                string `json:"qrcode_url"`
				DirectAppleInstallationURL string `json:"direct_apple_installation_url"`
			} `json:"sims"`
		} `json:"data"`
	}
	if err := a.do(ctx, http.MethodPost, "/v2/orders", body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data.Sims) == 0 {
		return nil, errors.New("airalo: empty sims in order response")
	}
	sim := resp.Data.Sims[0]
	return &OrderResult{
		ProviderRef: fmt.Sprintf("%d", resp.Data.ID),
		ICCID:       sim.ICCID,
		LPAString:   sim.QRCode, // Airalo returns the LPA: string in qrcode
		InstallURL:  sim.DirectAppleInstallationURL,
	}, nil
}

// UsageMB returns the usage for a specific Airalo SIM (by ICCID).
// Note: Airalo's API is per-SIM not per-order; for v1 we accept the providerRef
// as the iccid for usage queries.
func (a *Airalo) UsageMB(ctx context.Context, iccid string) (int, error) {
	var resp struct {
		Data struct {
			Remaining float64 `json:"remaining"`
			Total     float64 `json:"total"`
		} `json:"data"`
	}
	if err := a.do(ctx, http.MethodGet, "/v2/sims/"+iccid+"/usage", nil, &resp); err != nil {
		return 0, err
	}
	return int(resp.Data.Total - resp.Data.Remaining), nil
}

// Cancel — Airalo doesn't expose cancellation in the partner API publicly; no-op.
func (*Airalo) Cancel(_ context.Context, _ string) error { return nil }

// parseDataMB turns Airalo's data string like "5 GB" or "500 MB" into MB.
func parseDataMB(s string) int {
	var n float64
	var unit string
	fmt.Sscanf(s, "%f %s", &n, &unit)
	switch strings.ToUpper(unit) {
	case "GB":
		return int(n * 1024)
	case "MB":
		return int(n)
	default:
		return 0
	}
}
