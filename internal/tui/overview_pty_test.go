package tui

import (
	"context"
	"encoding/json"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

// TestOverviewPTYFixture runs the real TUI with isolated in-memory observations.
// It does not read managed data, contact a core or change system services.
func TestOverviewPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_OVERVIEW_PTY_REPORT")
	if report == "" {
		t.Skip("explicit Overview PTY report path required")
	}
	scenario := os.Getenv("LAZYCLASH_OVERVIEW_PTY_SCENARIO")
	if scenario == "" {
		scenario = "running"
	}
	b := newOverviewPTYBackend(scenario)
	language := os.Getenv("LAZYCLASH_OVERVIEW_PTY_LANGUAGE")
	if language == "" {
		language = "en"
	}
	if err := Run(context.Background(), b, language); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]int64{
		"service_changes": b.controls.Load(), "modes": b.modes.Load(),
		"selections": b.selections.Load(), "runtime_queries": b.runtimeQueries.Load(),
		"tun_changes": b.tunChanges.Load(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

type overviewPTYBackend struct {
	*fakeBackend
	scenario                                    string
	controls, modes, selections, runtimeQueries atomic.Int64
	tunChanges                                  atomic.Int64
}

func newOverviewPTYBackend(scenario string) *overviewPTYBackend {
	groups := []mihomo.Group{{Name: "PROXY", Type: "Selector", Now: "香港 01 · 日常线路", All: []string{"香港 01 · 日常线路", "DIRECT"}}}
	s := app.Snapshot{
		Installed: true, ConfigReady: true, HasRun: true, GuideDismissed: true,
		Version: "v1.19.33", RunningVersion: "v1.19.33",
		Applied: "overview-preview", AppliedName: "日常订阅 · 工作与生活.yaml", Generation: "overview-pty-generation",
		Profiles: []string{"default", "overview-preview", "travel"},
		ProfileDetails: []app.ProfileInfo{{ID: "overview-preview", Name: "日常订阅 · 工作与生活.yaml",
			Core: "mihomo", Kind: "remote", Origin: "subscription.example", Refreshable: true, Validated: true,
			UpdatedAt: time.Date(2026, 11, 8, 4, 30, 0, 0, time.UTC)}},
		MixedPort: 17897, Controller: "127.0.0.1:19097",
		Groups: groups, Proxies: map[string]mihomo.Proxy{
			"PROXY":        {Name: "PROXY", Type: "Selector", Now: groups[0].Now, All: groups[0].All},
			"香港 01 · 日常线路": {Name: "香港 01 · 日常线路", Type: "Trojan"},
			"DIRECT":       {Name: "DIRECT", Type: "Direct"},
		},
		Service:      service.Status{Active: true, State: "active", PID: 4242, EnableState: "disabled"},
		Mode:         app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
		Desired:      app.DesiredState{Known: true, Running: true},
		Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
	}
	switch scenario {
	case "stopped":
		s.Service.Active, s.Service.State, s.Service.PID = false, "inactive", 0
		s.RunningVersion = ""
		s.Mode.Actual, s.Mode.Running = "", false
		s.Desired.Running = false
	case "unavailable":
		s.ServiceError = "Fixture service observation unavailable"
		s.Service.State = "unknown"
		s.RunningVersion, s.Mode.Actual = "", ""
	case "drift":
		s.Version = "v1.20.0"
		s.Mode.Actual = "global"
		s.Drift = "Fixture running service differs from saved settings"
	case "pending":
		s.ProfilePending, s.ProfileDetails[0].Pending = true, true
		s.Capabilities.Scope, s.Capabilities.TUN.Supported = "system", true
		s.TUN = true
		actual := false
		s.TUNActual = &actual
	}
	return &overviewPTYBackend{fakeBackend: &fakeBackend{snapshot: s}, scenario: scenario}
}

func (b *overviewPTYBackend) Runtime(ctx context.Context) (app.RuntimeSnapshot, error) {
	b.runtimeQueries.Add(1)
	result := app.RuntimeSnapshot{State: "stopped", Service: b.snapshot.Service, ObservedAt: time.Now()}
	if b.scenario == "unavailable" {
		result.State, result.Error = "unavailable", "Fixture service observation unavailable"
	} else if b.snapshot.Service.Active {
		result = liveRuntime()
		result.Service, result.RunningVersion = b.snapshot.Service, b.snapshot.RunningVersion
		result.Traffic = &mihomo.Traffic{Up: 12 << 10, Down: 2304 << 10}
		result.Connections.Connections = []mihomo.Connection{{ID: "overview-1"}, {ID: "overview-2"}, {ID: "overview-3"}}
	}
	return result, ctx.Err()
}

func (b *overviewPTYBackend) Control(ctx context.Context, _ string) error {
	b.controls.Add(1)
	return ctx.Err()
}

func (b *overviewPTYBackend) SetMode(ctx context.Context, _ string) error {
	b.modes.Add(1)
	return ctx.Err()
}

func (b *overviewPTYBackend) Select(ctx context.Context, _, _ string) error {
	b.selections.Add(1)
	return ctx.Err()
}

func (b *overviewPTYBackend) SetTUN(ctx context.Context, _ bool) error {
	b.tunChanges.Add(1)
	return ctx.Err()
}

func (b *overviewPTYBackend) StreamRuntime(ctx context.Context, emit func(app.RuntimeEvent)) error {
	return streamRuntimeFixture(ctx, b, emit)
}
