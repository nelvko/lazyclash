package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

type runtimeState struct {
	snapshot                  app.RuntimeSnapshot
	generation                uint64
	active, pending, received bool
	cancel                    context.CancelFunc
}
type runtimeResultMsg struct {
	generation uint64
	snapshot   app.RuntimeSnapshot
	err        error
}
type runtimeTickMsg struct{ generation uint64 }

func (m *Model) stopRuntime() {
	if m.runtime.cancel != nil {
		m.runtime.cancel()
		m.runtime.cancel = nil
	}
	if m.runtime.active {
		m.runtime.generation++
	}
	m.runtime.active, m.runtime.pending = false, false
	m.runtime.received = false
}
func (m *Model) runtimeVisible() bool {
	return m.snapshot.Installed && (m.page == "overview" || m.page == "connections") && !m.busy && !m.quitting && !m.wizard.open && !m.wizard.networkOpen && m.form == "" && m.ctx.Err() == nil
}
func (m *Model) syncRuntime() tea.Cmd {
	if !m.runtimeVisible() {
		m.stopRuntime()
		return nil
	}
	if m.runtime.active {
		return nil
	}
	m.runtime.generation++
	m.runtime.active = true
	return m.runtimeCmd()
}
func (m *Model) runtimeCmd() tea.Cmd {
	if m.runtime.pending {
		return nil
	}
	m.runtime.generation++
	ctx, cancel := context.WithTimeout(m.ctx, 8*time.Second)
	m.runtime.cancel, m.runtime.pending = cancel, true
	id, backend := m.runtime.generation, m.backend
	return func() tea.Msg {
		s, err := backend.Runtime(ctx)
		return runtimeResultMsg{generation: id, snapshot: s, err: err}
	}
}
func (m *Model) acceptRuntime(v runtimeResultMsg) tea.Cmd {
	if !m.runtime.active || !m.runtime.pending || v.generation != m.runtime.generation {
		return nil
	}
	if m.runtime.cancel != nil {
		m.runtime.cancel()
		m.runtime.cancel = nil
	}
	m.runtime.pending = false
	m.runtime.received = true
	previousError := m.runtime.snapshot.Error
	if v.err != nil {
		m.runtime.snapshot = app.RuntimeSnapshot{State: "unavailable", Error: clean(v.err.Error())}
		m.snapshot.ServiceError = "Service status is unavailable; retry runtime monitoring"
		m.snapshot.APIError = m.runtime.snapshot.Error
	} else {
		m.runtime.snapshot = v.snapshot
		if v.snapshot.State == "unavailable" || v.snapshot.State == "unsupported" {
			m.snapshot.APIError = v.snapshot.Error
		} else if v.snapshot.State == "ready" && (m.snapshot.APIError == previousError || m.snapshot.APIError == "Start the service to view proxy groups") {
			m.snapshot.APIError = ""
		}
		if v.snapshot.Service.State != "" {
			m.snapshot.Service = v.snapshot.Service
			m.snapshot.ServiceError = ""
		} else if v.snapshot.State == "unavailable" || v.snapshot.State == "unsupported" {
			m.snapshot.ServiceError = v.snapshot.Error
			if m.snapshot.ServiceError == "" {
				m.snapshot.ServiceError = "Service status is unavailable"
			}
		}
		if v.snapshot.State == "uninstalled" {
			m.snapshot.Installed, m.snapshot.ConfigReady = false, false
			m.snapshot.Service.Active = false
		}
		if v.snapshot.State == "stopped" {
			m.snapshot.Groups = nil
			m.snapshot.APIError = "Start the service to view proxy groups"
		}
		m.snapshot.RunningVersion = v.snapshot.RunningVersion
		if v.snapshot.RunningVersion != "" {
			if v.snapshot.State == "ready" {
				m.snapshot.HasRun = true
			}
		}
		m.reconcileConnections()
	}
	id := m.runtime.generation
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return runtimeTickMsg{generation: id} })
}
func (m *Model) retryRuntime() tea.Cmd {
	m.stopRuntime()
	m.runtime.received = false
	return m.syncRuntime()
}
func runtimeStatus(s app.RuntimeSnapshot, received, pending bool, installed bool) string {
	if !installed {
		return "Core not installed"
	}
	if !received {
		if pending {
			return "Connecting…"
		}
		return "Waiting for runtime data"
	}
	switch s.State {
	case "stopped":
		return "Core stopped"
	case "unsupported":
		return "Service management unavailable"
	case "uninstalled":
		return "Core not installed"
	case "unavailable":
		return "API unavailable"
	case "ready":
		if s.Traffic == nil || s.Connections == nil || s.Memory == nil {
			return "Partially available"
		}
		return "Live"
	default:
		return "Connecting…"
	}
}
