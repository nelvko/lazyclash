package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

type websiteTestState struct {
	rows    map[string]websiteTestRow
	request uint64
	batch   bool
}

type websiteTestRow struct {
	request   uint64
	cancel    context.CancelFunc
	result    app.WebsiteTestResult
	err       error
	checkedAt time.Time
}

func (r websiteTestRow) pending() bool { return r.cancel != nil }

type websiteTestMsg struct {
	site    string
	request uint64
	result  app.WebsiteTestResult
	err     error
}

func (s websiteTestState) enabled(target string) bool {
	if target == "all" && s.batch {
		return false
	}
	for _, site := range app.WebsiteTestSites() {
		if (target == site || target == "all") && !s.rows[site].pending() {
			return true
		}
	}
	return false
}

func (m *Model) testWebsites(target string) tea.Cmd {
	s := &m.websiteTests
	if m.page != "overview" || m.busy || m.quitting || m.ctx.Err() != nil || !m.snapshotReady || m.snapshotStale ||
		!overviewProxyAvailable(m.snapshot) || !s.enabled(target) {
		return nil
	}
	if s.rows == nil {
		s.rows = make(map[string]websiteTestRow)
	}
	backend := m.backend
	var commands []tea.Cmd
	for _, site := range app.WebsiteTestSites() {
		if (target != site && target != "all") || s.rows[site].pending() {
			continue
		}
		ctx, cancel := context.WithCancel(m.ctx)
		s.request++
		id := s.request
		s.rows[site] = websiteTestRow{request: id, cancel: cancel}
		commands = append(commands, func() tea.Msg {
			result, err := backend.TestWebsite(ctx, site)
			return websiteTestMsg{site: site, request: id, result: result, err: err}
		})
	}
	if target == "all" {
		s.batch = true
		for _, card := range overviewCardRegionsFromModel(*m) {
			if card.ID == "overview.card.dashboard.website.title" {
				// In a short viewport keep the heading and first results together.
				card.H = min(card.H, max(1, dashboardGeometry(m.dashboardOptions()).height))
				m.revealBodyTarget(card)
				break
			}
		}
	}
	return tea.Batch(commands...)
}

func (s *websiteTestState) accept(msg websiteTestMsg) {
	row := s.rows[msg.site]
	if !row.pending() || msg.request != row.request {
		return
	}
	row.cancel()
	s.rows[msg.site] = websiteTestRow{result: msg.result, err: msg.err, checkedAt: time.Now()}
	for _, row := range s.rows {
		if row.pending() {
			return
		}
	}
	s.batch = false
}

func (s *websiteTestState) clear() {
	for _, row := range s.rows {
		if row.pending() {
			row.cancel()
		}
	}
	s.rows, s.batch = nil, false
}
