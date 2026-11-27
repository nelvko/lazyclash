package tui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func overviewWebsiteDragModel(t *testing.T, count int) (Model, *fakeBackend) {
	t.Helper()
	sites := app.DefaultWebsites()
	if count < len(sites) {
		sites = sites[:count]
	}
	for i := len(sites); i < count; i++ {
		sites = append(sites, app.Website{Name: fmt.Sprintf("Site %d", i), URL: fmt.Sprintf("https://%d.example/", i)})
	}
	b := &fakeBackend{prefs: app.Preferences{Websites: sites}, snapshot: newOverviewPTYBackend("running").snapshot}
	m := New(t.Context(), b, "en")
	m.busy = false
	m.adoptSnapshot(b.snapshot)
	m.width, m.height, m.focus = 160, 48, "website.all"
	m.layout()
	return m, b
}

func TestOverviewWebsiteSingleNameFocusesStoppedSite(t *testing.T) {
	m, _ := overviewWebsiteDragModel(t, 1)
	m.snapshot.Service.Active, m.snapshot.Service.State = false, "inactive"
	m.layout()
	site := m.websites[0]
	name := websitePointer(t, m, overviewWebsiteDragID(site))
	m, _ = update(m, tea.MouseClickMsg(name))
	m, _ = update(m, tea.MouseReleaseMsg(name))
	if m.focus != websiteActionID(site) || m.websiteDrag.active() || m.busy || len(m.websiteTests.running) != 0 {
		t.Fatal("a single stopped website should receive management focus without starting a drag or probe")
	}
}

func websitePointer(t *testing.T, m Model, id string) tea.Mouse {
	t.Helper()
	hit := findTarget(t, m, id)
	return tea.Mouse{X: hit.X + hit.W/2, Y: hit.Y, Button: tea.MouseLeft}
}

func websiteArrowPointer(t *testing.T, m Model, site app.Website, arrow string) tea.Mouse {
	t.Helper()
	hit := findTarget(t, m, overviewWebsiteDropPrefix+site.URL)
	lines := strings.Split(m.screen, "\n")
	for y := hit.Y; y < hit.Y+hit.H; y++ {
		face := ansi.Strip(ansi.Cut(lines[y], hit.X, hit.X+hit.W))
		if prefix, _, ok := strings.Cut(face, arrow); ok {
			return tea.Mouse{X: hit.X + ansi.StringWidth(prefix), Y: y, Button: tea.MouseLeft}
		}
	}
	t.Fatalf("%s is missing from the drop target for %s", arrow, site.Name)
	return tea.Mouse{}
}

func TestOverviewWebsiteHoverGripAcceptsImmediateDrag(t *testing.T) {
	m, _ := overviewWebsiteDragModel(t, 3)
	site := m.websites[0]
	nameID := overviewWebsiteDragID(site)
	name := findTarget(t, m, nameID)
	text := tea.Mouse{X: name.X, Y: name.Y, Button: tea.MouseLeft}
	grip := tea.Mouse{X: name.X - 2, Y: name.Y, Button: tea.MouseLeft}
	hover := func(p tea.Mouse) {
		m, _ = update(m, tea.MouseMotionMsg{X: p.X, Y: p.Y})
	}
	hasGrip := func() bool { return strings.Contains(ansi.Strip(m.screen), "⠿") }

	hover(text)
	if m.hover != nameID || !hasGrip() {
		t.Fatal("hovering the name should immediately reveal its grip")
	}
	for _, start := range []tea.Mouse{text, grip} {
		hover(start)
		if m.hover != nameID || !hasGrip() {
			t.Fatal("moving between the name and its grip cleared the hover")
		}
		var cmd tea.Cmd
		m, cmd = update(m, tea.MouseClickMsg(start))
		if cmd != nil || m.websiteDrag.source != site || !hasGrip() {
			t.Fatal("pressing the name or grip should immediately begin the same drag")
		}
		m, _ = update(m, tea.MouseReleaseMsg(start))
		if m.websiteDrag.active() || m.busy || m.hover != nameID || !hasGrip() || len(m.websiteTests.running) > 0 {
			t.Fatal("a stationary release must preserve hover without sorting or testing")
		}
	}
	result := websitePointer(t, m, websiteActionID(site))
	hover(result)
	if m.hover != websiteActionID(site) || hasGrip() {
		t.Fatal("hovering the result should hide the grip")
	}
	hover(text)
	m, _ = update(m, tea.MouseClickMsg(grip))
	m, _ = update(m, tea.MouseReleaseMsg(result))
	if m.hover != websiteActionID(site) || hasGrip() || len(m.websiteTests.running) > 0 {
		t.Fatal("releasing on the same site's result should hover it without testing")
	}
	hover(text)
	m, _ = update(m, tea.MouseClickMsg(grip))
	m, _ = update(m, tea.MouseMotionMsg(websitePointer(t, m, overviewWebsiteDragID(m.websites[1]))))
	if m.websiteDrag.source != site || m.websiteDrag.direction != "→" || len(m.websiteTests.running) > 0 {
		t.Fatal("the grip failed to drag immediately onto a neighboring site")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.websiteDrag.active() || hasGrip() {
		t.Fatal("cancelling over another site left the source grip visible")
	}
	hover(text)
	m, _ = update(m, tea.MouseClickMsg(grip))
	hover(tea.Mouse{})
	if m.websiteDrag.active() || hasGrip() {
		t.Fatal("a missing mouse release left a grip painted after moving away")
	}
	for _, msg := range []tea.Msg{tea.BlurMsg{}, tea.WindowSizeMsg{Width: m.width - 1, Height: m.height}} {
		hover(websitePointer(t, m, nameID))
		m, _ = update(m, msg)
		if hasGrip() || m.hover == nameID {
			t.Fatal("an invalidated screen kept the hovered source grip")
		}
	}
}

func TestOverviewWebsiteDragDropsOnNameOrResultWithoutTesting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to int
		targetID func(app.Website) string
		arrow    string
	}{
		{"right onto name", 0, 2, overviewWebsiteDragID, "→"},
		{"left onto name", 2, 1, overviewWebsiteDragID, "←"},
		{"up onto result", 3, 0, websiteActionID, "↑"},
		{"down onto result", 2, 5, websiteActionID, "↓"},
		{"up-left onto name", 4, 0, overviewWebsiteDragID, "↖"},
		{"up-right onto result", 3, 1, websiteActionID, "↗"},
		{"down-right onto name", 0, 4, overviewWebsiteDragID, "↘"},
		{"YouTube down-left onto Google", 2, 4, websiteActionID, "↙"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, b := overviewWebsiteDragModel(t, 6)
			original := slices.Clone(m.websites)
			// Place Google diagonally below and to the left of YouTube.
			original[1], original[4] = original[4], original[1]
			m.setWebsites(original)
			source, target := original[tc.from], original[tc.to]
			before := app.WebsiteTestResult{StatusCode: 200, Duration: 37 * time.Millisecond}
			m.websiteTests.rows = map[string]websiteTestRow{source.URL: {result: before}}
			m.layout()
			saves := 0
			want := slices.Clone(original)
			want = slices.Insert(slices.Delete(want, tc.from, tc.from+1), tc.to, source)
			b.relocateWebsite = func(_ context.Context, site, neighbor app.Website, after bool) ([]app.Website, error) {
				saves++
				if site != source || neighbor != target || after != (tc.to > tc.from) {
					t.Fatalf("unexpected move: %v to %v, after=%v", site, neighbor, after)
				}
				return want, nil
			}
			m, _ = update(m, tea.MouseClickMsg(websitePointer(t, m, overviewWebsiteDragID(source))))
			if m.focus != websiteActionID(source) || m.websiteDrag.source != source {
				t.Fatalf("name press must focus the result and begin sorting: focus=%q, drag=%+v", m.focus, m.websiteDrag)
			}
			p := websitePointer(t, m, tc.targetID(target))
			m, _ = update(m, tea.MouseMotionMsg(p))
			if m.websiteDrag.neighbor != target || m.websiteDrag.after != (tc.to > tc.from) ||
				m.websiteDrag.direction != tc.arrow || m.busy || !slices.Equal(m.websites, original) {
				t.Fatal("moving the pointer should only preview the new position")
			}
			// Walk one terminal cell at a time from the name to the arrow.
			// Losing the preview in a gap would make the arrow unreachable.
			arrow := websiteArrowPointer(t, m, target, tc.arrow)
			name := findTarget(t, m, overviewWebsiteDragID(target))
			p.X, p.Y = max(name.X, min(arrow.X, name.X+name.W-1)), name.Y
			for {
				m, _ = update(m, tea.MouseMotionMsg(p))
				if m.websiteDrag.direction != tc.arrow {
					t.Fatalf("the arrow disappeared approaching it at %d,%d", p.X, p.Y)
				}
				if p.X == arrow.X && p.Y == arrow.Y {
					break
				}
				p.X += cmp.Compare(arrow.X, p.X)
				p.Y += cmp.Compare(arrow.Y, p.Y)
			}
			m, cmd := update(m, tea.MouseReleaseMsg(p))
			if cmd == nil || !m.busy || m.websiteDrag.active() {
				t.Fatal("drop did not finish the gesture and start saving")
			}
			m, _ = update(m, cmd())
			if strings.Contains(ansi.Strip(m.screen), "⠿") {
				t.Fatal("saving a reordered grid left the old source grip visible")
			}
			m, _ = update(m, tea.MouseReleaseMsg(p))
			if saves != 1 || m.form != "" || !slices.Equal(m.websites, want) || m.focus != websiteActionID(source) ||
				m.websiteTests.rows[source.URL].result != before || len(m.websiteTests.running) != 0 {
				t.Fatal("drop lost order, overview focus or measurement, or started an extra operation")
			}
		})
	}
}

func TestOverviewWebsiteDragCancellationKeepsMeasurementIdle(t *testing.T) {
	for _, reason := range []string{"name click", "outside", "unmarked gutter", "escape", "resize", "blur", "open menu", "lost button", "new name press", "new arrow press"} {
		t.Run(reason, func(t *testing.T) {
			m, _ := overviewWebsiteDragModel(t, 3)
			original := slices.Clone(m.websites)
			name := websitePointer(t, m, overviewWebsiteDragID(original[0]))
			drop := websitePointer(t, m, websiteActionID(original[1]))
			m, _ = update(m, tea.MouseClickMsg(name))
			switch reason {
			case "name click":
				drop = name
			case "outside":
				drop.X, drop.Y = 0, 0
			case "unmarked gutter":
				name := findTarget(t, m, overviewWebsiteDragID(original[1]))
				drop.X, drop.Y = name.X-2, name.Y
			case "escape":
				m, _ = update(m, special(tea.KeyEscape))
			case "resize":
				m, _ = update(m, tea.WindowSizeMsg{Width: m.width - 1, Height: m.height})
			case "blur":
				m, _ = update(m, tea.BlurMsg{})
			case "open menu":
				name.Button = tea.MouseRight
				m, _ = update(m, tea.MouseClickMsg(name))
				if !m.menu.open {
					t.Fatal("right clicking a site should open its menu")
				}
				m, _ = update(m, special(tea.KeyEscape))
			case "lost button":
				m, _ = update(m, tea.MouseMotionMsg{X: drop.X, Y: drop.Y})
			case "new name press":
				drop = websitePointer(t, m, overviewWebsiteDragID(original[1]))
				m, _ = update(m, tea.MouseClickMsg(drop))
			case "new arrow press":
				m, _ = update(m, tea.MouseMotionMsg(drop))
				drop = websitePointer(t, m, overviewWebsiteDropPrefix+original[1].URL)
				m, _ = update(m, tea.MouseClickMsg(drop))
				if m.websiteDrag.active() || m.focus != websiteActionID(original[0]) {
					t.Fatal("pressing a transient arrow should cancel the drag and keep focus on its source")
				}
			}
			m, _ = update(m, tea.MouseReleaseMsg(drop))
			if m.websiteDrag.active() || m.websiteDrag.direction != "" || m.busy ||
				!slices.Equal(m.websites, original) || len(m.websiteTests.running) != 0 {
				t.Fatal("cancelling or clicking a name issued a probe or move")
			}
			if reason == "name click" && m.focus != websiteActionID(original[0]) {
				t.Fatal("a stationary name click did not focus its result")
			}
			m, _ = update(m, tea.MouseClickMsg(websitePointer(t, m, websiteActionID(original[1]))))
			if !m.websiteTests.rows[original[1].URL].pending() || len(m.websiteTests.running) != 1 {
				t.Fatal("an independent result click should issue exactly one test")
			}
			m.websiteTests.clear()
		})
	}
}

func TestOverviewWebsiteDragPagesOnlyOnEntry(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancel), func(t *testing.T) {
			m, b := overviewWebsiteDragModel(t, 13)
			sites := slices.Clone(m.websites)
			source := sites[0]
			m, _ = update(m, tea.MouseClickMsg(websitePointer(t, m, overviewWebsiteDragID(source))))
			turn := websitePointer(t, m, "website.next")
			m, _ = update(m, tea.MouseMotionMsg(turn))
			m, _ = update(m, tea.MouseMotionMsg(turn))
			if m.websiteOffset != 6 || m.websiteDrag.neighbor.URL != "" || m.websiteDrag.direction != "" {
				t.Fatalf("hovering on Next should advance exactly one page: offset=%d", m.websiteOffset)
			}
			m, _ = update(m, tea.MouseMotionMsg(websitePointer(t, m, websiteActionID(sites[7]))))
			if m.websiteDrag.direction != "↓" {
				t.Fatal("a drop on a later page should point down even in a different column")
			}
			turn = websitePointer(t, m, "website.next")
			m, _ = update(m, tea.MouseMotionMsg(turn))
			if m.websiteOffset != 12 {
				t.Fatal("reentering Next did not advance to the final page")
			}
			if cancel {
				m, _ = update(m, tea.MouseReleaseMsg(turn))
				if m.websiteDrag.active() || m.busy || !slices.Equal(m.websites, sites) || len(m.websiteTests.running) != 0 {
					t.Fatal("releasing over the original page button must cancel")
				}
				return
			}
			target := sites[12]
			b.relocateWebsite = func(_ context.Context, s, neighbor app.Website, after bool) ([]app.Website, error) {
				if s != source || neighbor != target || !after {
					t.Fatal("drag lost its source or relative position across pages")
				}
				return append(slices.Clone(sites[1:]), source), nil
			}
			drop := websitePointer(t, m, overviewWebsiteDragID(target))
			m, cmd := update(m, tea.MouseReleaseMsg(drop))
			if cmd == nil {
				t.Fatal("cross-page drop did not save")
			}
			m, _ = update(m, cmd())
			if m.websites[12] != source || !hasAction(m.targets, websiteActionID(source)) || m.focus != websiteActionID(source) {
				t.Fatal("moved site lost its page or focus")
			}
		})
	}
}

func TestOverviewWebsiteDraggingToEarlierPagePointsUp(t *testing.T) {
	m, _ := overviewWebsiteDragModel(t, 8)
	m.turnWebsitePage("website.next")
	m.layout()
	source, target := m.websites[7], m.websites[2]
	m, _ = update(m, tea.MouseClickMsg(websitePointer(t, m, overviewWebsiteDragID(source))))
	m, _ = update(m, tea.MouseMotionMsg(websitePointer(t, m, "website.prev")))
	m, _ = update(m, tea.MouseMotionMsg(websitePointer(t, m, overviewWebsiteDragID(target))))
	if m.websiteDrag.neighbor != target || m.websiteDrag.direction != "↑" {
		t.Fatal("dragging to an earlier page should point up even when the target is to the right")
	}
}
