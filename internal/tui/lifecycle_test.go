package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestQuittingDoesNotIssueServiceStop(t *testing.T) {
	for _, event := range []tea.Msg{key("q"), ctrlC(), parentCanceledMsg{}} {
		calls := 0
		b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true, State: "active"}}, control: func(context.Context, string) error { calls++; return nil }}
		m := ready(b)
		m, cmd := update(m, event)
		expectQuit(t, cmd)
		if calls != 0 || !m.snapshot.Service.Active {
			t.Fatal("quitting the interface changed the managed core")
		}
	}
}

func TestQuitDuringStartCancelsOnlyTheInteractiveWait(t *testing.T) {
	started := make(chan struct{})
	done := make(chan tea.Msg, 1)
	actions := make(chan string, 2)
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true, State: "active"}}, control: func(ctx context.Context, action string) error {
		actions <- action
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	m := ready(b)
	commands := commandBatch(t, m.serviceAction("start"))
	go func() { done <- commands[0]() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("start did not reach the shared use case")
	}
	m, _ = update(m, key("q"))
	select {
	case result := <-done:
		var cmd tea.Cmd
		m, cmd = update(m, result)
		expectQuit(t, cmd)
	case <-time.After(time.Second):
		t.Fatal("interactive wait did not observe cancellation")
	}
	if len(actions) != 1 || <-actions != "start" || !m.snapshot.Service.Active {
		t.Fatal("quitting issued an extra lifecycle action or hid the accepted start")
	}
}

func TestPersistentSettingsUseAsyncApplicationActionsAndShowErrors(t *testing.T) {
	for _, tc := range []struct {
		id               string
		autostartEnabled bool
	}{
		{autostartToggleTarget, false},
		{autostartToggleTarget, true},
		{id: "mode.rule"},
		{id: "mode.global"},
		{id: "mode.direct"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			calls := 0
			expected := errors.New("persistent update failed")
			b := &fakeBackend{snapshot: app.Snapshot{Installed: true}}
			b.snapshot.Service.Enabled = tc.autostartEnabled
			b.snapshot.Service.EnableState = "disabled"
			if tc.autostartEnabled {
				b.snapshot.Service.EnableState = "enabled"
			}
			b.autostart = func(_ context.Context, enabled bool) error {
				calls++
				if enabled == tc.autostartEnabled {
					t.Fatal("wrong autostart preference")
				}
				return expected
			}
			b.mode = func(_ context.Context, mode string) error {
				calls++
				if mode != strings.TrimPrefix(tc.id, "mode.") {
					t.Fatal("wrong routing mode")
				}
				return expected
			}
			m := ready(b)
			if tc.id == autostartToggleTarget {
				m.page = "settings"
			}
			cmd := m.activate(tc.id)
			if calls != 0 || !m.busy || cmd == nil {
				t.Fatal("persistent operation blocked the event loop")
			}
			m, _ = update(m, commandBatch(t, cmd)[0]())
			if calls != 1 || m.busy || !strings.Contains(m.failure, expected.Error()) {
				t.Fatal("application failure became a successful UI operation")
			}
		})
	}
}

func TestAutostartToggleRetainsFocusAfterRefresh(t *testing.T) {
	b := &fakeBackend{snapshot: settingsTestSnapshot()}
	b.autostart = func(_ context.Context, enabled bool) error {
		b.snapshot.Service.Enabled = enabled
		return nil
	}
	m := ready(b)
	directionCleanup(t, &m)
	m.page, m.focus, m.width, m.height = "settings", "settings.language", 160, 45
	m.layout()
	m, cmd := click(m, findTarget(t, m, autostartToggleTarget))
	m = finishProfileJob(t, m, cmd)
	if !m.snapshot.Service.Enabled || m.focus != autostartToggleTarget || !strings.Contains(m.contextHelp(), m.tr("dashboard.settings.disableAutostart")) {
		t.Fatalf("enabling autostart should leave its disable action focused, got %q", m.focus)
	}
	m, cmd = update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if m.snapshot.Service.Enabled || m.focus != autostartToggleTarget || !strings.Contains(m.contextHelp(), m.tr("dashboard.settings.enableAutostart")) {
		t.Fatalf("disabling autostart should leave its enable action focused, got %q", m.focus)
	}
}

func TestCoreSummaryPreservesSystemdTransitionsAndFailure(t *testing.T) {
	for state, want := range map[string]string{"activating": "Starting", "deactivating": "Stopping", "reloading": "Reloading", "failed": "Failed", "unknown": "Unavailable"} {
		got, _ := coreSummary(app.Snapshot{Installed: true, Service: service.Status{State: state}})
		if got != want {
			t.Fatalf("%s was presented as %s", state, got)
		}
	}
	s := app.Snapshot{Installed: true, Desired: app.DesiredState{Known: true, Running: true}, Drift: "desired running; service failed", Service: service.Status{State: "failed"}}
	card := ansi.Strip(strings.Join(overviewCoreCard(dashboardOptions{Snapshot: s}, 72).lines, "\n"))
	if strings.Contains(card, "Desired state") || !strings.Contains(card, "! Failed") || !strings.Contains(card, "lazyclash doctor") {
		t.Fatal("failed core did not show concise status and recovery")
	}
	access := ansi.Strip(strings.Join(overviewNetworkCard(dashboardOptions{Snapshot: s}, 72).lines, "\n"))
	if !strings.Contains(access, "Unsupported") {
		t.Fatal("access card hid TUN capability")
	}
}
