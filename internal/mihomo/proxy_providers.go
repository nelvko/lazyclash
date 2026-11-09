package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// ProxyProvider is an observation of a loaded source. Node details, URLs,
// subscription information and health check settings remain inside the core.
type ProxyProvider struct {
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	VehicleType string    `json:"vehicleType"`
	ProxyCount  int       `json:"proxyCount"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (c Client) ProxyProviders(ctx context.Context) ([]ProxyProvider, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	raw, err := c.request(ctx, http.MethodGet, "/providers/proxies", nil)
	if err != nil {
		return nil, err
	}
	invalid := diagnostic.New("diagnostic.api.proxyProvidersResponse", "invalid Mihomo proxy providers response")
	var response struct {
		Providers json.RawMessage `json:"providers"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Providers) == 0 || bytes.Equal(bytes.TrimSpace(response.Providers), []byte("null")) {
		return nil, invalid
	}
	var entries map[string]*struct {
		Name        *string         `json:"name"`
		Type        *string         `json:"type"`
		VehicleType *string         `json:"vehicleType"`
		Proxies     json.RawMessage `json:"proxies"`
		UpdatedAt   *time.Time      `json:"updatedAt"`
	}
	if json.Unmarshal(response.Providers, &entries) != nil {
		return nil, invalid
	}
	providers := make([]ProxyProvider, 0, len(entries))
	for name, entry := range entries {
		if strings.TrimSpace(name) == "" || entry == nil || entry.Name == nil || *entry.Name != name || entry.Type == nil || *entry.Type != "Proxy" || entry.VehicleType == nil || strings.TrimSpace(*entry.VehicleType) == "" || len(entry.Proxies) == 0 || bytes.Equal(bytes.TrimSpace(entry.Proxies), []byte("null")) {
			return nil, invalid
		}
		var proxies []json.RawMessage
		if json.Unmarshal(entry.Proxies, &proxies) != nil {
			return nil, invalid
		}
		updated := time.Time{}
		if entry.UpdatedAt != nil {
			updated = *entry.UpdatedAt
		}
		providers = append(providers, ProxyProvider{Name: name, Type: *entry.Type, VehicleType: *entry.VehicleType, ProxyCount: len(proxies), UpdatedAt: updated})
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	return providers, nil
}

// UpdateProxyProvider preserves a shorter caller/client deadline and never
// modifies the caller's HTTP client. Source eligibility is checked by the app.
func (c Client) UpdateProxyProvider(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return diagnostic.New("diagnostic.api.proxyProviderName", "proxy provider name must not be empty")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 30 * time.Second}
	if c.HTTP != nil {
		*client = *c.HTTP
		if client.Timeout <= 0 {
			client.Timeout = 30 * time.Second
		}
	}
	c.HTTP = client
	_, err := c.request(ctx, http.MethodPut, "/providers/proxies/"+url.PathEscape(name), nil)
	return err
}
