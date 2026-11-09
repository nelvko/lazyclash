package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

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

func TestLogFailureDoesNotInferStoppedServiceOrStartAnotherStatusWatcher(t *testing.T) {
	m := sessionLogsModel(t, &fakeBackend{})
	m.syncRuntime()
	primeLogRuntime(m)
	m.runtime.snapshot.Traffic = &mihomo.Traffic{Up: 12, Down: 34}
	generation := m.runtime.generation
	transition := m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeLogs, Instance: 1, ObservedAt: time.Now(), Err: errors.New("log stream interrupted")})
	if transition || m.statusCancel != nil || m.busy || !m.logs.reconnecting || !m.logs.active || !m.snapshot.Service.Active || m.runtime.snapshot.Traffic.Down != 34 {
		t.Fatal("a source failure invented a stopped process or duplicated the central status watcher")
	}
	if m.syncRuntime() != nil || m.runtime.generation != generation {
		t.Fatal("log failure replaced the shared session")
	}
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 2, ObservedAt: time.Now(), Snapshot: app.RuntimeSnapshot{State: "stopped", Service: service.Status{State: "inactive"}}})
	if m.snapshot.Service.Active || m.runtime.snapshot.State != "stopped" || !m.logs.active {
		t.Fatal("central status could not independently confirm a stopped process")
	}
}

func TestBufferedLogEventsCannotInventOrReplaceSystemdState(t *testing.T) {
	m := sessionLogsModel(t, &fakeBackend{})
	m.syncRuntime()
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 1, ObservedAt: time.Now(), Snapshot: app.RuntimeSnapshot{State: "stopped", Service: service.Status{State: "inactive"}}})
	m.acceptRuntimeEvent(logSessionEvent("Buffered event from the owned core"))
	if m.snapshot.Service.Active || m.statusCancel != nil {
		t.Fatal("buffered log event invented a running process or started another ownership query")
	}
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 2, ObservedAt: time.Now(), Snapshot: app.RuntimeSnapshot{State: "ready", Service: service.Status{Active: true, State: "active", PID: 4243}}})
	if !m.snapshot.Service.Active || m.snapshot.Service.State != "active" || m.snapshot.Service.PID != 4243 {
		t.Fatal("confirmed central state kept the old Stopped process")
	}
	before := len(m.logs.entries)
	m.acceptRuntimeEvent(logSessionEvent("Old process buffered event"))
	if len(m.logs.entries) != before || m.snapshot.Service.PID != 4243 {
		t.Fatal("old-instance log event entered the new process or changed systemd state")
	}
}

func TestQuietSharedStatusUpdatesTheHeaderWithoutWaitingForLogEvents(t *testing.T) {
	m := sessionLogsModel(t, &fakeBackend{})
	m.snapshot.Service = service.Status{State: "inactive"}
	m.syncRuntime()
	generation := m.runtime.generation
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 1, ObservedAt: time.Now(), Snapshot: app.RuntimeSnapshot{State: "ready", Service: service.Status{Active: true, State: "active", PID: 4242}}})
	m.layout()
	if !m.snapshot.Service.Active || !m.logs.active || m.runtime.generation != generation || len(m.logs.entries) != 0 {
		t.Fatal("quiet central status needed log data, replaced the session, or invented a record")
	}
}

func TestLateFullStatusReadCannotOverwriteForegroundOperation(t *testing.T) {
	m := sessionLogsModel(t, &fakeBackend{})
	m.snapshot.Service = service.Status{State: "inactive"}
	m.refreshCoreStatus()
	old := m.statusGeneration
	m.newJob("action")
	defer m.cancel()
	updated, _ := update(*m, coreStatusMsg{generation: old, snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true}}})
	*m = updated
	if m.snapshot.Service.Active {
		t.Fatal("late status read overwrote a newer foreground operation")
	}
}
