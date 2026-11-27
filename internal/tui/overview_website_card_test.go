package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestWebsiteCardShowsPerSiteResultAndActions(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		o := dashboardOptions{Locale: i18n.New(language), Now: time.Now(), Snapshot: app.Snapshot{
			Installed: true, MixedPort: 7890, Service: service.Status{Active: true, State: "active"},
		}}
		o.websiteTests.rows = map[string]websiteTestRow{
			"GitHub":  {result: app.WebsiteTestResult{StatusCode: 200, Duration: 123 * time.Millisecond}, checkedAt: o.Now},
			"Google":  {result: app.WebsiteTestResult{StatusCode: 403, Duration: 37 * time.Millisecond}, checkedAt: o.Now.Add(-2 * time.Minute)},
			"YouTube": {err: context.DeadlineExceeded, checkedAt: o.Now},
		}
		for _, width := range []int{72, 64, 52, 44, 24, 16} {
			card := overviewWebsiteCard(o, width)
			view := strings.Join(card.lines, "\n")
			assertCanvas(t, view, width, len(card.lines))
			assertRegions(t, card.targets, width, len(card.lines))
			compact := strings.Join(strings.Fields(ansi.Strip(view)), "")
			for _, label := range []string{"GitHub", "Google", "YouTube", o.Locale.T("dashboard.website.reachable"),
				"123 ms", o.Locale.T("dashboard.website.forbidden"), o.Locale.T("profiles.updated_minutes", 2), o.Locale.T("dashboard.website.timeout")} {
				if !strings.Contains(compact, strings.Join(strings.Fields(label), "")) {
					t.Fatalf("%s width %d missing %q: %s", language, width, label, ansi.Strip(view))
				}
			}
			if width >= 24 {
				for _, hit := range card.targets {
					if site := strings.TrimPrefix(hit.ID, "website."); site != "all" && !strings.Contains(ansi.Strip(card.lines[hit.Y]), site) {
						t.Fatalf("width %d separated the %s button from its site: %s", width, site, ansi.Strip(view))
					}
				}
			}
			for _, id := range []string{"website.all", "website.GitHub", "website.Google", "website.YouTube"} {
				if !hasAction(card.targets, id) {
					t.Fatalf("width %d missing %s", width, id)
				}
			}
		}

		o.websiteTests.rows["GitHub"] = websiteTestRow{err: errors.New("private diagnostic"), checkedAt: o.Now}
		o.websiteTests.rows["Google"] = websiteTestRow{result: app.WebsiteTestResult{StatusCode: 503}, checkedAt: o.Now}
		delete(o.websiteTests.rows, "YouTube")
		card := overviewWebsiteCard(o, 70)
		view := ansi.Strip(strings.Join(card.lines, "\n"))
		for _, want := range []string{o.Locale.T("dashboard.website.failed"), "HTTP 503", o.Locale.T("dashboard.website.untested")} {
			if !strings.Contains(view, want) {
				t.Fatalf("missing %s in %s", want, view)
			}
		}
		if strings.Contains(view, "private diagnostic") {
			t.Fatal("website row exposed raw diagnostics")
		}
		o.Snapshot.Service.Active = false
		card = overviewWebsiteCard(o, 70)
		view = ansi.Strip(strings.Join(card.lines, "\n"))
		if len(card.targets) != 0 || strings.Contains(view, "HTTP 503") || strings.Count(view, o.Locale.T("dashboard.status.unavailable")) != 3 {
			t.Fatalf("stopped proxy retained website actions or results: %s", view)
		}
	}
}

func TestWebsiteCardScrollsFocusedTestIntoCompactViewport(t *testing.T) {
	for _, lang := range []string{i18n.English, i18n.Chinese} {
		m := themeSample("overview-running")
		m.locale, m.width, m.height, m.snapshot.MixedPort = i18n.New(lang), 40, 16, 7890
		m.websiteTests.rows = map[string]websiteTestRow{}
		for _, site := range app.WebsiteTestSites() {
			m.websiteTests.rows[site] = websiteTestRow{result: app.WebsiteTestResult{StatusCode: 403, Duration: 231 * time.Millisecond}, checkedAt: time.Now()}
		}
		m.focus = "overview.card.dashboard.website.title"
		m.revealContentFocus()
		m.layout()
		for _, id := range []string{"website.all", "website.GitHub", "website.Google", "website.YouTube"} {
			k := special(tea.KeyTab)
			if id == "website.all" {
				k = special(tea.KeyEnter)
			}
			m, _ = update(m, k)
			if m.focus != id || !hasAction(m.targets, id) {
				t.Fatalf("%s: expected visible focus on %s, got %s", lang, id, m.focus)
			}
		}
	}
}
