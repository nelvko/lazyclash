package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

type Client struct {
	BaseURL string
	Secret  string
	HTTP    *http.Client
}

type Group struct {
	Name string   `json:"name"`
	Type string   `json:"type"`
	Now  string   `json:"now"`
	All  []string `json:"all"`
}

// open keeps buffered requests and streaming samples on the same authenticated,
// loopback-only transport. The returned body owns the private transport cleanup.
func (c Client) open(ctx context.Context, method, path string, body []byte) (io.ReadCloser, error) {
	return c.openRequest(ctx, method, path, body, false)
}

func (c Client) openRequest(ctx context.Context, method, path string, body []byte, stream bool) (io.ReadCloser, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, diagnostic.New("diagnostic.api.controllerURL", "invalid local controller URL")
	}
	if err := loopbackAddress(base.Host); err != nil {
		return nil, err
	}
	cl := &http.Client{Timeout: 5 * time.Second}
	if c.HTTP != nil {
		*cl = *c.HTTP
	}
	if cl.Timeout <= 0 {
		cl.Timeout = 5 * time.Second
	}
	if stream {
		// A quiet /logs connection intentionally has no response headers until
		// its first event. Bound connection setup, then let its owner cancel it.
		cl.Timeout = 0
	}
	transport := cl.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	closeIdle := func() {}
	if tr, ok := transport.(*http.Transport); ok {
		tr = tr.Clone()
		tr.Proxy = nil
		if stream {
			tr.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
			tr.TLSHandshakeTimeout = 5 * time.Second
			tr.ResponseHeaderTimeout = 0
		}
		transport = tr
		closeIdle = tr.CloseIdleConnections
	}
	cl.Transport = transport
	// Never follow redirects: the bearer token belongs to this local listener.
	cl.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		closeIdle()
		return nil, diagnostic.New("diagnostic.api.request", "cannot construct local controller request")
	}
	req.Header.Set("Authorization", "Bearer "+c.Secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := cl.Do(req)
	if err != nil {
		closeIdle()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, diagnostic.New("diagnostic.api.unreachable", "cannot reach local Mihomo API; check the service and controller port")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		closeIdle()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, diagnostic.New("diagnostic.api.httpCredentials", "Mihomo API returned HTTP %d; verify the managed listener and credentials", resp.StatusCode)
		}
		return nil, diagnostic.New("diagnostic.api.http", "Mihomo API returned HTTP %d", resp.StatusCode)
	}
	return apiBody{ReadCloser: resp.Body, closeIdle: closeIdle}, nil
}

type apiBody struct {
	io.ReadCloser
	closeIdle func()
}

func (b apiBody) Close() error {
	err := b.ReadCloser.Close()
	b.closeIdle()
	return err
}

func (c Client) request(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	response, err := c.open(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer response.Close()
	data, err := io.ReadAll(io.LimitReader(response, MaxConfigSize+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, diagnostic.New("diagnostic.api.readResponse", "cannot read Mihomo API response")
	}
	if len(data) > MaxConfigSize {
		return nil, diagnostic.New("diagnostic.api.responseSize", "Mihomo API response exceeds 8 MiB")
	}
	return data, nil
}

func (c Client) Ping(ctx context.Context) error {
	_, err := c.Version(ctx)
	return err
}

func (c Client) Version(ctx context.Context) (string, error) {
	raw, err := c.request(ctx, http.MethodGet, "/version", nil)
	if err != nil {
		return "", err
	}
	var v struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Version == "" {
		return "", diagnostic.New("diagnostic.api.version", "local listener did not return a Mihomo version")
	}
	return v.Version, nil
}

func (c Client) Groups(ctx context.Context) ([]Group, error) {
	raw, err := c.request(ctx, http.MethodGet, "/proxies", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Proxies map[string]Group `json:"proxies"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.Proxies == nil {
		return nil, diagnostic.New("diagnostic.api.proxyResponse", "invalid Mihomo proxy response")
	}
	groups := []Group{}
	for name, g := range result.Proxies {
		if len(g.All) > 0 {
			g.Name = name
			groups = append(groups, g)
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups, nil
}

func (c Client) Select(ctx context.Context, group, node string) error {
	groups, err := c.Groups(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, g := range groups {
		if g.Name != group {
			continue
		}
		if g.Type != "Selector" {
			return diagnostic.New("diagnostic.api.automaticGroup", "this group is selected automatically by Mihomo")
		}
		for _, n := range g.All {
			if n == node {
				found = true
				break
			}
		}
	}
	if !found {
		return diagnostic.New("diagnostic.api.nodeUnavailable", "the group or node is no longer available; refresh the nodes page")
	}
	payload, _ := json.Marshal(map[string]string{"name": node})
	_, err = c.request(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), payload)
	if err != nil {
		return err
	}
	groups, err = c.Groups(ctx)
	if err != nil {
		return diagnostic.New("diagnostic.api.selectConfirm", "node selection was accepted but could not be confirmed: %w", err)
	}
	for _, g := range groups {
		if g.Name == group && g.Now == node {
			return nil
		}
	}
	return diagnostic.New("diagnostic.api.selectUnapplied", "Mihomo did not apply the requested node selection; refresh the nodes page")
}

type Configuration struct {
	Mode string
	TUN  bool
}

// Configuration returns observed engine settings, never an optimistic copy of
// the most recent PATCH request.
func (c Client) Configuration(ctx context.Context) (Configuration, error) {
	raw, err := c.request(ctx, http.MethodGet, "/configs", nil)
	if err != nil {
		return Configuration{}, err
	}
	var cfg struct {
		Mode string `json:"mode"`
		TUN  *struct {
			Enable *bool `json:"enable"`
		} `json:"tun"`
	}
	if json.Unmarshal(raw, &cfg) != nil || !ValidMode(cfg.Mode) || cfg.TUN == nil || cfg.TUN.Enable == nil {
		return Configuration{}, diagnostic.New("diagnostic.api.configResponse", "invalid Mihomo runtime configuration response")
	}
	return Configuration{Mode: cfg.Mode, TUN: *cfg.TUN.Enable}, nil
}

func (c Client) SetMode(ctx context.Context, mode string) error {
	if !ValidMode(mode) {
		return diagnostic.New("diagnostic.api.mode", "Mihomo mode must be rule, global or direct")
	}
	payload, _ := json.Marshal(map[string]string{"mode": mode})
	if _, err := c.request(ctx, http.MethodPatch, "/configs", payload); err != nil {
		return err
	}
	cfg, err := c.Configuration(ctx)
	if err != nil {
		return diagnostic.New("diagnostic.api.modeConfirm", "mode update was accepted but could not be confirmed: %w", err)
	}
	if cfg.Mode != mode {
		return diagnostic.New("diagnostic.api.modeUnapplied", "Mihomo did not apply the requested mode")
	}
	return nil
}
