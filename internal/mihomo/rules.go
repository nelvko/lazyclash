package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// Rule is one entry in Mihomo's effective matching order. Index is the core's
// zero-based position; Size is -1 when a record count does not apply.
type Rule struct {
	Index   int        `json:"index"`
	Type    string     `json:"type"`
	Payload string     `json:"payload"`
	Proxy   string     `json:"proxy"`
	Size    int        `json:"size"`
	Extra   *RuleExtra `json:"extra,omitempty"`
}

// RuleExtra is optional information supplied by a core's rule wrapper. Its
// absence does not imply a disabled rule or a known zero hit count.
type RuleExtra struct {
	Disabled  bool      `json:"disabled"`
	HitCount  uint64    `json:"hitCount"`
	HitAt     time.Time `json:"hitAt"`
	MissCount uint64    `json:"missCount"`
	MissAt    time.Time `json:"missAt"`
}

// Rules reads the effective rules without editing, sorting or expanding rule
// providers. Equal rules at different positions remain separate entries.
func (c Client) Rules(ctx context.Context) ([]Rule, error) {
	raw, err := c.request(ctx, http.MethodGet, "/rules", nil)
	if err != nil {
		return nil, err
	}
	invalid := diagnostic.New("diagnostic.api.rulesResponse", "invalid Mihomo rules response")
	var response struct {
		Rules json.RawMessage `json:"rules"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Rules) == 0 || bytes.Equal(bytes.TrimSpace(response.Rules), []byte("null")) {
		return nil, invalid
	}
	var entries []struct {
		Index   *int       `json:"index"`
		Type    *string    `json:"type"`
		Payload *string    `json:"payload"`
		Proxy   *string    `json:"proxy"`
		Size    *int       `json:"size"`
		Extra   *RuleExtra `json:"extra,omitempty"`
	}
	if json.Unmarshal(response.Rules, &entries) != nil {
		return nil, invalid
	}
	rules := make([]Rule, 0, len(entries))
	seen := make(map[int]bool, len(entries))
	for _, entry := range entries {
		if entry.Index == nil || *entry.Index < 0 || seen[*entry.Index] || entry.Type == nil || strings.TrimSpace(*entry.Type) == "" || entry.Payload == nil || entry.Proxy == nil || entry.Size == nil || *entry.Size < -1 {
			return nil, invalid
		}
		seen[*entry.Index] = true
		rules = append(rules, Rule{Index: *entry.Index, Type: *entry.Type, Payload: *entry.Payload, Proxy: *entry.Proxy, Size: *entry.Size, Extra: entry.Extra})
	}
	return rules, nil
}
