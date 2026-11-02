package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestBusyLogsEndExecutesStatusReadAndAllowsLaterObservation(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Service: service.Status{State: "inactive"}}}
	m := ready(b)
	m.width, m.height = 120, 32
	m.newJob("action")
	m.page, m.focus, m.logs.paused = "logs", "logs.list", true
	m.message = "Updating subscription"
	m.layout()
	defer func() {
		m.stopRuntime()
		m.stopLogs()
		m.stopCoreStatus()
		if m.cancel != nil {
			m.cancel()
		}
	}()

	var cmd tea.Cmd
	m, cmd = update(m, special(tea.KeyEnd))
	if m.statusCancel == nil || !m.busy {
		t.Fatal("End did not request an independent status read during the operation")
	}
	var status *coreStatusMsg
	var execute func(tea.Cmd)
	execute = func(next tea.Cmd) {
		if next == nil {
			return
		}
		switch msg := next().(type) {
		case tea.BatchMsg:
			for _, child := range msg {
				execute(child)
			}
		case coreStatusMsg:
			status = &msg
		}
	}
	execute(cmd)
	if status == nil {
		t.Fatal("busy End discarded the command that completes the pending status read")
	}
	m, _ = update(m, *status)
	if m.statusCancel != nil || !m.busy {
		t.Fatal("status completion did not release its own pending state independently of the operation")
	}

	m, _ = update(m, resultMsg{id: m.id, snapshot: b.snapshot, hasSnapshot: true})
	if m.busy {
		t.Fatal("operation did not finish")
	}
	b.snapshot.Service = service.Status{Active: true, State: "active"}
	cmd = m.observeCoreState("ready")
	if cmd == nil || m.statusCancel == nil {
		t.Fatal("the prior busy End left status observation permanently suppressed")
	}
	m, _ = update(m, cmd())
	if m.statusCancel != nil || !m.snapshot.Service.Active {
		t.Fatal("later observation did not refresh and release the shared service state")
	}
}
