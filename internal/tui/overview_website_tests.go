package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

const maxActiveWebsiteTests = 3

func websiteActionID(site app.Website) string { return "website.site." + site.URL }

func websiteColumns(inner int) int {
	switch {
	case inner >= 62:
		return 3
	case inner >= 40:
		return 2
	default:
		return 1
	}
}

func websiteCapacity(inner int) int { return 2 * websiteColumns(inner) }

type websiteTestState struct {
	rows    map[string]websiteTestRow // keyed by normalized URL
	running map[uint64]bool           // canceled probes retain their slot until their command returns
	request uint64
	batch   bool
}

type websiteTestRow struct {
	request uint64
	cancel  context.CancelFunc
	queued  bool
	result  app.WebsiteTestResult
	err     error
}

func (r websiteTestRow) pending() bool { return r.queued || r.cancel != nil }

type websiteTestMsg struct {
	url     string
	request uint64
	result  app.WebsiteTestResult
	err     error
}

func (s websiteTestState) canTestAll(sites []app.Website) bool {
	if !s.batch {
		for _, site := range sites {
			if !s.rows[site.URL].pending() {
				return true
			}
		}
	}
	return false
}

func (m *Model) setWebsites(sites []app.Website) {
	kept := make(map[string]bool, len(sites))
	for _, site := range sites {
		kept[site.URL] = true
	}
	for _, site := range m.websites {
		if !kept[site.URL] {
			row := m.websiteTests.rows[site.URL]
			if row.cancel != nil {
				row.cancel()
			}
			delete(m.websiteTests.rows, site.URL)
		}
	}
	m.websites = sites
	m.websiteTests.finishBatch()
}

func (m *Model) websiteCardInnerWidth() int {
	width := dashboardBodyWidth(m.dashboardOptions())
	if width >= 90 {
		width = (width - 2) / 2
	}
	return overviewInnerWidth(width)
}

func (m *Model) revealWebsiteCard() {
	for _, card := range overviewCardRegionsFromModel(*m) {
		if card.ID == "overview.card.dashboard.website.title" {
			card.H = min(card.H, max(1, dashboardGeometry(m.dashboardOptions()).height))
			m.revealBodyTarget(card)
			return
		}
	}
}

func (m *Model) testWebsites(target string) tea.Cmd {
	s := &m.websiteTests
	if m.page != "overview" || m.busy || m.quitting || m.ctx.Err() != nil || !m.snapshotReady || m.snapshotStale ||
		!overviewProxyAvailable(m.snapshot) || target == "website.all" && !s.canTestAll(m.websites) {
		return nil
	}
	if s.rows == nil {
		s.rows = make(map[string]websiteTestRow)
	}
	for _, site := range m.websites {
		if (target == websiteActionID(site) || target == "website.all") && !s.rows[site.URL].pending() {
			s.rows[site.URL] = websiteTestRow{queued: true}
		}
	}
	if target == "website.all" {
		s.batch = true
		if m.form == "" {
			m.revealWebsiteCard()
		}
	}
	return m.nextWebsiteTests()
}

func (m *Model) nextWebsiteTests() tea.Cmd {
	s := &m.websiteTests
	backend := m.backend
	var commands []tea.Cmd
	for _, site := range m.websites {
		if len(s.running) >= maxActiveWebsiteTests {
			break
		}
		if !s.rows[site.URL].queued {
			continue
		}
		ctx, cancel := context.WithCancel(m.ctx)
		s.request++
		id := s.request
		if s.running == nil {
			s.running = make(map[uint64]bool)
		}
		s.running[id] = true
		s.rows[site.URL] = websiteTestRow{request: id, cancel: cancel}
		commands = append(commands, func() tea.Msg {
			result, err := backend.TestWebsite(ctx, site)
			return websiteTestMsg{url: site.URL, request: id, result: result, err: err}
		})
	}
	return tea.Batch(commands...)
}

func (m *Model) acceptWebsiteTest(msg websiteTestMsg) tea.Cmd {
	s := &m.websiteTests
	delete(s.running, msg.request)
	row := s.rows[msg.url]
	if row.cancel != nil && row.request == msg.request {
		row.cancel()
		s.rows[msg.url] = websiteTestRow{result: msg.result, err: msg.err}
	}
	// A removed site's canceled request also releases a slot for queued sites.
	s.finishBatch()
	return m.nextWebsiteTests()
}

func (s *websiteTestState) finishBatch() {
	for _, row := range s.rows {
		if row.pending() {
			return
		}
	}
	s.batch = false
}

func (s *websiteTestState) clear() {
	for _, row := range s.rows {
		if row.cancel != nil {
			row.cancel()
		}
	}
	s.rows, s.batch = nil, false
}
