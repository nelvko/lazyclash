package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

type coreStatusMsg struct {
	generation uint64
	snapshot   app.Snapshot
	err        error
}

func (m *Model) stopCoreStatus() {
	if m.statusCancel != nil {
		m.statusCancel()
		m.statusCancel = nil
	}
	m.statusGeneration++
}

// A failed log stream does not establish whether the process stopped. Read the
// shared snapshot without turning a background check into a blocking UI job.
func (m *Model) refreshCoreStatus() tea.Cmd {
	m.stopCoreStatus()
	ctx, cancel := context.WithTimeout(m.ctx, 8*time.Second)
	m.statusCancel = cancel
	id, backend := m.statusGeneration, m.backend
	return func() tea.Msg {
		defer cancel()
		s, err := backend.Snapshot(ctx)
		return coreStatusMsg{generation: id, snapshot: s, err: err}
	}
}

func (m *Model) observeCoreState(state string) {
	switch state {
	case "stopped":
		m.snapshot.Service.Active = false
		m.snapshot.Service.State = "inactive"
		m.snapshot.ServiceError = ""
		m.snapshot.Groups = nil
	case "uninstalled":
		m.snapshot.Installed, m.snapshot.ConfigReady = false, false
		m.snapshot.Service.Active = false
		m.snapshot.ServiceError = ""
		m.snapshot.Groups = nil
	case "ready":
		m.snapshot.Installed, m.snapshot.HasRun = true, true
		m.snapshot.Service.Active = true
		m.snapshot.Service.State = "active"
		m.snapshot.ServiceError = ""
	}
}
