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

func (m *Model) observeCoreState(state string) tea.Cmd {
	// API events can arrive after a process exits. Only an application snapshot
	// may replace systemd state; keep at most one such observation in flight.
	if m.statusCancel != nil {
		return nil
	}
	switch state {
	case "stopped":
		m.snapshot.Groups = nil
		m.snapshot.Proxies = nil
		if m.snapshot.Service.Active || m.snapshot.Service.State != "inactive" || m.snapshot.ServiceError != "" {
			return m.refreshCoreStatus()
		}
	case "uninstalled":
		m.snapshot.Groups = nil
		m.snapshot.Proxies = nil
		if m.snapshot.Installed {
			return m.refreshCoreStatus()
		}
	case "ready":
		if !m.snapshot.Service.Active || m.snapshot.ServiceError != "" {
			return m.refreshCoreStatus()
		}
	}
	return nil
}

// A full settings/profile read can finish after a newer live ownership check.
// Merge that newer process observation while keeping the read's profile data.
func (m *Model) adoptSnapshot(snapshot app.Snapshot) {
	latest := m.runtime.observed[app.RuntimeStatus]
	if !snapshot.ServiceObservedAt.IsZero() && latest.After(snapshot.ServiceObservedAt) {
		runtime := m.runtime.snapshot
		if runtime.Service.State != "" {
			snapshot.Service = runtime.Service
			snapshot.ServiceError, snapshot.ServiceDiagnostic = "", nil
		} else if runtime.State == "unavailable" || runtime.State == "unsupported" {
			snapshot.ServiceError, snapshot.ServiceDiagnostic = runtime.Error, runtime.Diagnostic
		}
		snapshot.ServiceObservedAt = latest
		snapshot.RunningVersion = runtime.RunningVersion
		if runtime.State == "ready" || runtime.State == "stopped" {
			snapshot.Installed = true
		}
		if runtime.State == "stopped" || runtime.State == "uninstalled" {
			snapshot.Groups, snapshot.Proxies = nil, nil
		}
		if runtime.State == "uninstalled" {
			snapshot.Installed, snapshot.ConfigReady = false, false
		}
	}
	m.snapshot, m.snapshotReady = snapshot, true
	if !m.runtime.active || !snapshot.ServiceObservedAt.IsZero() {
		m.runtime.history.observeSnapshot(snapshot)
	}
}
