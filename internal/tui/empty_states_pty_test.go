package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// This opt-in fixture renders explicit, isolated observations in the real Tea
// program. It never runs page commands or contacts a core. n selects the next
// fixture observation; q exits. Ordinary app interactions are tested elsewhere.
func TestEmptyStatesPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_EMPTY_PTY_REPORT")
	if report == "" {
		t.Skip("explicit empty-state PTY report path required")
	}
	backend := &emptyStatesPTYBackend{rulesPTYBackend: newRulesPTYBackend(20)}
	fixture := emptyStatesPTYModel{backend: backend, language: os.Getenv("LAZYCLASH_EMPTY_PTY_LANGUAGE"), report: report, width: 80, height: 24}
	fixture.selectCase()
	if _, err := tea.NewProgram(fixture, tea.WithoutSignalHandler()).Run(); err != nil {
		t.Fatal(err)
	}
	calls := map[string]int64{"rules_reads": backend.queries.Load(), "service_changes": backend.controls.Load(), "modes": backend.modes.Load(), "selections": backend.selections.Load()}
	data, err := json.Marshal(calls)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report+".calls.json", data, 0600); err != nil {
		t.Fatal(err)
	}
}

type emptyStatesPTYBackend struct{ *rulesPTYBackend }

func (b *emptyStatesPTYBackend) RuleProviders(ctx context.Context) (app.RuleProviderSnapshot, error) {
	return app.RuleProviderSnapshot{State: "ready"}, ctx.Err()
}
func (b *emptyStatesPTYBackend) UpdateRuleProvider(ctx context.Context, _ string) error {
	return ctx.Err()
}

type emptyStatesPTYCase struct{ page, state, title string }

func emptyStatesPTYCases() []emptyStatesPTYCase {
	var cases []emptyStatesPTYCase
	for _, page := range []string{"rules", "connections", "logs", "proxies"} {
		for _, state := range []string{"empty", "filtered", "stopped", "failed"} {
			cases = append(cases, emptyStatesPTYCase{page: page, state: state})
		}
	}
	cases = append(cases,
		emptyStatesPTYCase{page: "providers", state: "empty", title: "empty.providers.none"},
		emptyStatesPTYCase{page: "providers", state: "filtered", title: "providers.no_matches"},
		emptyStatesPTYCase{page: "providers", state: "stopped", title: "runtime.status.stopped"},
		emptyStatesPTYCase{page: "providers", state: "failed", title: "empty.providers.read_failed"},
		emptyStatesPTYCase{page: "profiles", state: "empty", title: "empty.profiles.none"},
		emptyStatesPTYCase{page: "profiles", state: "failed", title: "profiles.unavailable"},
		emptyStatesPTYCase{page: "notifications", state: "empty", title: "notification.empty"})
	for _, page := range []string{"rules", "connections", "logs", "proxies"} {
		cases = append(cases, emptyStatesPTYCase{page: page, state: "initial-failed", title: "empty.read_failed"})
	}
	return cases
}

type emptyStatesPTYModel struct {
	model          Model
	backend        *emptyStatesPTYBackend
	language       string
	report         string
	index          int
	width, height  int
	fixtureFailure error
}

func (f emptyStatesPTYModel) Init() tea.Cmd  { return nil }
func (f emptyStatesPTYModel) View() tea.View { return f.model.View() }
func (f emptyStatesPTYModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.width, f.height = msg.Width, msg.Height
		f.selectCase()
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return f, tea.Quit
		case "n":
			f.index = (f.index + 1) % len(emptyStatesPTYCases())
			f.selectCase()
		}
	}
	if f.fixtureFailure != nil {
		return f, tea.Quit
	}
	return f, nil
}

func (f *emptyStatesPTYModel) selectCase() {
	c := emptyStatesPTYCases()[f.index]
	m := New(context.Background(), f.backend, f.language)
	m.snapshot, m.snapshotReady, m.busy, m.job = f.backend.snapshot, true, false, ""
	m.message, m.failure, m.notification.items = "", "", nil
	m.logoIntro.used, m.width, m.height, m.page, m.focus = true, f.width, f.height, c.page, "content"
	m.wizard = newWizard()
	m.runtime.received, m.runtime.snapshot = true, liveRuntime()
	m.rules.active, m.rules.received = true, true
	m.rules.snapshot = app.RuleSnapshot{State: "ready"}
	m.logs.active = true
	if c.state == "filtered" {
		m.rules.snapshot.Rules = []mihomo.Rule{{Index: 0, Type: "Domain", Payload: "fixture.example.invalid", Proxy: "DIRECT", Size: -1}}
		m.rules.search.SetValue("missing")
		m.runtime.snapshot.Connections.Connections = []mihomo.Connection{fixtureConnection("fixture", "fixture.example.invalid", 1)}
		m.connections.search.SetValue("missing")
		m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "fixture retained event"}})
		m.logs.search.SetValue("missing")
		m.snapshot.Groups = []mihomo.Group{{Name: "Fixture group", Type: "Selector", Now: "DIRECT", All: []string{"DIRECT"}}}
	}
	if c.state == "stopped" {
		m.snapshot.Service.State, m.snapshot.Service.Active = "inactive", false
		m.runtime.snapshot.State, m.runtime.snapshot.Connections = "stopped", nil
		m.rules.snapshot.State = "stopped"
	}
	if c.state == "failed" {
		m.rules.snapshot.State, m.rules.err = "unavailable", errors.New("Fixture rule source unavailable")
		m.runtime.snapshot.Connections, m.runtime.snapshot.ConnectionsError = nil, "Fixture connection source unavailable"
		m.logs.err, m.logs.reconnecting = errors.New("Fixture log source unavailable"), true
		m.snapshot.APIError = "Fixture proxy source unavailable"
	}
	if c.state == "initial-failed" {
		m.snapshotReady, m.busy, m.failure = false, false, "Fixture initial snapshot unavailable"
		m.runtime.received, m.rules.received, m.logs.active = false, false, false
	}
	if c.page == "providers" {
		m.page = "rules"
		m.providers.open, m.providers.active, m.providers.received = true, true, true
		m.providers.snapshot.State = "ready"
		if c.state == "filtered" {
			m.providers.snapshot.Providers = []providerRow{{Name: "Fixture provider", Type: "Rule", VehicleType: "HTTP"}}
			m.providers.search.SetValue("missing")
		} else if c.state == "stopped" {
			m.providers.snapshot.State = "stopped"
		} else if c.state == "failed" {
			m.providers.err = errors.New("Fixture provider source unavailable")
		}
	}
	if c.page == "profiles" && c.state == "failed" {
		m.snapshotReady = false
	}
	if c.page == "notifications" {
		m.page = "rules"
		m.notification.panel.show(0)
	}
	if c.page != "providers" && c.page != "notifications" {
		m.enterContent()
	}
	if c.page == "proxies" && c.state == "filtered" {
		m.focusProxySearch()
		m.proxies.search.SetValue("missing")
		m.proxySearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	m.layout()
	if c.title == "" {
		var state emptyStateOptions
		switch c.page {
		case "rules":
			state = m.rulesEmptyState()
		case "connections":
			state = m.connectionsEmptyState()
		case "logs":
			state = m.logsEmptyState()
		case "proxies":
			switch c.state {
			case "empty":
				state.Title = m.tr("proxies.no_groups")
			case "filtered":
				state.Title = m.tr("proxies.search_no_match")
			case "stopped":
				state.Title = m.tr("runtime.status.stopped")
			case "failed":
				state.Title = m.tr("empty.proxies.failed")
			}
		}
		c.title = state.Title
	} else {
		c.title = m.tr(c.title)
	}
	metadata := map[string]any{"index": f.index, "total": len(emptyStatesPTYCases()), "page": c.page, "state": c.state, "title": c.title, "width": f.width, "height": f.height, "targets": m.targets}
	data, err := json.Marshal(metadata)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(f.report), 0700)
	}
	if err == nil {
		err = os.WriteFile(f.report, data, 0600)
	}
	f.model, f.fixtureFailure = m, err
}
