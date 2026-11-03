package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// Proxy contains the display metadata returned by the core. Connection settings
// and credentials are deliberately absent from this model.
type Proxy struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Now     string         `json:"now"`
	All     []string       `json:"all"`
	UDP     bool           `json:"udp"`
	History []ProxyHistory `json:"history"`
}

type ProxyHistory struct {
	Time  time.Time `json:"time"`
	Delay int       `json:"delay"`
}

func (p Proxy) IsGroup() bool {
	if p.All != nil {
		return true
	}
	switch p.Type {
	case "Selector", "URLTest", "Fallback", "LoadBalance", "Relay":
		return true
	}
	return false
}

func (c Client) Proxies(ctx context.Context) (map[string]Proxy, error) {
	raw, err := c.request(ctx, http.MethodGet, "/proxies", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Proxies map[string]Proxy `json:"proxies"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.Proxies == nil {
		return nil, diagnostic.New("diagnostic.api.proxyResponse", "invalid Mihomo proxy response")
	}
	for name, proxy := range result.Proxies {
		// References use the map key; never trust a conflicting embedded name.
		proxy.Name = name
		result.Proxies[name] = proxy
	}
	return result.Proxies, nil
}

// ProxyGroups derives the group directory from the same observation as metadata.
func ProxyGroups(proxies map[string]Proxy) []Group {
	groups := make([]Group, 0)
	for name, p := range proxies {
		if p.IsGroup() {
			groups = append(groups, Group{Name: name, Type: p.Type, Now: p.Now, All: p.All})
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups
}

const DefaultProxyDelayURL = "https://www.gstatic.com/generate_204"
const ProxyDelayTimeout = 5 * time.Second

// ProxyDelay is one test observation, distinct from the core's older history.
// A failed/timeout observation never carries a fabricated zero-millisecond delay.
type ProxyDelay struct {
	State string
	Delay int
	Time  time.Time
	URL   string
}

func (c Client) TestProxyDelay(ctx context.Context, node string) (ProxyDelay, error) {
	result := ProxyDelay{URL: DefaultProxyDelayURL}
	if node == "" {
		return result, diagnostic.New("diagnostic.api.nodeUnavailable", "the group or node is no longer available; refresh the nodes page")
	}
	// Allow the core's five-second timer to produce its authoritative HTTP 504
	// before our bounded HTTP request expires. Preserve the caller's deadline.
	client := &http.Client{}
	if c.HTTP != nil {
		*client = *c.HTTP
	}
	client.Timeout = ProxyDelayTimeout + 2*time.Second
	c.HTTP = client
	query := url.Values{"url": {DefaultProxyDelayURL}, "timeout": {strconv.FormatInt(ProxyDelayTimeout.Milliseconds(), 10)}}
	raw, err := c.request(ctx, http.MethodGet, "/proxies/"+url.PathEscape(node)+"/delay?"+query.Encode(), nil)
	result.Time = time.Now()
	if err != nil {
		var status *apiHTTPError
		if errors.As(err, &status) {
			switch status.status {
			case http.StatusGatewayTimeout:
				result.State = "timeout"
				return result, nil
			case http.StatusServiceUnavailable:
				result.State = "failed"
				return result, nil
			}
		}
		return result, err
	}
	var response struct {
		Delay int `json:"delay"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Delay <= 0 || response.Delay > 65535 {
		return result, diagnostic.New("diagnostic.api.delayResponse", "invalid Mihomo proxy delay response")
	}
	result.State, result.Delay = "success", response.Delay
	return result, nil
}
