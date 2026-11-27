package tui

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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

func TestOverviewWebsiteDragDropsOnNameOrResultWithoutTesting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to int
		targetID func(app.Website) string
	}{
		{"horizontal onto name", 0, 2, overviewWebsiteDragID},
		{"across rows onto result", 3, 1, websiteActionID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, b := overviewWebsiteDragModel(t, 4)
			original := slices.Clone(m.websites)
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
			if m.websiteDrag.neighbor != target || m.websiteDrag.after != (tc.to > tc.from) || m.busy || !slices.Equal(m.websites, original) {
				t.Fatal("moving the pointer should only preview the new position")
			}
			m, cmd := update(m, tea.MouseReleaseMsg(p))
			if cmd == nil || !m.busy || m.websiteDrag.active() {
				t.Fatal("drop did not finish the gesture and start saving")
			}
			m, _ = update(m, cmd())
			m, _ = update(m, tea.MouseReleaseMsg(p))
			if saves != 1 || m.form != "" || !slices.Equal(m.websites, want) || m.focus != websiteActionID(source) ||
				m.websiteTests.rows[source.URL].result != before || len(m.websiteTests.running) != 0 {
				t.Fatal("drop lost order, overview focus or measurement, or started an extra operation")
			}
		})
	}
}

func TestOverviewWebsiteDragCancellationKeepsMeasurementIdle(t *testing.T) {
	for _, reason := range []string{"name click", "outside", "escape", "resize", "lost button", "new name press"} {
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
			case "escape":
				m, _ = update(m, special(tea.KeyEscape))
			case "resize":
				m, _ = update(m, tea.WindowSizeMsg{Width: m.width - 1, Height: m.height})
			case "lost button":
				m, _ = update(m, tea.MouseMotionMsg{X: drop.X, Y: drop.Y})
			case "new name press":
				drop = websitePointer(t, m, overviewWebsiteDragID(original[1]))
				m, _ = update(m, tea.MouseClickMsg(drop))
			}
			m, _ = update(m, tea.MouseReleaseMsg(drop))
			if m.websiteDrag.active() || m.busy || !slices.Equal(m.websites, original) || len(m.websiteTests.running) != 0 {
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
			if m.websiteOffset != 6 || m.websiteDrag.neighbor.URL != "" {
				t.Fatalf("hovering on Next should advance exactly one page: offset=%d", m.websiteOffset)
			}
			m, _ = update(m, tea.MouseMotionMsg(websitePointer(t, m, websiteActionID(sites[6]))))
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
