package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
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
		o.websites, o.Focus, o.Hover = []app.Website{site}, "", overviewWebsiteDragID(site)
		cell := o.websiteCell(site, width)
		if len(cell.targets) != 2 || cell.targets[0].ID != overviewWebsiteDragID(site) || cell.targets[1].ID != websiteActionID(site) || cell.targets[1].Y != 1 ||
			strings.Contains(ansi.Strip(cell.lines[0]), "⠿") {
			t.Fatalf("width %d: a lone site name must have a separate context-menu target: %+v", width, cell)
		}
		o.websites = []app.Website{site, other}
		o.Focus, o.Hover = websiteActionID(site), ""
		cell = o.websiteCell(site, width)
		name, result := cell.targets[0], cell.targets[1]
		if name.ID != overviewWebsiteDragID(site) || result.ID != o.Focus || regionsOverlap(name, result) || strings.Contains(cell.lines[0], "⠿") ||
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

func TestWebsiteResultHoverPreservesSemanticColors(t *testing.T) {
	site := app.DefaultWebsites()[0]
	id := websiteActionID(site)
	for _, state := range []struct {
		label, color string
		row          websiteTestRow
		unavailable  bool
	}{
		{label: "测速", color: accent},
		{label: "173 ms", color: success, row: websiteTestRow{result: app.WebsiteTestResult{StatusCode: 200, Duration: 173 * time.Millisecond}}},
		{label: "超时", color: warning, row: websiteTestRow{err: context.DeadlineExceeded}},
		{label: "测试中", row: websiteTestRow{cancel: func() {}}},
		{label: "不可用", unavailable: true},
	} {
		for _, interaction := range []struct{ name, hover, focus string }{
			{"idle", "", ""}, {"hover", id, ""}, {"focus", "", id}, {"focus and hover", id, id},
		} {
			t.Run(state.label+"/"+interaction.name, func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(i18n.Chinese), Focus: interaction.focus, Hover: interaction.hover,
					Snapshot:         app.Snapshot{Installed: true, MixedPort: 7890, Service: service.Status{Active: true, State: "active"}},
					StateUnavailable: state.unavailable}
				o.websiteTests.rows = map[string]websiteTestRow{site.URL: state.row}
				line := o.websiteCell(site, 24).lines[1]
				plain := ansi.Strip(line)
				if strings.Contains(plain, "›") != (interaction.focus == id) || strings.Trim(plain, " ›") != state.label {
					t.Fatalf("wrong result text or focus marker: %q", plain)
				}
				background := panel
				if interaction.hover == id && !state.unavailable && !state.row.pending() {
					background = hoverBackground
				}
				screen := uv.NewScreenBuffer(ansi.StringWidth(plain), 1)
				uv.NewStyledString(line).Draw(screen, screen.Bounds())
				// Skip the separate focus marker and check the entire button face, including padding.
				for x := 1; x < screen.Width(); x++ {
					cell := screen.CellAt(x, 0)
					if cell.Content == "" {
						continue // Second terminal cell of a wide glyph.
					}
					expectThemeColor(t, "face background", cell.Style.Bg, background)
					if state.color != "" {
						expectThemeColor(t, "result foreground", cell.Style.Fg, state.color)
					}
					if cell.Style.Underline != uv.UnderlineNone {
						t.Fatalf("unexpected underline at column %d", x)
					}
				}
			})
		}
	}
}

func TestWebsiteNameHoverExtendsItsMouseTargetToTheGrip(t *testing.T) {
	sites := app.DefaultWebsites()
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ASCII} {
		o := dashboardOptions{websites: sites, Locale: i18n.New(i18n.Chinese), Monochrome: profile == colorprofile.ASCII}
		idle := o.websiteCell(sites[0], 24)
		name, result := idle.targets[0], idle.targets[1]
		o.Hover = name.ID
		hovered := o.websiteCell(sites[0], 24)
		hit := findTarget(t, Model{targets: hovered.targets}, name.ID)
		line := terminalTheme(hovered.lines[0], profile)
		if hit.X != name.X-2 || hit.X+hit.W != name.X+name.W || hit.Y != name.Y || hit.H != name.H ||
			ansi.Strip(ansi.Cut(line, hit.X, hit.X+hit.W)) != "⠿ "+sites[0].Name {
			t.Fatalf("%v: visible grip, space and name must share one mouse target: %+v", profile, hit)
		}
		if profile == colorprofile.TrueColor && !strings.Contains(line, themed("⠿", muted, panel, false)) {
			t.Fatal("the idle hover grip should use the muted color")
		}
		screen := uv.NewScreenBuffer(24, 2)
		uv.NewStyledString(terminalTheme(idle.lines[0], profile)+"\n"+line).Draw(screen, screen.Bounds())
		for x := name.X; x < name.X+name.W; x++ {
			if !screen.CellAt(x, 0).Equal(screen.CellAt(x, 1)) {
				t.Fatalf("%v: hovering changed the site name at column %d", profile, x)
			}
		}
		if len(hovered.targets) != len(idle.targets) || !slices.Contains(hovered.targets, result) ||
			!slices.Equal(hovered.overviewFocusRegions(), idle.overviewFocusRegions()) || hovered.lines[result.Y] != idle.lines[result.Y] {
			t.Fatal("hovering the name changed its keyboard stop or test result")
		}
		o.Busy = true
		busy := o.websiteCell(sites[0], 24)
		if strings.Contains(busy.lines[0], "⠿") || hasAction(busy.targets, name.ID) {
			t.Fatal("a busy card offered drag interaction")
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
			name := findTarget(t, Model{targets: neutral.targets}, overviewWebsiteDragID(site))
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

func TestWebsiteDragMarkersPointIntoTheTargetWithoutMovingControls(t *testing.T) {
	defaults := app.DefaultWebsites()
	sites := []app.Website{
		{Name: strings.Repeat("长站名", 10), URL: "https://long.example/"},
		{Name: "gpt", URL: "https://gpt.example/"}, defaults[2],
		defaults[0], defaults[1], {Name: "京东", URL: "https://jd.example/"},
	}
	for _, tc := range []struct {
		name            string
		width, from, to int
		direction       string
	}{
		{"pressed name", 72, 2, 2, ""},
		{"right toward GitHub", 52, 2, 3, "→"},
		{"left toward YouTube", 52, 3, 2, "←"},
		{"left toward clipped Chinese name", 52, 1, 0, "←"},
		{"down to next row", 72, 1, 4, "↓"},
		{"up to previous row", 72, 4, 1, "↑"},
		{"down and left from YouTube to Google under gpt", 72, 2, 4, "↙"},
		{"down and right to Google", 72, 0, 4, "↘"},
		{"up and left to clipped Chinese name", 72, 4, 0, "↖"},
		{"up and right to YouTube", 72, 4, 2, "↗"},
		{"up past a wrapped result", 10, 1, 0, "↑"},
		{"down onto first row of next page", 24, 2, 3, "↓"},
		{"up onto last row of previous page", 24, 3, 2, "↑"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := dashboardOptions{Locale: i18n.New(i18n.Chinese), websites: sites, websiteOffset: tc.to}
			idle := overviewWebsiteCard(o, tc.width)
			source, target := sites[tc.from], sites[tc.to]
			o.Hover = overviewWebsiteDragID(sites[1])
			o.websiteDrag = overviewWebsiteDragState{source: source, neighbor: target, direction: tc.direction}
			card := overviewWebsiteCard(o, tc.width)
			view := strings.Join(card.lines, "\n")
			assertCanvas(t, view, tc.width, len(idle.lines))
			assertRegions(t, card.targets, tc.width, len(card.lines))
			for _, hit := range idle.targets {
				wantHit := hit
				if hit.ID == overviewWebsiteDragID(source) {
					wantHit.X -= 2
					wantHit.W += 2
				}
				if !slices.Contains(card.targets, wantHit) {
					t.Fatalf("dragging misplaced an existing control: %+v", wantHit)
				}
				got := ansi.Strip(ansi.Cut(card.lines[hit.Y], hit.X, hit.X+hit.W))
				want := ansi.Strip(ansi.Cut(idle.lines[hit.Y], hit.X, hit.X+hit.W))
				if got != want {
					t.Fatalf("%s now displays %q, want %q", hit.ID, got, want)
				}
			}
			m := Model{targets: card.targets}
			wantGrips := 0
			if hasAction(card.targets, overviewWebsiteDragID(source)) {
				wantGrips = 1
				name := findTarget(t, m, overviewWebsiteDragID(source))
				if grip := ansi.Strip(ansi.Cut(card.lines[name.Y], name.X, name.X+2)); grip != "⠿ " {
					t.Fatalf("source grip = %q", grip)
				}
			}
			if strings.Count(view, "⠿") != wantGrips {
				t.Fatal("only the visible drag source should show a grip")
			}
			if tc.direction == "" {
				if strings.ContainsAny(view, "←→↑↓↖↗↙↘") || len(card.targets) != len(idle.targets) {
					t.Fatal("stationary press added an arrow or a drop target")
				}
				return
			}
			name := findTarget(t, m, overviewWebsiteDragID(target))
			dropID := overviewWebsiteDropPrefix + target.URL
			if hasAction(card.overviewFocusRegions(), dropID) || strings.Count(view, tc.direction) != 1 {
				t.Fatalf("expected one visible %s and no keyboard stop for it", tc.direction)
			}
			var arrow hitRegion
			for y, line := range card.lines {
				if before, _, ok := strings.Cut(ansi.Strip(line), tc.direction); ok {
					arrow = hitRegion{X: ansi.StringWidth(before), Y: y, W: 1, H: 1}
					break
				}
			}
			if !slices.ContainsFunc(card.targets, func(hit hitRegion) bool {
				return hit.ID == dropID && regionsOverlap(hit, arrow)
			}) {
				t.Fatalf("%s at %+v did not accept drops onto %s", tc.direction, arrow, target.Name)
			}
			wantY := name.Y
			switch tc.direction {
			case "↓", "↘", "↙":
				wantY = name.Y - 1
			case "↑", "↗", "↖":
				for _, result := range idle.targets {
					if result.ID == websiteActionID(target) {
						wantY = max(wantY, result.Y+result.H)
					}
				}
			}
			var minX, maxX int
			switch tc.direction {
			case "→":
				minX, maxX = name.X-2, name.X-2
			case "←":
				minX, maxX = name.X+name.W+1, name.X+name.W+1
			case "↘", "↗":
				minX, maxX = name.X-1, name.X-1
			case "↙", "↖":
				minX, maxX = name.X+name.W, name.X+name.W
			case "↓", "↑":
				minX, maxX = name.X, name.X+name.W-1
			}
			if arrow.Y != wantY || arrow.X < minX || arrow.X > maxX {
				t.Fatalf("%s at (%d,%d), want x between %d and %d on row %d facing %s",
					tc.direction, arrow.X, arrow.Y, minX, maxX, wantY, target.Name)
			}
		})
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
