package tui

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// One application session owns all four live sources. Pages only read this
// cache; navigation, overlays and ordinary foreground jobs do not reconnect it.
type runtimeState struct {
	snapshot                  app.RuntimeSnapshot
	history                   trafficHistory
	chartScale                trafficScaleState
	generation, instance      uint64
	installation              string
	processPID                int
	active, pending, received bool
	refreshNeeded             bool
	cancel                    context.CancelFunc
	task                      *runtimeTask
	retryAt                   time.Time
	ctx                       context.Context
	updates                   <-chan app.RuntimeEvent
	observed                  map[app.RuntimeSource]time.Time
}

// Either the command or cancellation claims responsibility for done. Commands
// that were returned to Bubble Tea but never executed must not block a retry.
type runtimeTask struct {
	claimed  atomic.Bool
	done     chan struct{}
	previous <-chan struct{}
}

func (task *runtimeTask) finish() {
	if task.previous != nil {
		select {
		case <-task.previous:
		default:
			// A cancelled queued command must carry its predecessor's cleanup
			// barrier, so another rapid retry cannot overtake that predecessor.
			go func() { <-task.previous; close(task.done) }()
			return
		}
	}
	close(task.done)
}

type runtimeResultMsg struct {
	generation uint64
	events     []app.RuntimeEvent
}

type runtimeEndedMsg struct {
	generation uint64
	err        error
}

type runtimeRetryMsg struct{ generation uint64 }

func (m *Model) stopRuntime() {
	if m.runtime.cancel != nil {
		m.runtime.cancel()
		m.runtime.cancel = nil
	}
	if m.runtime.active {
		m.runtime.generation++
	}
	if task := m.runtime.task; task != nil && task.claimed.CompareAndSwap(false, true) {
		task.finish()
	}
	m.runtime.active, m.runtime.pending, m.logs.active = false, false, false
}

func (m *Model) syncRuntime() tea.Cmd {
	if !m.runtime.active {
		m.runtime.history.observeSnapshot(m.snapshot)
	}
	if m.quitting || m.ctx.Err() != nil {
		m.stopRuntime()
		return nil
	}
	if m.runtime.active {
		if m.runtime.refreshNeeded && !m.busy && m.statusCancel == nil {
			m.runtime.refreshNeeded = false
			return m.refreshCoreStatus()
		}
		return nil
	}
	if !m.snapshotReady {
		return nil
	}
	if !m.runtime.retryAt.IsZero() && time.Now().Before(m.runtime.retryAt) {
		return nil
	}
	// Once started, stay subscribed even while stopped so the central owner can
	// detect a restart. A Logs visit also gives unavailable cores a diagnostic.
	if !m.snapshot.Installed && m.page != "logs" && !m.logs.started {
		return nil
	}
	m.runtime.generation++
	m.runtime.instance = 0
	m.runtime.observed = make(map[app.RuntimeSource]time.Time)
	ctx, cancel := context.WithCancel(m.ctx)
	updates := make(chan app.RuntimeEvent, 128)
	previousTask := m.runtime.task
	task := &runtimeTask{done: make(chan struct{})}
	if previousTask != nil {
		task.previous = previousTask.done
	}
	m.runtime.active, m.runtime.pending = true, true
	m.runtime.cancel, m.runtime.ctx, m.runtime.updates = cancel, ctx, updates
	m.runtime.task, m.runtime.retryAt = task, time.Time{}
	m.logs.started, m.logs.active = true, true
	id, backend := m.runtime.generation, m.backend
	run := func() tea.Msg {
		if !task.claimed.CompareAndSwap(false, true) {
			return nil
		}
		defer task.finish()
		defer close(updates)
		if previousTask != nil {
			select {
			case <-previousTask.done:
			case <-ctx.Done():
				return nil
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		// StreamRuntime owns endpoint retries and the one shared process watcher.
		err := backend.StreamRuntime(ctx, func(event app.RuntimeEvent) {
			select {
			case updates <- event:
			case <-ctx.Done():
			}
		})
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			err = errors.New("Runtime monitoring ended unexpectedly; reconnecting")
		}
		return runtimeEndedMsg{generation: id, err: err}
	}
	return tea.Batch(run, waitRuntime(ctx, id, updates))
}

// Endpoint failures are retried by the application. This path handles only an
// unexpected end of the entire session, and leaves one guarded retry scheduled.
func (m *Model) acceptRuntimeEnded(msg runtimeEndedMsg) tea.Cmd {
	if !m.runtime.active || msg.generation != m.runtime.generation {
		return nil
	}
	m.stopRuntime()
	now := time.Now()
	err := msg.err
	if err == nil {
		err = errors.New("Runtime monitoring ended unexpectedly; reconnecting")
	}
	previous := m.runtime.snapshot
	m.runtime.snapshot = app.RuntimeSnapshot{
		State: "unavailable", Service: previous.Service, RunningVersion: previous.RunningVersion,
		ObservedAt: now, Error: err.Error(), Diagnostic: err,
	}
	m.runtime.received = true
	m.runtime.history.observe(m.runtime.snapshot, now)
	m.runtime.chartScale.lowSince = time.Time{}
	m.observeConnections(now)
	m.snapshot.APIError, m.snapshot.APIDiagnostic = err.Error(), err
	m.logs.reconnecting, m.logs.recovered, m.logs.err = true, false, err
	m.runtime.retryAt = now.Add(time.Second)
	id := m.runtime.generation
	return tea.Tick(time.Until(m.runtime.retryAt), func(time.Time) tea.Msg { return runtimeRetryMsg{generation: id} })
}

// Drain a bounded batch without merging traffic or log events: every real
// observation reaches its history even when redraw notifications are batched.
func waitRuntime(ctx context.Context, generation uint64, updates <-chan app.RuntimeEvent) tea.Cmd {
	return func() tea.Msg {
		result := runtimeResultMsg{generation: generation}
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-updates:
			if !ok {
				return nil
			}
			result.events = append(result.events, event)
		}
		for len(result.events) < 32 {
			select {
			case event, ok := <-updates:
				if !ok {
					return result
				}
				result.events = append(result.events, event)
			default:
				return result
			}
		}
		return result
	}
}

func (m *Model) acceptRuntime(msg runtimeResultMsg) tea.Cmd {
	if !m.runtime.active || msg.generation != m.runtime.generation {
		return nil
	}
	var refresh tea.Cmd
	for _, event := range msg.events {
		if m.acceptRuntimeEvent(event) {
			m.runtime.refreshNeeded = true
		}
		if m.runtime.refreshNeeded && !m.busy && m.statusCancel == nil {
			m.runtime.refreshNeeded = false
			refresh = m.refreshCoreStatus()
		}
	}
	return tea.Batch(waitRuntime(m.runtime.ctx, m.runtime.generation, m.runtime.updates), refresh)
}

// Returns true only for a service transition that needs a full settings/profile
// refresh. Per-source failures never erase another source's successful values.
func (m *Model) acceptRuntimeEvent(event app.RuntimeEvent) bool {
	now := time.Now()
	at := event.ObservedAt
	if at.IsZero() {
		at = now
	}
	if at.After(now) {
		return false
	}
	if event.Source == app.RuntimeStatus {
		if event.Instance < m.runtime.instance {
			return false
		}
	} else if event.Instance != m.runtime.instance {
		return false
	}
	// Handshakes report connectivity, not an observed metric. In particular a
	// reconnection must not advance the connections clock for an old snapshot.
	if event.Connected {
		if event.Source == app.RuntimeLogs {
			m.logs.recovered = m.logs.recovered || m.logs.reconnecting
			m.logs.reconnecting, m.logs.err = false, nil
		}
		return false
	}
	if last := m.runtime.observed[event.Source]; !last.IsZero() && !at.After(last) {
		return false
	}
	if m.runtime.observed == nil {
		m.runtime.observed = make(map[app.RuntimeSource]time.Time)
	}
	m.runtime.observed[event.Source] = at
	m.runtime.pending, m.runtime.received = false, true
	s := &m.runtime.snapshot
	switch event.Source {
	case app.RuntimeStatus:
		if event.Installation != "" {
			if m.runtime.installation != "" && m.runtime.installation != event.Installation {
				m.runtime.history.clear()
			}
			m.runtime.installation = event.Installation
		}
		if event.Snapshot.Service.PID > 0 {
			if m.runtime.processPID > 0 && m.runtime.processPID != event.Snapshot.Service.PID {
				m.runtime.history.clear()
			}
			m.runtime.processPID = event.Snapshot.Service.PID
		}
		previous := *s
		previousService := m.snapshot.Service
		current := event.Snapshot
		if event.Err != nil {
			current = app.RuntimeSnapshot{State: "unavailable", Error: event.Err.Error(), Diagnostic: event.Err}
		}
		changed := event.Instance != m.runtime.instance
		if changed || current.State != "ready" {
			*s = current
			m.runtime.instance = event.Instance
			// A new process must not inherit another process's ordering watermarks.
			m.runtime.observed = map[app.RuntimeSource]time.Time{app.RuntimeStatus: at}
			m.observeConnections(at)
		} else {
			s.State, s.Service, s.Error, s.Diagnostic = current.State, current.Service, current.Error, current.Diagnostic
			if current.RunningVersion != "" {
				s.RunningVersion = current.RunningVersion
			}
		}
		s.ObservedAt = at
		if current.Service.State != "" {
			m.snapshot.Service = current.Service
			m.snapshot.ServiceError, m.snapshot.ServiceDiagnostic = "", nil
		} else if current.State == "unavailable" || current.State == "unsupported" {
			m.snapshot.ServiceError, m.snapshot.ServiceDiagnostic = current.Error, current.Diagnostic
		}
		if current.State == "ready" || current.State == "stopped" {
			m.snapshot.Installed = true
		}
		if current.State == "ready" {
			if previous.State != "ready" || previousService.State == "unknown" || m.snapshot.APIError == previous.Error {
				m.snapshot.APIError, m.snapshot.APIDiagnostic = "", nil
			}
		} else {
			m.snapshot.APIError, m.snapshot.APIDiagnostic = current.Error, current.Diagnostic
		}
		if current.State == "uninstalled" {
			m.snapshot.Installed, m.snapshot.ConfigReady, m.snapshot.Service.Active = false, false, false
		}
		if current.State == "stopped" || current.State == "uninstalled" {
			m.snapshot.Groups, m.snapshot.Proxies = nil, nil
			m.runtime.history.clear()
		}
		if current.State != "ready" {
			m.logs.reconnecting, m.logs.recovered = true, false
			m.runtime.chartScale.lowSince = time.Time{}
		}
		m.runtime.history.observeSnapshot(m.snapshot)
		m.snapshot.RunningVersion = s.RunningVersion
		return previous.State != current.State || previousService != m.snapshot.Service
	case app.RuntimeVersion:
		if event.Err == nil {
			s.RunningVersion = event.Snapshot.RunningVersion
			m.snapshot.RunningVersion = s.RunningVersion
			if s.RunningVersion != "" {
				m.snapshot.HasRun = true
			}
		}
	case app.RuntimeTraffic:
		if s.State != "ready" {
			return false
		}
		s.Traffic, s.TrafficError, s.TrafficDiagnostic = event.Traffic, "", nil
		if event.Err != nil {
			s.Traffic, s.TrafficError, s.TrafficDiagnostic = nil, event.Err.Error(), event.Err
		}
		sample := *s
		sample.ObservedAt = at
		if m.runtime.history.observe(sample, now) {
			m.runtime.chartScale.observe(m.runtime.history.points, at)
		}
	case app.RuntimeMemory:
		if s.State != "ready" {
			return false
		}
		s.Memory, s.MemoryError, s.MemoryDiagnostic = event.Memory, "", nil
		if event.Err != nil {
			s.Memory, s.MemoryError, s.MemoryDiagnostic = nil, event.Err.Error(), event.Err
		}
	case app.RuntimeConnections:
		if s.State != "ready" {
			return false
		}
		s.Connections, s.ConnectionsError, s.ConnectionsDiagnostic = event.Connections, "", nil
		if event.Err != nil {
			s.Connections, s.ConnectionsError, s.ConnectionsDiagnostic = nil, event.Err.Error(), event.Err
		}
		// Per-connection rates use this source's timestamp, never the most recent
		// traffic/memory/status notification.
		s.ObservedAt = at
		m.observeConnections(at)
	case app.RuntimeLogs:
		if event.Err != nil {
			m.logs.reconnecting, m.logs.recovered, m.logs.err = true, false, event.Err
		} else if event.Log != nil {
			m.appendLogs([]mihomo.LogEntry{*event.Log})
			m.logs.recovered = m.logs.recovered || m.logs.reconnecting
			m.logs.reconnecting, m.logs.err = false, nil
		}
	}
	return false
}

func (m *Model) retryRuntime() tea.Cmd {
	m.stopRuntime()
	m.runtime.retryAt = time.Time{}
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
