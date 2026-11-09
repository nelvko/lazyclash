package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// TestProxyProvidersPTYFixture runs only when explicitly requested. All source,
// core and service state belongs to this in-memory backend.
func TestProxyProvidersPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_PROXY_PROVIDERS_PTY_REPORT")
	if report == "" {
		t.Skip("explicit proxy provider PTY report path required")
	}
	b := newProxyProvidersPTYBackend(report)
	language := os.Getenv("LAZYCLASH_PROXY_PROVIDERS_PTY_LANGUAGE")
	if language == "" {
		language = "en"
	}
	if err := Run(context.Background(), b, language); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writeProviderReport(); err != nil {
		t.Fatal(err)
	}
}

type proxyProvidersPTYBackend struct {
	*fakeBackend
	mu        sync.Mutex
	report    string
	providers []mihomo.ProxyProvider
	calls     proxyProvidersPTYReport
	fileFails bool
	active    int
}

type proxyProvidersPTYReport struct {
	Reads     int      `json:"reads"`
	Snapshots int      `json:"snapshots"`
	Attempts  []string `json:"attempts"`
	Updates   []string `json:"updates"`
	Failures  []string `json:"failures"`
	Canceled  int      `json:"canceled"`
	MaxActive int      `json:"max_active"`
	Service   int      `json:"service"`
	Modes     int      `json:"modes"`
}

func newProxyProvidersPTYBackend(report string) *proxyProvidersPTYBackend {
	b := &proxyProvidersPTYBackend{fakeBackend: newProxiesPTYBackend().fakeBackend, report: report, fileFails: true}
	for _, p := range []struct{ name, vehicle string }{
		{"remote_nodes", "HTTP"}, {"local_nodes", "File"}, {"remote_second", "HTTP"},
		{"remote_third", "HTTP"}, {"remote_slow", "HTTP"},
		{"inline_nodes", "Inline"}, {"compatible_nodes", "Compatible"},
	} {
		b.providers = append(b.providers, mihomo.ProxyProvider{Name: p.name, Type: "Proxy", VehicleType: p.vehicle, ProxyCount: 5})
	}
	for index := range 15 {
		b.providers = append(b.providers, mihomo.ProxyProvider{Name: fmt.Sprintf("inline-extra-%02d", index), Type: "Proxy", VehicleType: "Inline", ProxyCount: index + 1})
	}
	return b
}

// Caller holds mu; atomic replacement lets a running PTY inspect counters.
func (b *proxyProvidersPTYBackend) writeProviderReport() error {
	raw, err := json.Marshal(b.calls)
	if err != nil {
		return err
	}
	if err := os.WriteFile(b.report+".tmp", raw, 0600); err != nil {
		return err
	}
	return os.Rename(b.report+".tmp", b.report)
}

func (b *proxyProvidersPTYBackend) Snapshot(ctx context.Context) (app.Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls.Snapshots++
	result := b.snapshot
	result.Groups = slices.Clone(b.snapshot.Groups)
	for i := range result.Groups {
		result.Groups[i].All = slices.Clone(result.Groups[i].All)
	}
	result.Proxies = make(map[string]mihomo.Proxy, len(b.snapshot.Proxies))
	for name, proxy := range b.snapshot.Proxies {
		proxy.All = slices.Clone(proxy.All)
		proxy.History = slices.Clone(proxy.History)
		result.Proxies[name] = proxy
	}
	if err := b.writeProviderReport(); err != nil {
		return result, err
	}
	return result, ctx.Err()
}

func (b *proxyProvidersPTYBackend) ProxyProviders(ctx context.Context) (app.ProxyProviderSnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls.Reads++
	if err := b.writeProviderReport(); err != nil {
		return app.ProxyProviderSnapshot{}, err
	}
	return app.ProxyProviderSnapshot{State: "ready", Providers: slices.Clone(b.providers), ObservedAt: time.Now()}, ctx.Err()
}

func (b *proxyProvidersPTYBackend) UpdateProxyProvider(ctx context.Context, name string) error {
	b.mu.Lock()
	b.calls.Attempts = append(b.calls.Attempts, name)
	b.active++
	b.calls.MaxActive = max(b.calls.MaxActive, b.active)
	_ = b.writeProviderReport()
	b.mu.Unlock()
	delay := 180 * time.Millisecond
	if name == "remote_slow" {
		delay = time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		b.mu.Lock()
		defer b.mu.Unlock()
		b.active--
		b.calls.Canceled++
		_ = b.writeProviderReport()
		return ctx.Err()
	case <-timer.C:
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.active--
	if err := ctx.Err(); err != nil {
		return err
	}
	for i := range b.providers {
		p := &b.providers[i]
		if p.Name != name {
			continue
		}
		if p.VehicleType != "HTTP" && p.VehicleType != "File" {
			return errors.New("fixture read-only provider")
		}
		if p.VehicleType == "File" && b.fileFails {
			b.fileFails = false
			b.calls.Failures = append(b.calls.Failures, name)
			_ = b.writeProviderReport()
			return errors.New("fixture local source read failed")
		}
		b.calls.Updates = append(b.calls.Updates, name)
		p.ProxyCount++
		p.UpdatedAt = time.Now()
		// The core's provider refresh introduces a new member to the active group.
		if name == "remote_nodes" {
			group := &b.snapshot.Groups[0]
			if !slices.Contains(group.All, "Updated node") {
				group.All = append(group.All, "Updated node")
			}
			b.snapshot.Proxies["Updated node"] = mihomo.Proxy{Name: "Updated node", Type: "Shadowsocks"}
		}
		return b.writeProviderReport()
	}
	return fmt.Errorf("unknown fixture provider %q", name)
}

func (b *proxyProvidersPTYBackend) Control(ctx context.Context, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls.Service++
	return b.writeProviderReport()
}

func (b *proxyProvidersPTYBackend) SetMode(ctx context.Context, mode string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls.Modes++
	b.snapshot.Mode.Desired, b.snapshot.Mode.Actual = mode, mode
	return b.writeProviderReport()
}

func (b *proxyProvidersPTYBackend) Runtime(ctx context.Context) (app.RuntimeSnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return app.RuntimeSnapshot{State: "ready", Service: b.snapshot.Service, RunningVersion: b.snapshot.RunningVersion}, ctx.Err()
}
