package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func TestSiteDragPreviewsAndSavesOnceOnDrop(t *testing.T) {
	sites := app.DefaultWebsites()
	for _, pair := range [][2]int{{0, 2}, {2, 0}} {
		from, to := pair[0], pair[1]
		t.Run(fmt.Sprintf("%d to %d", from, to), func(t *testing.T) {
			calls := 0
			want := slices.Clone(sites)
			want = slices.Insert(slices.Delete(want, from, from+1), to, sites[from])
			b := &fakeBackend{snapshot: settingsTestSnapshot(), relocateWebsite: func(_ context.Context, site, neighbor app.Website, after bool) ([]app.Website, error) {
				calls++
				if site != sites[from] || neighbor != sites[to] || after != (from < to) {
					t.Fatalf("wrong drop: %v, %v, after=%v", site, neighbor, after)
				}
				return want, nil
			}}
			m := ready(b)
			directionCleanup(t, &m)
			m.activate("website.manage")
			m.layout()
			m, _ = click(m, findTarget(t, m, siteDragGrip+sites[from].URL))
			dest := findTarget(t, m, siteManagerRowID+sites[to].URL)
			p := tea.Mouse{X: dest.X + 2, Y: dest.Y, Button: tea.MouseLeft}
			m, _ = update(m, tea.MouseMotionMsg(p))
			if m.siteManager.drag.neighbor != sites[to] || !slices.Equal(m.websites, sites) || calls != 0 {
				t.Fatal("drag did not preview the destination independently of persistence")
			}
			screen, _ := m.renderSiteManager("")
			marker := "↑"
			if from < to {
				marker = "↓"
			}
			line := strings.Split(ansi.Strip(screen), "\n")[dest.Y]
			if ansi.Cut(line, dest.X, dest.X+1) != marker {
				t.Fatalf("missing %q on destination row: %q", marker, line)
			}
			m, cmd := update(m, tea.MouseReleaseMsg(p))
			if cmd == nil || !m.busy || m.siteManager.drag.active() {
				t.Fatal("dropping a website should start one save and finish its gesture")
			}
			m, _ = update(m, cmd())
			m, _ = update(m, tea.MouseReleaseMsg(p))
			if calls != 1 || !slices.Equal(m.websites, want) || m.siteManager.selected != to || m.focus != "websiteManager.list" {
				t.Fatalf("unexpected saved order or selection: calls=%d, websites=%v, selected=%d", calls, m.websites, m.siteManager.selected)
			}
		})
	}
}

func TestSiteDragCancelsWithoutSaving(t *testing.T) {
	for _, reason := range []string{"same row", "outside", "escape", "resize", "new press", "hover without button"} {
		t.Run(reason, func(t *testing.T) {
			m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
			directionCleanup(t, &m)
			m.activate("website.manage")
			m.layout()
			before := slices.Clone(m.websites)
			grip := findTarget(t, m, siteDragGrip+m.websites[0].URL)
			dest := findTarget(t, m, siteManagerRowID+m.websites[1].URL)
			m, _ = click(m, grip)
			drop := tea.MouseReleaseMsg{X: dest.X, Y: dest.Y, Button: tea.MouseLeft}
			switch reason {
			case "same row":
				drop.X, drop.Y = grip.X, grip.Y
			case "outside":
				drop.X, drop.Y = 0, 0
			case "escape":
				m, _ = update(m, special(tea.KeyEscape))
			case "resize":
				m, _ = update(m, tea.WindowSizeMsg{Width: m.width - 1, Height: m.height})
			case "new press":
				m, _ = click(m, dest)
				if m.siteManager.selected != 1 {
					t.Fatal("independent row click after a lost release should select normally")
				}
			case "hover without button":
				m, _ = update(m, tea.MouseMotionMsg{X: dest.X, Y: dest.Y, Button: tea.MouseNone})
			}
			m, _ = update(m, drop)
			if m.form != "website-manager" || m.siteManager.drag.active() || m.busy || !slices.Equal(m.websites, before) {
				t.Fatal("canceled drag moved a website, left a save pending or closed the manager")
			}
		})
	}
}

func TestSiteDragScrollsToUnseenRowsAndDiscardsOldTicks(t *testing.T) {
	m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
	directionCleanup(t, &m)
	var sites []app.Website
	for i := range 30 {
		sites = append(sites, app.Website{Name: fmt.Sprintf("Site %d", i), URL: fmt.Sprintf("https://s%d.test/", i)})
	}
	m.setWebsites(sites)
	m.activate("website.manage")
	m.layout()
	m, _ = click(m, findTarget(t, m, siteDragGrip+sites[0].URL))
	edge := findTarget(t, m, siteManagerRowID+sites[11].URL)
	p := tea.MouseMotionMsg{X: edge.X + 2, Y: edge.Y, Button: tea.MouseLeft}
	m, cmd := update(m, p)
	tick := m.siteManager.drag.tick
	if cmd == nil || tick == nil {
		t.Fatal("dragging to the viewport edge did not start scrolling")
	}
	for step := 0; step < 4; step++ {
		m, _ = update(m, m.siteManager.drag.tick)
		target := findTarget(t, m, siteManagerRowID+sites[12+step].URL)
		if m.siteManager.offset != step+1 || target.Y != p.Y || m.siteManager.drag.neighbor != sites[12+step] {
			t.Fatal("edge scroll lost its target under the stationary pointer")
		}
	}
	// Stop at the opposing edge and verify upward scrolling as well.
	top := findTarget(t, m, siteManagerRowID+sites[4].URL)
	p.Y = top.Y
	m, _ = update(m, p)
	up := m.siteManager.drag.tick
	if up == nil || up.step != -1 {
		t.Fatal("moving to the top edge did not reverse scrolling")
	}
	m, _ = update(m, up)
	if m.siteManager.offset != 3 || m.siteManager.drag.neighbor != sites[3] {
		t.Fatal("top edge failed to scroll up")
	}
	m, _ = update(m, special(tea.KeyEscape))
	m, _ = update(m, tick)
	if m.siteManager.drag.active() || m.siteManager.offset != 0 || m.busy {
		t.Fatal("stale scroll did not leave the canceled drag and selection alone")
	}
}
