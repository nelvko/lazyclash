package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestOverviewWebsiteSingleAndBatch(t *testing.T) {
	calls := make(map[string]int)
	b := &fakeBackend{testWebsite: func(_ context.Context, site app.Website) (app.WebsiteTestResult, error) {
		calls[site.Name]++
		if site.Name == "Cloudflare" {
			return app.WebsiteTestResult{}, context.DeadlineExceeded
		}
		return app.WebsiteTestResult{StatusCode: 200, Duration: 73 * time.Millisecond}, nil
	}}
	m := New(t.Context(), b, "en")
	m.busy = false
	m.adoptSnapshot(app.Snapshot{Installed: true, MixedPort: 17890, Service: service.Status{State: "active", Active: true, PID: 42}})
	github := m.activate(websiteActionID(app.DefaultWebsites()[0]))
	all := m.activate("website.all")
	if github == nil || all == nil || m.activate(websiteActionID(app.DefaultWebsites()[0])) != nil || m.activate("website.all") != nil || m.busy {
		t.Fatal("expected an independent GitHub request and a nonblocking batch for the remaining websites")
	}
	cmds := all().(tea.BatchMsg)
	if len(cmds) != 2 {
		t.Fatalf("batch issued %d new requests, want 2", len(cmds))
	}
	m.acceptWebsiteTest(cmds[1]().(websiteTestMsg))
	cloudflare := m.websiteTests.rows[app.DefaultWebsites()[2].URL]
	if cloudflare.pending() || !errors.Is(cloudflare.err, context.DeadlineExceeded) ||
		!m.websiteTests.rows[app.DefaultWebsites()[0].URL].pending() || m.activate("website.all") != nil {
		t.Fatal("a timeout should update just its own row while the batch is running")
	}
	m.acceptWebsiteTest(github().(websiteTestMsg))
	m.navigate("settings")
	m.acceptWebsiteTest(cmds[0]().(websiteTestMsg))
	if m.activate("website.all") != nil {
		t.Fatal("only the overview should dispatch a website test")
	}
	m.navigate("overview")
	for _, site := range app.DefaultWebsites() {
		row := m.websiteTests.rows[site.URL]
		if calls[site.Name] != 1 || row.pending() {
			t.Fatalf("%s row after batch = %+v, calls = %d", site, row, calls[site.Name])
		}
		if site.Name != "Cloudflare" && (row.err != nil || row.result.StatusCode != 200 || row.result.Duration != 73*time.Millisecond) {
			t.Fatalf("%s result = %+v", site, row)
		}
	}
	if m.activate("website.example") != nil || m.activate(websiteActionID(app.DefaultWebsites()[2])) == nil {
		t.Fatal("only the configured sites should accept a new test")
	}
	m.websiteTests.clear()
}

func TestOverviewWebsiteDiscardsStaleRequests(t *testing.T) {
	b := &fakeBackend{testWebsite: func(ctx context.Context, _ app.Website) (app.WebsiteTestResult, error) {
		return app.WebsiteTestResult{StatusCode: 200}, ctx.Err()
	}}
	m := New(t.Context(), b, "en")
	m.busy = false
	s := app.Snapshot{Installed: true, MixedPort: 17890, Service: service.Status{State: "active", Active: true, PID: 42},
		Mode: app.ModeStatus{Actual: "rule"}}
	m.adoptSnapshot(s)
	old := m.activate(websiteActionID(app.DefaultWebsites()[1]))
	s.Mode.Actual = "global"
	m.adoptSnapshot(s)
	canceled := old().(websiteTestMsg)
	if !errors.Is(canceled.err, context.Canceled) || len(m.websiteTests.rows) != 0 {
		t.Fatal("a route change should cancel and invalidate the previous route's tests")
	}
	current := m.activate(websiteActionID(app.DefaultWebsites()[1]))
	canceled.err = nil // A completion can race with cancellation.
	m.acceptWebsiteTest(canceled)
	if !m.websiteTests.rows[app.DefaultWebsites()[1].URL].pending() {
		t.Fatal("late completion replaced the newer test")
	}
	m.acceptWebsiteTest(current().(websiteTestMsg))
	s.Service.PID++
	m.adoptSnapshot(s)
	if len(m.websiteTests.rows) != 0 {
		t.Fatal("a restarted process inherited website results")
	}
	pending := m.activate(websiteActionID(app.DefaultWebsites()[1]))
	s.Service.State, s.Service.Active = "inactive", false
	m.adoptSnapshot(s)
	if !errors.Is(pending().(websiteTestMsg).err, context.Canceled) || m.activate("website.all") != nil {
		t.Fatal("a stopped core should have no pending or available test")
	}
	s.Service.State, s.Service.Active = "active", true
	m.adoptSnapshot(s)
	pending = m.activate(websiteActionID(app.DefaultWebsites()[1]))
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
			backend := newOverviewPTYBackend("running")
			m := New(t.Context(), backend, "zh-Hans")
			m.busy = false
			m.adoptSnapshot(backend.snapshot)
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

func TestWebsiteBatchBoundsRequestsIncludingHiddenSites(t *testing.T) {
	sites := app.DefaultWebsites()
	for i := 4; i <= 8; i++ {
		sites = append(sites, app.Website{Name: fmt.Sprintf("Site %d", i), URL: fmt.Sprintf("https://s%d.example/", i)})
	}
	sites[4].Name = "all"
	calls := make(map[string]int)
	b := &fakeBackend{prefs: app.Preferences{Websites: sites}, testWebsite: func(_ context.Context, site app.Website) (app.WebsiteTestResult, error) {
		calls[site.URL]++
		return app.WebsiteTestResult{StatusCode: 204}, nil
	}}
	m := New(t.Context(), b, "en")
	m.busy = false
	m.adoptSnapshot(app.Snapshot{Installed: true, MixedPort: 7890, Service: service.Status{State: "active", Active: true, PID: 8}})
	m.width, m.height, m.focus = 160, 45, "website.all"
	m.layout()
	cmd := m.activate(m.focus)
	running := cmd().(tea.BatchMsg)
	removed := sites[1]
	stale := running[1]
	running = slices.Delete(running, 1, 2)
	retained := slices.Delete(slices.Clone(sites), 1, 2)
	m.setWebsites(retained)
	m.setWebsites(sites)
	// A newly added entry waits for an explicit test and for the canceled
	// probe's slot. Its replacement must reject the old URL's late result.
	if m.websiteTests.rows[removed.URL].pending() {
		t.Fatal("re-added URL joined an already-issued batch")
	}
	if cmd := m.activate(websiteActionID(removed)); cmd != nil || !m.websiteTests.rows[removed.URL].queued {
		t.Fatal("a fourth probe started before the canceled request returned")
	}
	old := stale().(websiteTestMsg)
	replacement := m.acceptWebsiteTest(old)
	if replacement == nil || m.acceptWebsiteTest(old) != nil || !m.websiteTests.rows[removed.URL].pending() ||
		m.websiteTests.rows[removed.URL].result.StatusCode != 0 {
		t.Fatal("late completion for removed URL replaced its new request")
	}
	running = append(running, replacement)
	for len(running) > 0 {
		if len(running) > 3 {
			t.Fatalf("%d probes in flight", len(running))
		}
		job := running[0]
		running = running[1:]
		if next := m.acceptWebsiteTest(job().(websiteTestMsg)); next != nil {
			running = append(running, next)
		}
	}
	if m.websiteTests.batch || len(calls) != len(sites) {
		t.Fatalf("unfinished batch: calls=%v", calls)
	}
	for _, site := range sites {
		wantCalls := 1
		if site == removed {
			wantCalls++
		}
		if calls[site.URL] != wantCalls || m.websiteTests.rows[site.URL].result.StatusCode != 204 {
			t.Fatalf("incorrect result for %s: %+v", site.Name, m.websiteTests.rows[site.URL])
		}
	}
	single := m.activate(websiteActionID(sites[4]))
	if single == nil || m.websiteTests.batch {
		t.Fatal("site named all launched a batch")
	}
	m.acceptWebsiteTest(single().(websiteTestMsg))
	m.setWebsites([]app.Website{})
	if m.activate("website.all") != nil || !hasAction(m.contentTargets(), "website.add") {
		t.Fatal("empty list must only offer management")
	}
}

func TestWebsiteListKeepsFocusedResultVisibleAcrossResize(t *testing.T) {
	m, _ := overviewWebsiteDragModel(t, 8)
	m.activate("website.viewAll")
	m.focusWebsiteAt(7)
	for _, size := range [][2]int{{40, 16}, {120, 32}, {160, 45}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if m.form != "website-list" || m.focus != websiteActionID(m.websites[7]) || !hasAction(m.targets, m.focus) {
			t.Fatalf("resize to %v lost visible result %q", size, m.focus)
		}
	}
	m.setWebsites(m.websites[:2])
	m.websiteListAction("website.back")
	m.layout()
	if m.form != "" || m.focus != "website.add" || hasAction(m.contentTargets(), "website.viewAll") {
		t.Fatal("return left an unreachable overflow action")
	}
}

func TestWebsiteResultsFollowURLAcrossEditsAndReordering(t *testing.T) {
	sites := app.DefaultWebsites()
	m := Model{websites: sites}
	result := app.WebsiteTestResult{StatusCode: 200, Duration: 42 * time.Millisecond}
	m.websiteTests.rows = map[string]websiteTestRow{sites[0].URL: {result: result}}
	moved := []app.Website{sites[1], {Name: "New name", URL: sites[0].URL}, sites[2]}
	m.setWebsites(moved)
	if m.websiteTests.rows[moved[1].URL].result != result {
		t.Fatal("renaming or moving a site lost the result for its URL")
	}
	changed := slices.Clone(moved)
	changed[1].URL = "https://example.org/"
	m.setWebsites(changed)
	if len(m.websiteTests.rows) != 0 {
		t.Fatal("editing the URL retained the old result")
	}
}
