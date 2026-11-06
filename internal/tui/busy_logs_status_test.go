package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestBusyLogsEndResumesDisplayWithoutReplacingTheSession(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Service: service.Status{State: "inactive"}}}
	m := ready(b)
	m.width, m.height, m.page, m.focus = 120, 32, "logs", "logs.list"
	m.syncLogs()
	defer m.stopLogs()
	m.pauseLogs()
	ctx, generation := m.logs.ctx, m.logs.generation
	m.newJob("action")
	defer m.cancel()
	m, _ = update(m, special(tea.KeyEnd))
	if !m.busy || m.logs.paused || !m.logs.active || ctx.Err() != nil || m.logs.generation != generation || m.statusCancel != nil {
		t.Fatal("busy End replaced the session or failed to resume display independently")
	}
	m, _ = update(m, resultMsg{id: m.id, snapshot: b.snapshot, hasSnapshot: true})
	b.snapshot.Service = service.Status{Active: true, State: "active"}
	cmd := m.observeCoreState("ready")
	if cmd == nil || m.statusCancel == nil {
		t.Fatal("resuming display suppressed later service observations")
	}
	m, _ = update(m, cmd())
	if m.statusCancel != nil || !m.snapshot.Service.Active {
		t.Fatal("later observation did not refresh and release its pending state")
	}
}

func TestBusyLogResultsKeepCollectionWithoutRacingTheForegroundSnapshot(t *testing.T) {
	m := logsModel()
	m.syncLogs()
	defer m.stopLogs()
	m.newJob("action")
	defer m.cancel()
	generation := m.logs.generation
	m, _ = update(m, logsResultMsg{generation: generation, done: true, err: errors.New("fixture connection interrupted")})
	if !m.logs.active || !m.logs.reconnecting || m.statusCancel != nil {
		t.Fatal("a background disconnect started a snapshot read during a foreground operation")
	}
	m, _ = update(m, logsResultMsg{generation: generation, entries: []mihomo.LogEntry{{Level: "info", Message: "collecting during the operation"}}})
	if len(m.logs.entries) != 1 || !m.logs.active || m.statusCancel != nil || !m.busy {
		t.Fatal("a background event interrupted capture or raced the foreground snapshot")
	}
}
