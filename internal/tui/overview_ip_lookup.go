package tui

import (
	"context"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

const ipRefreshInterval = 5 * time.Minute

type overviewIPState struct {
	info            app.IPInfo
	loading, failed bool
	checkedAt       time.Time
	pid             int
	started         uint64
	request         uint64
	cancel          context.CancelFunc
}

type ipInfoResultMsg struct {
	request uint64
	info    app.IPInfo
	err     error
}

func overviewIPAvailable(s app.Snapshot) bool {
	return s.Installed && s.Service.Active && s.Service.State == "active" && s.ServiceError == "" &&
		(s.MixedPort > 0 || s.RuntimeSettings.HTTPPort > 0 || s.RuntimeSettings.SOCKSPort > 0)
}

// A selector can switch nodes without publishing a new config generation.
func ipRouteChanged(before, after app.Snapshot) bool {
	return before.Generation != after.Generation || before.Mode.Actual != after.Mode.Actual ||
		!slices.EqualFunc(before.Groups, after.Groups, func(a, b mihomo.Group) bool {
			return a.Name == b.Name && a.Now == b.Now
		})
}

func (s *overviewIPState) stop() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.loading = false
}

func (s *overviewIPState) reset() {
	s.stop()
	*s = overviewIPState{request: s.request}
}

// The runtime heartbeat already wakes the overview every second.
func (m *Model) syncIPInfo(force bool) tea.Cmd {
	s := &m.ipInfo
	process := m.snapshot.Service
	if !m.snapshotReady || !overviewIPAvailable(m.snapshot) {
		s.reset()
		return nil
	}
	if s.pid != process.PID || s.started != process.StartTimestampMonotonic {
		s.reset()
		s.pid, s.started = process.PID, process.StartTimestampMonotonic
	}
	if m.page != "overview" || m.quitting || m.ctx.Err() != nil || m.inlineInstallVisible() {
		s.stop()
		return nil
	}
	if m.busy || s.loading || (!force && time.Since(s.checkedAt) < ipRefreshInterval) {
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	s.cancel, s.loading = cancel, true
	s.request++
	id, backend := s.request, m.backend
	return func() tea.Msg {
		info, err := backend.LookupIPInfo(ctx)
		return ipInfoResultMsg{request: id, info: info, err: err}
	}
}

func (m *Model) acceptIPInfo(msg ipInfoResultMsg) {
	s := &m.ipInfo
	if !s.loading || msg.request != s.request {
		return
	}
	s.stop()
	s.checkedAt, s.failed = time.Now(), msg.err != nil
	s.info = msg.info
}
