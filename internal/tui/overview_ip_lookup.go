package tui

import (
	"context"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type overviewIPState struct {
	info            app.IPInfo
	route           overviewIPRoute
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

type overviewIPRoute struct {
	profile, recipe, mode string
	groups                []mihomo.Group
	groupsKnown           bool
}

// The pinned profile, mode and proxy groups come from independent reads.
func (s *overviewIPState) observeRoute(snapshot app.Snapshot) {
	if !overviewIPAvailable(snapshot) {
		return
	}
	r := &s.route
	changed := r.profile != snapshot.Applied || r.recipe != snapshot.AppliedRecipeHash
	if changed {
		*r = overviewIPRoute{profile: snapshot.Applied, recipe: snapshot.AppliedRecipeHash}
	}
	if mode := snapshot.Mode.Actual; mode != "" && snapshot.Mode.Error == "" {
		changed = changed || (r.mode != "" && r.mode != mode)
		r.mode = mode
	}
	if snapshot.APIError == "" || snapshot.Groups != nil {
		if r.groupsKnown && !slices.EqualFunc(r.groups, snapshot.Groups, func(a, b mihomo.Group) bool {
			return a.Name == b.Name && a.Now == b.Now
		}) {
			changed = true
		}
		r.groups, r.groupsKnown = snapshot.Groups, true
	}
	if changed {
		s.clearResult()
	}
}

func (s *overviewIPState) stop() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.loading = false
}

func (s *overviewIPState) clearResult() {
	s.stop()
	s.info, s.checkedAt, s.failed = app.IPInfo{}, time.Time{}, false
}

func (m *Model) syncIPInfo(force bool) tea.Cmd {
	s := &m.ipInfo
	process := m.snapshot.Service
	if !m.snapshotReady || !overviewIPAvailable(m.snapshot) {
		s.clearResult()
		return nil
	}
	if s.pid != process.PID || s.started != process.StartTimestampMonotonic {
		s.clearResult()
		s.pid, s.started = process.PID, process.StartTimestampMonotonic
	}
	if m.page != "overview" || m.quitting || m.ctx.Err() != nil || m.inlineInstallVisible() {
		s.stop()
		return nil
	}
	if m.busy || s.loading || (!force && !s.checkedAt.IsZero()) {
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
