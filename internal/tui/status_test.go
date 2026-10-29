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

func TestRulesObservedStopUpdatesGlobalHeader(t *testing.T) {
	m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true, State: "active"}}})
	m.width, m.height, m.page = 120, 32, "rules"
	m.rules.active, m.rules.pending, m.rules.generation = true, true, 4
	m, _ = update(m, rulesResultMsg{generation: 4, snapshot: app.RuleSnapshot{State: "stopped"}})
	if m.snapshot.Service.Active || !strings.Contains(ansi.Strip(strings.Split(m.screen, "\n")[0]), "Mihomo · Stopped") {
		t.Fatal("Rules kept a stale Running header after observing a stopped core")
	}
}

func TestLogFailureRefreshesStatusWithoutBlockingOrRestartingCapture(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true}}
	m := ready(b)
	m.page = "logs"
	m.snapshot.Service = service.Status{Active: true, State: "active"}
	m.logs.active, m.logs.generation = true, 8
	m, cmd := update(m, logsResultMsg{generation: 8, done: true, err: errors.New("Core stopped; reconnect logs")})
	if cmd == nil || m.busy || !m.logs.ended || m.logs.active {
		t.Fatal("log failure blocked the UI or restarted a failed stream")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			if child != nil {
				m, _ = update(m, child())
			}
		}
	} else {
		m, _ = update(m, msg)
	}
	if m.snapshot.Service.Active || m.logs.active {
		t.Fatal("background status read kept stale service state or reconnected capture")
	}
	old := m.statusGeneration
	m.newJob("action")
	defer m.cancel()
	m, _ = update(m, coreStatusMsg{generation: old, snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true}}})
	if m.snapshot.Service.Active {
		t.Fatal("late status read overwrote a newer foreground operation")
	}
}

func TestNewLiveLogObservationRepairsStoppedHeader(t *testing.T) {
	m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true}})
	m.page = "logs"
	m.logs.active, m.logs.generation = true, 9
	m, _ = update(m, logsResultMsg{generation: 9, entries: []mihomo.LogEntry{{Level: "info", Message: "Live event from the owned core"}}})
	if !m.snapshot.Service.Active || m.snapshot.Service.State != "active" {
		t.Fatal("fresh live log observation kept a stale Stopped header")
	}
}

func TestQuietReconnectRefreshesHeaderWithoutWaitingForLogEvents(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true}}
	m := ready(b)
	m.page, m.logs.ended = "logs", true
	// A different client starts the service while this view is disconnected.
	b.snapshot.Service = service.Status{Active: true, State: "active"}
	cmd := m.resumeLogs()
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
