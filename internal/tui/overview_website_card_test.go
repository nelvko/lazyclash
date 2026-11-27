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
			if !hasAction(card.targets, "website.all") || !hasAction(card.targets, "website.add") {
				t.Fatalf("width %d: missing card actions", width)
			}
			if strings.Count(view, "⚡︎") != 1 || strings.Count(view, "＋") != 1 || strings.Contains(view, "⠿") {
				t.Fatalf("width %d: incorrect header or site-name glyphs", width)
			}
			var add, all hitRegion
			for _, hit := range card.targets {
				switch hit.ID {
				case "website.add":
					add = hit
				case "website.all":
					all = hit
				}
			}
			if width >= 24 && (add.Y != all.Y || add.X+add.W != all.X || all.X+all.W != width-2) {
				t.Fatalf("width %d: add and test all are not adjacent at the right edge of the header", width)
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
				site := sites[i]
				name := findTarget(t, Model{targets: card.targets}, overviewWebsiteDragID(site))
				label := ansi.Strip(ansi.Cut(card.lines[name.Y], name.X, name.X+name.W))
				if hit.ID != websiteActionID(site) || hit.X+2 != name.X || hit.Y != name.Y+1 ||
					label != site.Name || hasAction(card.overviewFocusRegions(), name.ID) {
					t.Fatalf("width %d: draggable name %q at %+v, test action %+v", width, label, name, hit)
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
				for i, want := range []string{"123 ms", "37 ms", o.Locale.T("dashboard.website.timeout")} {
					hit := buttons[i]
					right := width
					if next := i + 1; next < len(buttons) && buttons[next].Y == hit.Y {
						right = buttons[next].X
					}
					got := strings.TrimSpace(ansi.Strip(ansi.Cut(card.lines[hit.Y], hit.X, right)))
					if got != want {
						t.Fatalf("%s width %d: %s displays %q, want %q", language, width, sites[i].Name, got, want)
					}
				}
			}
		}

		o.websiteTests.rows[sites[0].URL] = websiteTestRow{err: errors.New("private diagnostic")}
		o.websiteTests.rows[sites[1].URL] = websiteTestRow{result: app.WebsiteTestResult{StatusCode: 503, Duration: 260 * time.Microsecond}}
		delete(o.websiteTests.rows, sites[2].URL)
		card := overviewWebsiteCard(o, 70)
		view := ansi.Strip(strings.Join(card.lines, "\n"))
		for _, want := range []string{o.Locale.T("dashboard.website.timeout"), "1 ms", o.Locale.T("dashboard.website.test")} {
			if !strings.Contains(view, want) {
				t.Fatalf("missing %s in %s", want, view)
			}
		}
		if strings.Contains(view, "private diagnostic") || strings.Contains(view, "503") {
			t.Fatal("website row exposed a diagnostic or HTTP status")
		}
		o.websiteTests.rows[sites[0].URL] = websiteTestRow{result: app.WebsiteTestResult{StatusCode: 302, Duration: 8 * time.Millisecond}}
		if got, _ := o.websiteTestStatus(sites[0].URL); got != "8 ms" {
			t.Fatalf("redirect displayed %q", got)
		}
		o.Snapshot.Service.Active = false
		card = overviewWebsiteCard(o, 70)
		view = ansi.Strip(strings.Join(card.lines, "\n"))
		if len(card.targets) != 1+2*len(sites) || !hasAction(card.targets, "website.add") || strings.Contains(view, " ms") || strings.Count(view, o.Locale.T("dashboard.status.unavailable")) != 3 {
			t.Fatalf("stopped proxy showed the wrong results or actions: %s", view)
		}
		for _, site := range sites {
			if !hasAction(card.targets, overviewWebsiteDragID(site)) || !hasAction(card.targets, websiteActionID(site)) {
				t.Fatalf("stopped proxy lost the name or menu keyboard stop for %v", site)
			}
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
		for _, id := range []string{"website.add", "website.all", websiteActionID(app.DefaultWebsites()[0]), websiteActionID(app.DefaultWebsites()[1]), websiteActionID(app.DefaultWebsites()[2])} {
			k := special(tea.KeyTab)
			if id == "website.add" {
				k = special(tea.KeyEnter)
			}
			m, _ = update(m, k)
			if m.focus != id || !hasAction(m.targets, id) {
				t.Fatalf("%s: expected visible focus on %s, got %s", lang, id, m.focus)
			}
		}
	}
}

func TestWebsiteResultIsOnlyKeyboardActionAndNeverOverlapsName(t *testing.T) {
	site := app.Website{Name: "X", URL: "https://example.org/"}
	other := app.Website{Name: "Y", URL: "https://other.example/"}
	o := dashboardOptions{Locale: i18n.New(i18n.English), Snapshot: app.Snapshot{
		Installed: true, MixedPort: 7890, Service: service.Status{Active: true, State: "active"},
	}}
	for _, width := range []int{6, 24} {
		o.websites, o.Focus, o.Hover = []app.Website{site}, "", ""
		cell := o.websiteCell(site, width)
		if len(cell.targets) != 2 || cell.targets[0].ID != overviewWebsiteDragID(site) || cell.targets[1].ID != websiteActionID(site) || cell.targets[1].Y != 1 ||
			strings.Contains(ansi.Strip(cell.lines[0]), "⠿") {
			t.Fatalf("width %d: a lone site name must have a separate context-menu target: %+v", width, cell)
		}
		o.websites = []app.Website{site, other}
		o.Focus = websiteActionID(site)
		cell = o.websiteCell(site, width)
		name, result := cell.targets[0], cell.targets[1]
		if name.ID != overviewWebsiteDragID(site) || result.ID != o.Focus || regionsOverlap(name, result) ||
			!strings.Contains(ansi.Strip(cell.lines[result.Y]), "› Test") {
			t.Fatalf("width %d: name and focused test faces mismatch: %+v", width, cell)
		}
		if stops := cell.overviewFocusRegions(); len(stops) != 1 || stops[0].ID != o.Focus {
			t.Fatalf("width %d: website had extra keyboard stops: %+v", width, stops)
		}
		if width == 24 && (name.W >= width || result.W >= width) {
			t.Fatal("unused cell space was made interactive")
		}
		o.Focus, o.Hover = "", result.ID
		if hovered := o.websiteCell(site, width); hovered.lines[0] != cell.lines[0] ||
			hovered.lines[result.Y] == cell.lines[result.Y] || strings.Contains(ansi.Strip(hovered.lines[result.Y]), "›") {
			t.Fatal("hovering a result changed the name or imitated keyboard focus")
		}
	}
}

func TestWebsiteDragPreviewAndDropTargetsIncludeUnavailableResults(t *testing.T) {
	sites := app.DefaultWebsites()
	o := dashboardOptions{Locale: i18n.New(i18n.English), websites: sites}
	neutral := overviewWebsiteCard(o, 48)
	o.websiteDrag.source, o.websiteDrag.neighbor = sites[0], sites[1]
	for _, after := range []bool{false, true} {
		o.websiteDrag.after = after
		card := overviewWebsiteCard(o, 48)
		for i, site := range sites {
			name := findTarget(t, Model{targets: card.targets}, overviewWebsiteDragID(site))
			result := findTarget(t, Model{targets: card.targets}, websiteActionID(site))
			got := ansi.Strip(ansi.Cut(card.lines[name.Y], name.X, name.X+name.W))
			if got != site.Name || result.Y != name.Y+1 || !strings.Contains(ansi.Strip(card.lines[result.Y]), o.Locale.T("dashboard.status.unavailable")) {
				t.Fatalf("name or disabled drop row misplaced: %q, %+v", got, result)
			}
			plainName := ansi.Cut(neutral.lines[name.Y], name.X, name.X+name.W)
			dragName := ansi.Cut(card.lines[name.Y], name.X, name.X+name.W)
			if (plainName != dragName) != (i < 2) {
				t.Fatalf("incorrect drag emphasis for %s", site.Name)
			}
		}
	}
}

func TestWebsiteNamesAndResultsShowTheirOwnFooterActions(t *testing.T) {
	for _, lang := range []string{i18n.English, i18n.Chinese} {
		m := themeSample("overview-running")
		m.locale, m.width, m.height, m.snapshot.MixedPort = i18n.New(lang), 160, 60, 7890
		site, neighbor := m.websites[0], m.websites[1]
		m.focus = websiteActionID(site)
		m.layout()
		check := func(terms ...string) {
			t.Helper()
			footer := strings.Split(ansi.Strip(m.screen), "\n")[m.height-1]
			for _, s := range terms {
				if !strings.Contains(footer, s) {
					t.Fatalf("%s: missing %q in %q", lang, s, footer)
				}
			}
			if strings.Contains(footer, site.URL) {
				t.Fatal("footer printed the target address")
			}
		}
		check("Enter "+m.tr("dashboard.website.testSite", site.Name), m.tr("dashboard.website.menuKey"))
		name := findTarget(t, m, overviewWebsiteDragID(site))
		m, _ = update(m, tea.MouseMotionMsg{X: name.X, Y: name.Y})
		check(m.tr("dashboard.website.dragName"), m.tr("dashboard.website.contextMenu"))
		result := findTarget(t, m, websiteActionID(site))
		m, _ = update(m, tea.MouseMotionMsg{X: result.X + 2, Y: result.Y})
		check(m.tr("dashboard.website.clickTest", site.Name), m.tr("dashboard.website.menuKey"))
		m.snapshot.Service.Active = false
		m.layout()
		check(m.tr("dashboard.website.menuKey"))
		if strings.Contains(m.contextHelp(), m.tr("dashboard.website.testSite", site.Name)) || m.activate(websiteActionID(site)) != nil {
			t.Fatal("inactive proxy offered a website test")
		}
		m.websiteDrag.source = site
		m.hover, m.focus = "website.next", "website.next"
		m.layout()
		check(strings.Split(m.tr("dashboard.website.dragging"), " · ")...)
		m.websiteDrag.neighbor, m.websiteDrag.after = neighbor, true
		m.layout()
		check(strings.Split(m.tr("dashboard.website.dropAfter", neighbor.Name), " · ")...)
	}
}
