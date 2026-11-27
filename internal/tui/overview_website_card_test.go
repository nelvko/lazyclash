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
		sites := app.DefaultWebsites()
		o := dashboardOptions{websites: sites, Locale: i18n.New(language), Snapshot: app.Snapshot{
			Installed: true, MixedPort: 7890, Service: service.Status{Active: true, State: "active"},
		}}
		o.websiteTests.rows = map[string]websiteTestRow{
			sites[0].URL: {result: app.WebsiteTestResult{StatusCode: 200, Duration: 123 * time.Millisecond}},
			sites[1].URL: {result: app.WebsiteTestResult{StatusCode: 403, Duration: 37 * time.Millisecond}},
			sites[2].URL: {err: context.DeadlineExceeded},
		}
		for _, width := range []int{72, 64, 52, 44, 24, 16} {
			card := overviewWebsiteCard(o, width)
			view := strings.Join(card.lines, "\n")
			assertCanvas(t, view, width, len(card.lines))
			assertRegions(t, card.targets, width, len(card.lines))
			if !hasAction(card.targets, "website.all") || !hasAction(card.targets, "website.manage") {
				t.Fatalf("width %d: missing card actions", width)
			}
			if strings.Count(view, "⚡︎") != 1 {
				t.Fatalf("width %d: expected one test-all icon", width)
			}
			var buttons []hitRegion
			for _, hit := range card.targets {
				if strings.HasPrefix(hit.ID, "website.site.") {
					buttons = append(buttons, hit)
				}
			}
			if len(buttons) != len(sites) {
				t.Fatalf("width %d: got %d website buttons", width, len(buttons))
			}
			for i, hit := range buttons {
				label := ansi.Strip(ansi.Cut(card.lines[hit.Y], hit.X, hit.X+hit.W))
				if hit.ID != websiteActionID(sites[i]) || width >= 24 && !strings.Contains(label, sites[i].Name) {
					t.Fatalf("width %d: %s button paints %q", width, hit.ID, label)
				}
			}
			a, b, c := buttons[0], buttons[1], buttons[2]
			switch width {
			case 72:
				if a.Y != b.Y || b.Y != c.Y || !(a.X < b.X && b.X < c.X) {
					t.Fatalf("wide card should fit all three sites in one row: %+v", buttons)
				}
			case 52:
				if a.Y != b.Y || c.Y <= a.Y || a.X >= b.X || c.X != a.X {
					t.Fatalf("medium card should flow into two columns: %+v", buttons)
				}
			case 24, 16:
				if !(a.Y < b.Y && b.Y < c.Y) || a.X != b.X || b.X != c.X {
					t.Fatalf("narrow card should stack sites: %+v", buttons)
				}
			}
			if width == 24 || width == 72 {
				statuses := [][]string{
					{o.Locale.T("dashboard.website.reachable"), "123 ms"},
					{o.Locale.T("dashboard.website.forbidden"), "37 ms"},
					{o.Locale.T("dashboard.website.timeout")},
				}
				for i, hit := range buttons {
					right, bottom := width, len(card.lines)
					if i+1 < len(buttons) {
						if next := buttons[i+1]; next.Y == hit.Y {
							right = next.X
						} else {
							bottom = next.Y
						}
					}
					var result strings.Builder
					for _, line := range card.lines[hit.Y+1 : bottom] {
						result.WriteString(strings.Join(strings.Fields(ansi.Strip(ansi.Cut(line, hit.X, right))), ""))
					}
					for _, label := range statuses[i] {
						if !strings.Contains(result.String(), strings.Join(strings.Fields(label), "")) {
							t.Fatalf("%s width %d: %s did not display %q under its button: %s", language, width, sites[i].Name, label, ansi.Strip(view))
						}
					}
				}
			}
		}

		o.websiteTests.rows[sites[0].URL] = websiteTestRow{err: errors.New("private diagnostic")}
		o.websiteTests.rows[sites[1].URL] = websiteTestRow{result: app.WebsiteTestResult{StatusCode: 503}}
		delete(o.websiteTests.rows, sites[2].URL)
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
		if len(card.targets) != 1 || !hasAction(card.targets, "website.manage") || strings.Contains(view, "HTTP 503") || strings.Count(view, o.Locale.T("dashboard.status.unavailable")) != 3 {
			t.Fatalf("stopped proxy retained website actions or results: %s", view)
		}
	}
}

func TestWebsiteCardScrollsFocusedTestIntoCompactViewport(t *testing.T) {
	for _, lang := range []string{i18n.English, i18n.Chinese} {
		m := themeSample("overview-running")
		m.locale, m.width, m.height, m.snapshot.MixedPort = i18n.New(lang), 40, 16, 7890
		m.websiteTests.rows = map[string]websiteTestRow{}
		for _, site := range app.DefaultWebsites() {
			m.websiteTests.rows[site.URL] = websiteTestRow{result: app.WebsiteTestResult{StatusCode: 403, Duration: 231 * time.Millisecond}}
		}
		m.focus = "overview.card.dashboard.website.title"
		m.revealContentFocus()
		m.layout()
		for _, id := range []string{"website.all", websiteActionID(app.DefaultWebsites()[0]), websiteActionID(app.DefaultWebsites()[1]), websiteActionID(app.DefaultWebsites()[2])} {
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
