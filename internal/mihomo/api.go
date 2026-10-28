package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
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

func (c Client) request(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("invalid local controller URL")
	}
	if err := loopbackAddress(base.Host); err != nil {
		return nil, err
	}
	cl := &http.Client{Timeout: 5 * time.Second}
	if c.HTTP != nil {
		*cl = *c.HTTP
	}
	if cl.Timeout == 0 {
		cl.Timeout = 5 * time.Second
	}
	transport := cl.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if tr, ok := transport.(*http.Transport); ok {
		tr = tr.Clone()
		tr.Proxy = nil
		transport = tr
		defer tr.CloseIdleConnections()
	}
	cl.Transport = transport
	// Never follow redirects: the bearer token belongs to this local listener.
	cl.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("cannot construct local controller request")
	}
	req.Header.Set("Authorization", "Bearer "+c.Secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := cl.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("cannot reach local Mihomo API; check the service and controller port")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("Mihomo API returned HTTP %d; verify the managed listener and credentials", resp.StatusCode)
		}
		return nil, fmt.Errorf("Mihomo API returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxConfigSize+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("cannot read Mihomo API response")
	}
	if len(data) > MaxConfigSize {
		return nil, errors.New("Mihomo API response exceeds 8 MiB")
	}
	return data, nil
}

func (c Client) Ping(ctx context.Context) error {
	raw, err := c.request(ctx, http.MethodGet, "/version", nil)
	if err != nil {
		return err
	}
	var v struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Version == "" {
		return errors.New("local listener did not return a Mihomo version")
	}
	return nil
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
		return nil, errors.New("invalid Mihomo proxy response")
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
			return errors.New("this group is selected automatically by Mihomo")
		}
		for _, n := range g.All {
			if n == node {
				found = true
				break
			}
		}
	}
	if !found {
		return errors.New("the group or node is no longer available; refresh the nodes page")
	}
	payload, _ := json.Marshal(map[string]string{"name": node})
	_, err = c.request(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), payload)
	return err
}
