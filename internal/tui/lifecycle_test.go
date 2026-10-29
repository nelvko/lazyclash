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
	for _, id := range []string{"autostart-enable", "autostart-disable", "mode.rule", "mode.global", "mode.direct"} {
		t.Run(id, func(t *testing.T) {
			calls := 0
			expected := errors.New("persistent update failed")
			b := &fakeBackend{snapshot: app.Snapshot{Installed: true}}
			b.autostart = func(_ context.Context, enabled bool) error {
				calls++
				if enabled != (id == "autostart-enable") {
					t.Fatal("wrong autostart preference")
				}
				return expected
			}
			b.mode = func(_ context.Context, mode string) error {
				calls++
				if mode != strings.TrimPrefix(id, "mode.") {
					t.Fatal("wrong routing mode")
				}
				return expected
			}
			m := ready(b)
			cmd := m.activate(id)
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

func TestCoreSummaryPreservesSystemdTransitionsAndFailure(t *testing.T) {
	for state, want := range map[string]string{"activating": "Starting", "deactivating": "Stopping", "reloading": "Reloading", "failed": "Failed", "unknown": "Unavailable"} {
		got, _ := coreSummary(app.Snapshot{Installed: true, Service: service.Status{State: state}})
		if got != want {
			t.Fatalf("%s was presented as %s", state, got)
		}
	}
	s := app.Snapshot{Installed: true, Desired: app.DesiredState{Known: true, Running: true}, Drift: "desired running; service failed", Service: service.Status{State: "failed"}}
	card := ansi.Strip(strings.Join(overviewCards(dashboardOptions{Snapshot: s})[3], "\n"))
	if !strings.Contains(card, "Desired state") || !strings.Contains(card, s.Drift) || !strings.Contains(card, "Unsupported in user service") {
		t.Fatal("environment card hid lifecycle drift or TUN capability")
	}
}
