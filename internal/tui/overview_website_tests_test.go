package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestOverviewWebsiteSingleAndBatch(t *testing.T) {
	calls := make(map[string]int)
	b := &fakeBackend{testWebsite: func(_ context.Context, site string) (app.WebsiteTestResult, error) {
		calls[site]++
		if site == "YouTube" {
			return app.WebsiteTestResult{}, context.DeadlineExceeded
		}
		return app.WebsiteTestResult{StatusCode: 200, Duration: 73 * time.Millisecond}, nil
	}}
	m := New(t.Context(), b, "en")
	m.busy = false
	m.adoptSnapshot(app.Snapshot{Installed: true, MixedPort: 17890, Service: service.Status{State: "active", Active: true, PID: 42}})
	github := m.activate("website.GitHub")
	all := m.activate("website.all")
	if github == nil || all == nil || m.activate("website.GitHub") != nil || m.activate("website.all") != nil || m.busy {
		t.Fatal("expected an independent GitHub request and a nonblocking batch for the remaining websites")
	}
	cmds := all().(tea.BatchMsg)
	if len(cmds) != 2 {
		t.Fatalf("batch issued %d new requests, want 2", len(cmds))
	}
	m.websiteTests.accept(cmds[1]().(websiteTestMsg))
	yt := m.websiteTests.rows["YouTube"]
	if yt.pending() || yt.checkedAt.IsZero() || !errors.Is(yt.err, context.DeadlineExceeded) ||
		!m.websiteTests.rows["GitHub"].pending() || m.activate("website.all") != nil {
		t.Fatal("a timeout should update just its own row while the batch is running")
	}
	m.websiteTests.accept(github().(websiteTestMsg))
	m.navigate("settings")
	m.websiteTests.accept(cmds[0]().(websiteTestMsg))
	if m.activate("website.all") != nil {
		t.Fatal("only the overview should dispatch a website test")
	}
	m.navigate("overview")
	for _, site := range app.WebsiteTestSites() {
		row := m.websiteTests.rows[site]
		if calls[site] != 1 || row.pending() || row.checkedAt.IsZero() {
			t.Fatalf("%s row after batch = %+v, calls = %d", site, row, calls[site])
		}
		if site != "YouTube" && (row.err != nil || row.result.StatusCode != 200 || row.result.Duration != 73*time.Millisecond) {
			t.Fatalf("%s result = %+v", site, row)
		}
	}
	if m.activate("website.example") != nil || m.activate("website.YouTube") == nil {
		t.Fatal("only the configured sites should accept a new test")
	}
	m.websiteTests.clear()
}

func TestOverviewWebsiteDiscardsStaleRequests(t *testing.T) {
	b := &fakeBackend{testWebsite: func(ctx context.Context, _ string) (app.WebsiteTestResult, error) {
		return app.WebsiteTestResult{StatusCode: 200}, ctx.Err()
	}}
	m := New(t.Context(), b, "en")
	m.busy = false
	s := app.Snapshot{Installed: true, MixedPort: 17890, Service: service.Status{State: "active", Active: true, PID: 42},
		Mode: app.ModeStatus{Actual: "rule"}}
	m.adoptSnapshot(s)
	old := m.activate("website.Google")
	s.Mode.Actual = "global"
	m.adoptSnapshot(s)
	canceled := old().(websiteTestMsg)
	if !errors.Is(canceled.err, context.Canceled) || len(m.websiteTests.rows) != 0 {
		t.Fatal("a route change should cancel and invalidate the previous route's tests")
	}
	current := m.activate("website.Google")
	canceled.err = nil // A completion can race with cancellation.
	m.websiteTests.accept(canceled)
	if !m.websiteTests.rows["Google"].pending() {
		t.Fatal("late completion replaced the newer test")
	}
	m.websiteTests.accept(current().(websiteTestMsg))
	s.Service.PID++
	m.adoptSnapshot(s)
	if len(m.websiteTests.rows) != 0 {
		t.Fatal("a restarted process inherited website results")
	}
	pending := m.activate("website.Google")
	s.Service.State, s.Service.Active = "inactive", false
	m.adoptSnapshot(s)
	if !errors.Is(pending().(websiteTestMsg).err, context.Canceled) || m.activate("website.all") != nil {
		t.Fatal("a stopped core should have no pending or available test")
	}
	s.Service.State, s.Service.Active = "active", true
	m.adoptSnapshot(s)
	pending = m.activate("website.Google")
	m.requestQuit()
	if !errors.Is(pending().(websiteTestMsg).err, context.Canceled) || len(m.websiteTests.rows) != 0 {
		t.Fatal("quit should cancel remaining website requests")
	}
}

func TestOverviewWebsiteBatchRevealsResults(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
	}{{"header at bottom", 80, 24}, {"shorter than card", 40, 10}, {"already visible", 160, 48}} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(t.Context(), newOverviewPTYBackend("running"), "zh-Hans")
			m.busy = false
			m.adoptSnapshot(m.backend.(*overviewPTYBackend).snapshot)
			m.width, m.height, m.focus = tc.width, tc.height, "website.all"
			m.layout()
			var card hitRegion
			for _, region := range overviewCardRegionsFromModel(m) {
				if region.ID == "overview.card.dashboard.website.title" {
					card = region
				}
			}
			if card.H == 0 {
				t.Fatal("missing website card")
			}
			height := dashboardGeometry(m.dashboardOptions()).height
			// Put only the header's button at the bottom of the viewport.
			if tc.width < 160 {
				for _, target := range m.contentTargets() {
					if target.ID == "website.all" {
						m.scroll = target.Y - height + 1
					}
				}
			}
			before := m.scroll
			m, cmd := update(m, special(tea.KeyEnter))
			defer m.websiteTests.clear()
			if cmd == nil || !m.websiteTests.batch || m.focus != "website.all" {
				t.Fatalf("batch did not start with stable focus: %q", m.focus)
			}
			if m.scroll > card.Y || m.scroll+height < card.Y+min(card.H, height) {
				t.Fatalf("scroll %d, viewport height %d, card at %d with height %d", m.scroll, height, card.Y, card.H)
			}
			if tc.width == 160 && before != m.scroll {
				t.Fatalf("visible card moved scroll from %d to %d", before, m.scroll)
			}
		})
	}
}
