package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
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
	if m.runtime.active || m.runtime.pending {
		m.runtime.generation++
	}
	m.runtime.active, m.runtime.pending = false, false
}
func (m *Model) runtimeVisible() bool {
	sidebar := dashboardGeometry(dashboardOptions{Width: m.width, Height: m.height}).sidebar
	return m.snapshot.Installed && (m.page == "overview" || m.page == "connections" || sidebar) && !m.busy && !m.quitting && !m.helpOpen && !m.ports.open && !m.wizard.open && !m.wizard.networkOpen && m.form == "" && m.ctx.Err() == nil
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
	previousState, previousService := m.runtime.snapshot.State, m.snapshot.Service
	if v.err != nil {
		m.runtime.snapshot = app.RuntimeSnapshot{State: "unavailable", Error: v.err.Error(), Diagnostic: v.err}
		m.snapshot.ServiceError = "Service status is unavailable; retry runtime monitoring"
		m.snapshot.APIError = m.runtime.snapshot.Error
		m.snapshot.APIDiagnostic = v.err
	} else {
		m.runtime.snapshot = v.snapshot
		if v.snapshot.State == "unavailable" || v.snapshot.State == "unsupported" {
			m.snapshot.APIError = v.snapshot.Error
			m.snapshot.APIDiagnostic = v.snapshot.Diagnostic
		} else if v.snapshot.State == "ready" && (m.snapshot.APIError == previousError || previousState == "stopped" || !previousService.Active) {
			m.snapshot.APIError = ""
			m.snapshot.APIDiagnostic = nil
		}
		if v.snapshot.Service.State != "" {
			m.snapshot.Service = v.snapshot.Service
			m.snapshot.ServiceError = ""
			m.snapshot.ServiceDiagnostic = nil
		} else if v.snapshot.State == "unavailable" || v.snapshot.State == "unsupported" {
			m.snapshot.ServiceError = v.snapshot.Error
			m.snapshot.ServiceDiagnostic = v.snapshot.Diagnostic
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
			m.snapshot.Proxies = nil
			m.snapshot.APIError = "Start the service to view proxy groups"
			m.snapshot.APIDiagnostic = nil
		}
		m.snapshot.RunningVersion = v.snapshot.RunningVersion
		if v.snapshot.RunningVersion != "" {
			if v.snapshot.State == "ready" {
				m.snapshot.HasRun = true
			}
		}
	}
	m.observeConnections(time.Now())
	id := m.runtime.generation
	tick := tea.Tick(time.Second, func(time.Time) tea.Msg { return runtimeTickMsg{generation: id} })
	if v.err == nil && m.statusCancel == nil && (previousState != v.snapshot.State || previousService.State != m.snapshot.Service.State || previousService.PID != m.snapshot.Service.PID || previousService.Enabled != m.snapshot.Service.Enabled) {
		// A different client or systemd recovery can change persistent settings
		// and active profiles. Refresh them after a runtime transition as well.
		return tea.Batch(tick, m.refreshCoreStatus())
	}
	return tick
}
func (m *Model) retryRuntime() tea.Cmd {
	m.stopRuntime()
	m.runtime.received = false
	return m.syncRuntime()
}
func runtimeStatus(s app.RuntimeSnapshot, received, pending bool, installed bool, locales ...i18n.Localizer) string {
	var locale i18n.Localizer
	if len(locales) > 0 {
		locale = locales[0]
	}
	if !installed {
		return locale.T("runtime.status.not_installed")
	}
	if !received {
		if pending {
			return locale.T("runtime.status.connecting")
		}
		return locale.T("runtime.status.waiting")
	}
	switch s.Service.State {
	case "activating":
		return locale.T("runtime.status.starting")
	case "deactivating":
		return locale.T("runtime.status.stopping")
	case "failed":
		return locale.T("runtime.status.failed")
	}
	switch s.State {
	case "stopped":
		return locale.T("runtime.status.stopped")
	case "unsupported":
		return locale.T("runtime.status.management_unavailable")
	case "uninstalled":
		return locale.T("runtime.status.not_installed")
	case "unavailable":
		return locale.T("runtime.status.api_unavailable")
	case "ready":
		if s.Traffic == nil || s.Connections == nil || s.Memory == nil {
			return locale.T("runtime.status.partial")
		}
		return locale.T("runtime.status.live")
	default:
		return locale.T("runtime.status.connecting")
	}
}
