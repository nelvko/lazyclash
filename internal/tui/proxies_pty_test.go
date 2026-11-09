package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

// TestProxiesPTYFixture is an explicit in-memory backend for a real controlling
// PTY. It does not read managed storage, access a network or change services.
func TestProxiesPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_PROXIES_PTY_REPORT")
	if report == "" {
		t.Skip("explicit Proxies PTY report path required")
	}
	b := newProxiesPTYBackend()
	b.varyDelays = os.Getenv("LAZYCLASH_PROXIES_PTY_RETEST") == "1"
	language := os.Getenv("LAZYCLASH_PROXIES_PTY_LANGUAGE")
	if language == "" {
		language = "en"
	}
	if err := Run(context.Background(), b, language); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	raw, err := json.Marshal(struct {
		Selections int               `json:"selections"`
		Modes      int               `json:"modes"`
		Tests      int               `json:"tests"`
		Canceled   int               `json:"canceled"`
		MaxTests   int               `json:"max_tests"`
		Selected   map[string]string `json:"selected"`
		Requests   map[string]int    `json:"requests,omitempty"`
	}{b.selections, b.modes, b.tests, b.canceled, b.maxTests, b.selectedLocked(), b.delayRequests})
	b.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

type proxiesPTYBackend struct {
	*fakeBackend
	mu                                              sync.Mutex
	selections, modes, tests, canceled, activeTests int
	maxTests                                        int
	varyDelays                                      bool
	delayRequests                                   map[string]int
}

func newProxiesPTYBackend() *proxiesPTYBackend {
	groups := []mihomo.Group{
		{Name: "PROXY", Type: "Selector", Now: "AUTO", All: []string{"AUTO", "HK 01", "HK 02", "JP 01", "SG 01", "US 01", "DIRECT", "REJECT"}},
		{Name: "AUTO", Type: "URLTest", Now: "HK 02", All: []string{"HK 01", "HK 02", "JP 01", "SG 01"}},
		{Name: "ChatGPT", Type: "Selector", Now: "Japan", All: []string{"PROXY", "Japan", "JP 01", "SG 01", "US 01", "DIRECT"}},
		{Name: "YouTube", Type: "Selector", Now: "PROXY", All: []string{"PROXY", "Hong Kong", "Japan", "DIRECT"}},
		{Name: "Hong Kong", Type: "Fallback", Now: "HK 01", All: []string{"HK 01", "HK 02"}},
		{Name: "Japan", Type: "Selector", Now: "JP 01", All: []string{"JP 01", "JP 02", "DIRECT"}},
		{Name: "BALANCE", Type: "LoadBalance", All: []string{"HK 01", "JP 01", "SG 01"}},
		{Name: "EMPTY", Type: "Selector", All: []string{}},
		{Name: "GLOBAL", Type: "Selector", Now: "PROXY", All: []string{"PROXY", "AUTO", "Hong Kong", "Japan", "DIRECT", "REJECT"}},
	}
	proxies := make(map[string]mihomo.Proxy)
	for _, group := range groups {
		proxies[group.Name] = mihomo.Proxy{Name: group.Name, Type: group.Type, Now: group.Now, All: group.All}
	}
	for name, kind := range map[string]string{"HK 01": "Shadowsocks", "HK 02": "Trojan", "JP 01": "VLESS", "JP 02": "Shadowsocks", "SG 01": "Hysteria2", "US 01": "VMess", "DIRECT": "Direct", "REJECT": "Reject"} {
		proxy := mihomo.Proxy{Name: name, Type: kind, UDP: name != "DIRECT" && name != "REJECT"}
		if delay := map[string]int{"HK 01": 42, "HK 02": 36, "JP 01": 81}[name]; delay != 0 {
			proxy.History = []mihomo.ProxyHistory{{Time: time.Now().Add(-time.Minute), Delay: delay}}
		}
		proxies[name] = proxy
	}
	return &proxiesPTYBackend{fakeBackend: &fakeBackend{snapshot: app.Snapshot{
		Installed: true, ConfigReady: true, HasRun: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
		Applied: "pty-example", AppliedName: "PTY example", Generation: "pty-proxies-generation",
		Service: service.Status{Active: true, State: "active", PID: 4242},
		Mode:    app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
		Groups:  groups, Proxies: proxies,
		Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
	}}}
}

func (b *proxiesPTYBackend) selectedLocked() map[string]string {
	selected := make(map[string]string)
	for _, group := range b.snapshot.Groups {
		if group.Type == "Selector" {
			selected[group.Name] = group.Now
		}
	}
	return selected
}

func (b *proxiesPTYBackend) Snapshot(ctx context.Context) (app.Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := b.snapshot
	result.Groups = append([]mihomo.Group(nil), b.snapshot.Groups...)
	for index := range result.Groups {
		result.Groups[index].All = append([]string{}, result.Groups[index].All...)
	}
	result.Proxies = make(map[string]mihomo.Proxy, len(b.snapshot.Proxies))
	for name, proxy := range b.snapshot.Proxies {
		if proxy.All != nil {
			proxy.All = append([]string{}, proxy.All...)
		}
		proxy.History = append([]mihomo.ProxyHistory(nil), proxy.History...)
		result.Proxies[name] = proxy
	}
	return result, ctx.Err()
}

func (b *proxiesPTYBackend) Select(ctx context.Context, group, node string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(120 * time.Millisecond):
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for index, item := range b.snapshot.Groups {
		if item.Name != group || item.Type != "Selector" {
			continue
		}
		for _, candidate := range item.All {
			if candidate != node {
				continue
			}
			b.snapshot.Groups[index].Now = node
			proxy := b.snapshot.Proxies[group]
			proxy.Now = node
			b.snapshot.Proxies[group] = proxy
			b.selections++
			return nil
		}
	}
	return fmt.Errorf("fixture candidate is unavailable")
}

func (b *proxiesPTYBackend) SetMode(ctx context.Context, mode string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.snapshot.Mode.Desired, b.snapshot.Mode.Actual = mode, mode
	b.modes++
	return nil
}

func (b *proxiesPTYBackend) Runtime(ctx context.Context) (app.RuntimeSnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return app.RuntimeSnapshot{State: "ready", Service: b.snapshot.Service, RunningVersion: b.snapshot.RunningVersion}, ctx.Err()
}

func (b *proxiesPTYBackend) TestProxyDelay(ctx context.Context, group, node string) (mihomo.ProxyDelay, error) {
	b.mu.Lock()
	b.tests++
	attempt := 1
	if b.varyDelays {
		if b.delayRequests == nil {
			b.delayRequests = make(map[string]int)
		}
		key := group + "/" + node
		b.delayRequests[key]++
		attempt = b.delayRequests[key]
	}
	b.activeTests++
	if b.activeTests > b.maxTests {
		b.maxTests = b.activeTests
	}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.activeTests--
		b.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		b.mu.Lock()
		b.canceled++
		b.mu.Unlock()
		// Deliberately return a late successful value to verify that canceled
		// generations cannot publish their results into the current view.
		time.Sleep(80 * time.Millisecond)
		return mihomo.ProxyDelay{State: "success", Delay: 777, Time: time.Now(), URL: "https://www.gstatic.com/generate_204"}, nil
	case <-time.After(900 * time.Millisecond):
	}
	delay := map[string]int{"HK 01": 45, "HK 02": 38, "JP 01": 79, "JP 02": 92, "US 01": 211}[node]
	state := "success"
	if node == "SG 01" {
		state = "timeout"
	} else if delay == 0 {
		state = "failed"
	}
	if state == "success" {
		delay += attempt - 1
	}
	return mihomo.ProxyDelay{State: state, Delay: delay, Time: time.Now(), URL: "https://www.gstatic.com/generate_204"}, nil
}

func (b *proxiesPTYBackend) StreamRuntime(ctx context.Context, emit func(app.RuntimeEvent)) error {
	return streamRuntimeFixture(ctx, b, emit)
}
