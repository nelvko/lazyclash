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

// RuleProvider describes a rule source loaded by the core. Inline providers
// have no file format; a zero UpdatedAt remains unknown rather than "now".
type RuleProvider struct {
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	VehicleType string    `json:"vehicleType"`
	Behavior    string    `json:"behavior"`
	Format      string    `json:"format"`
	RuleCount   int       `json:"ruleCount"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// RuleProviders reads metadata only. Provider files, URLs and inline payloads
// stay in the core; the API's unordered map is returned in stable name order.
func (c Client) RuleProviders(ctx context.Context) ([]RuleProvider, error) {
	raw, err := c.request(ctx, http.MethodGet, "/providers/rules", nil)
	if err != nil {
		return nil, err
	}
	invalid := diagnostic.New("diagnostic.api.ruleProvidersResponse", "invalid Mihomo rule providers response")
	var response struct {
		Providers json.RawMessage `json:"providers"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Providers) == 0 || bytes.Equal(bytes.TrimSpace(response.Providers), []byte("null")) {
		return nil, invalid
	}
	var entries map[string]*struct {
		Name        *string    `json:"name"`
		Type        *string    `json:"type"`
		VehicleType *string    `json:"vehicleType"`
		Behavior    *string    `json:"behavior"`
		Format      *string    `json:"format"`
		RuleCount   *int       `json:"ruleCount"`
		UpdatedAt   *time.Time `json:"updatedAt"`
	}
	if json.Unmarshal(response.Providers, &entries) != nil {
		return nil, invalid
	}
	providers := make([]RuleProvider, 0, len(entries))
	for name, entry := range entries {
		if strings.TrimSpace(name) == "" || entry == nil || entry.Name == nil || *entry.Name != name || entry.Type == nil || *entry.Type != "Rule" || entry.VehicleType == nil || strings.TrimSpace(*entry.VehicleType) == "" || entry.Behavior == nil || strings.TrimSpace(*entry.Behavior) == "" || entry.Format == nil || (strings.TrimSpace(*entry.Format) == "" && !strings.EqualFold(*entry.VehicleType, "Inline")) || entry.RuleCount == nil || *entry.RuleCount < 0 || entry.UpdatedAt == nil {
			return nil, invalid
		}
		providers = append(providers, RuleProvider{Name: name, Type: *entry.Type, VehicleType: *entry.VehicleType, Behavior: *entry.Behavior, Format: *entry.Format, RuleCount: *entry.RuleCount, UpdatedAt: *entry.UpdatedAt})
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	return providers, nil
}

// UpdateRuleProvider asks the core to reload one source. Downloads can take
// longer than ordinary reads, but both the request and caller lifetime remain
// bounded, and an explicitly shorter client timeout is preserved.
func (c Client) UpdateRuleProvider(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return diagnostic.New("diagnostic.api.ruleProviderName", "rule provider name must not be empty")
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
	_, err := c.request(ctx, http.MethodPut, "/providers/rules/"+url.PathEscape(name), nil)
	return err
}
