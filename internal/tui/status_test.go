package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestRulesObservedStopUpdatesSharedStateAndPageStatus(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true, State: "active"}}}
	m := ready(b)
	b.snapshot.Service = service.Status{State: "inactive"}
	m.width, m.height, m.page = 120, 32, "rules"
	m.rules.active, m.rules.pending, m.rules.generation = true, true, 4
	var cmd tea.Cmd
	m, cmd = update(m, rulesResultMsg{generation: 4, snapshot: app.RuleSnapshot{State: "stopped"}})
	m, _ = update(m, commandBatch(t, cmd)[1]())
	if m.snapshot.Service.Active || !strings.Contains(ansi.Strip(m.screen), "Core stopped") {
		t.Fatal("Rules kept stale running state after observing a stopped core")
	}
}

func TestLogFailureRefreshesStatusWhileSessionWaitsForRetry(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true}}
	m := ready(b)
	m.page = "logs"
	m.snapshot.Service = service.Status{Active: true, State: "active"}
	m.logs.active, m.logs.generation = true, 8
	m, cmd := update(m, logsResultMsg{generation: 8, done: true, err: errors.New("Core stopped; reconnect logs")})
	if cmd == nil || m.busy || !m.logs.reconnecting || !m.logs.active {
		t.Fatal("log failure blocked the UI or restarted a failed stream")
	}
	// The first command waits for the next session update; only execute the
	// independent status read here, without inventing a second stream.
	m, _ = update(m, commandBatch(t, cmd)[1]())
	if m.snapshot.Service.Active || !m.logs.active || !m.logs.reconnecting {
		t.Fatal("background status read kept stale service state or ended the session")
	}
	old := m.statusGeneration
	m.newJob("action")
	defer m.cancel()
	m, _ = update(m, coreStatusMsg{generation: old, snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true}}})
	if m.snapshot.Service.Active {
		t.Fatal("late status read overwrote a newer foreground operation")
	}
}

func TestLogEventsRefreshSystemdStateWithoutInventingRunningState(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Service: service.Status{State: "inactive"}}}
	m := ready(b)
	m.page = "logs"
	m.logs.active, m.logs.generation = true, 9
	var cmd tea.Cmd
	m, cmd = update(m, logsResultMsg{generation: 9, entries: []mihomo.LogEntry{{Level: "info", Message: "Buffered event from the owned core"}}})
	if m.snapshot.Service.Active {
		t.Fatal("buffered log event invented a running systemd process")
	}
	m, _ = update(m, commandBatch(t, cmd)[1]())
	if m.snapshot.Service.Active {
		t.Fatal("systemd observation was overridden by a buffered API event")
	}
	b.snapshot.Service = service.Status{Active: true, State: "active"}
	m, cmd = update(m, logsResultMsg{generation: 9, entries: []mihomo.LogEntry{{Level: "info", Message: "Fresh event"}}})
	m, _ = update(m, commandBatch(t, cmd)[1]())
	if !m.snapshot.Service.Active || m.snapshot.Service.State != "active" {
		t.Fatal("confirmed systemd state kept a stale Stopped header")
	}
}

func TestQuietReconnectRefreshesHeaderWithoutWaitingForLogEvents(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true}}
	m := ready(b)
	m.page = "logs"
	// A different client starts the service while this view is disconnected.
	b.snapshot.Service = service.Status{Active: true, State: "active"}
	cmd := m.reconnectLogs()
	defer m.stopLogs()
	defer m.stopCoreStatus()
	commands := commandBatch(t, cmd)
	if len(commands) != 2 {
		t.Fatal("reconnect did not independently request the current service snapshot")
	}
	// Execute only the snapshot command. The stream has not emitted any event.
	m, _ = update(m, commands[1]())
	if !m.snapshot.Service.Active || !m.logs.active {
		t.Fatal("quiet reconnect left the prior Stopped summary")
	}
}
