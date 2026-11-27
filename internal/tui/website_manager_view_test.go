package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestSiteManagerArrowsShowSelectedSiteInFooter(t *testing.T) {
	for _, tc := range []struct{ language, up, down string }{
		{i18n.English, "Move Google up", "Move Google down"},
		{i18n.Chinese, "上移 Google", "下移 Google"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
			directionCleanup(t, &m)
			m.locale = i18n.New(tc.language)
			m, _ = update(m, tea.WindowSizeMsg{Width: 100, Height: 24})
			m.activate("website.manage")
			m.layout()
			m, _ = update(m, special(tea.KeyDown))
			up, down := findTarget(t, m, "websiteManager.up"), findTarget(t, m, "websiteManager.down")
			line := strings.Split(ansi.Strip(m.screen), "\n")[up.Y]
			if up.Y != down.Y || down.X != up.X+up.W || !strings.Contains(ansi.Cut(line, up.X, up.X+up.W), "↑") ||
				!strings.Contains(ansi.Cut(line, down.X, down.X+down.W), "↓") {
				t.Fatalf("adjacent arrow controls missing from %q", line)
			}

			for range m.siteManagerButtons() {
				m, _ = update(m, special(tea.KeyTab))
				if m.focus == up.ID {
					break
				}
			}
			footer := func() string { return strings.Split(ansi.Strip(m.screen), "\n")[m.height-1] }
			if m.focus != up.ID || !strings.Contains(footer(), tc.up) {
				t.Fatalf("focus %q has footer %q", m.focus, footer())
			}
			m, _ = update(m, tea.MouseMotionMsg{X: down.X + down.W/2, Y: down.Y})
			if m.hover != down.ID || !strings.Contains(footer(), tc.down) {
				t.Fatalf("hover %q has footer %q", m.hover, footer())
			}
			m.selectManagerSite(0)
			m.focus, m.hover = up.ID, ""
			if m.contextHelp() != m.tr("websiteManager.keys") {
				t.Fatalf("disabled up button advertised a move: %s", m.contextHelp())
			}
			m.selectManagerSite(len(m.websites) - 1)
			m.hover = down.ID
			if m.contextHelp() != m.tr("websiteManager.keys") {
				t.Fatalf("disabled down button advertised a move: %s", m.contextHelp())
			}
		})
	}
}

func TestSiteManagerScrollsSelectedRowAndKeepsActionsVisible(t *testing.T) {
	var sites []app.Website
	for i := range 25 {
		sites = append(sites, app.Website{
			Name: fmt.Sprintf("站点%02d", i) + strings.Repeat("长名称", 20),
			URL:  fmt.Sprintf("https://%02d.example/path", i),
		})
	}
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{120, 32}, {40, 16}, {24, 10}} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, size[0], size[1]), func(t *testing.T) {
				m := Model{width: size[0], height: size[1], locale: i18n.New(language), websites: sites, noColor: true}
				m.siteManager.err = strings.Repeat("Save error ", 15)
				for _, selected := range []int{0, 1, 19, 24, 2, 0} {
					m.selectManagerSite(selected)
					screen, targets := m.renderSiteManager("")
					assertCanvas(t, screen, m.width, m.height)
					assertRegions(t, targets, m.width, m.height)
					lines := strings.Split(ansi.Strip(screen), "\n")
					rowCount := 0
					for _, hit := range targets {
						if !strings.HasPrefix(hit.ID, "websiteManager.site.") {
							continue
						}
						i := m.siteManager.offset + rowCount
						rowCount++
						line := ansi.Cut(lines[hit.Y], hit.X, hit.X+hit.W)
						gripID := siteDragGrip + sites[i].URL
						grip := findTarget(t, Model{targets: targets}, gripID)
						if grip.X+grip.W != hit.X || grip.Y != hit.Y || ansi.Cut(lines[hit.Y], grip.X, grip.X+grip.W) != "⠿ " {
							t.Fatalf("drag grip is outside its own hit area for site %d: %v %v", i, grip, hit)
						}
						if hit.ID != "websiteManager.site."+sites[i].URL || !strings.Contains(line, fmt.Sprintf("站点%02d", i)) ||
							strings.Contains(line, "›") != (i == selected) || strings.Contains(line, sites[i].URL) != (m.width == 120) {
							t.Fatalf("mismatched row for site %d, selected %d: %s %q", i, selected, hit.ID, line)
						}
					}
					if rowCount == 0 || rowCount >= len(sites) || !hasAction(targets, "websiteManager.site."+sites[selected].URL) {
						t.Fatalf("selection %d is outside the list at offset %d", selected, m.siteManager.offset)
					}
					if !strings.Contains(ansi.Strip(screen), fmt.Sprintf("%d/%d", selected+1, len(sites))) {
						t.Fatal("missing scroll position")
					}
					for _, id := range []string{"websiteManager.add", "websiteManager.edit", "websiteManager.close"} {
						if !hasAction(targets, id) {
							t.Fatalf("missing action %s", id)
						}
					}
					if hasAction(targets, "websiteManager.up") != (selected > 0) || hasAction(targets, "websiteManager.down") != (selected+1 < len(sites)) {
						t.Fatal("unexpected move actions")
					}
				}
			})
		}
	}
}

func TestSiteManagerSingleSiteHasNoDragGrip(t *testing.T) {
	m := Model{width: 40, height: 16, locale: i18n.New(i18n.English), websites: app.DefaultWebsites()[:1]}
	screen, hits := m.renderSiteManager("")
	if strings.Contains(ansi.Strip(screen), "⠿") || hasAction(hits, siteDragGrip+m.websites[0].URL) || !hasAction(hits, siteManagerRowID+m.websites[0].URL) {
		t.Fatal("a single site should be selectable without offering drag sorting")
	}
}

func TestSiteManagerEmptyListShowsAddAndClose(t *testing.T) {
	m := Model{width: 32, height: 10, locale: i18n.New(i18n.Chinese)}
	m.siteManager.offset = 10
	m.selectManagerSite(0)
	screen, hits := m.renderSiteManager("")
	assertCanvas(t, screen, m.width, m.height)
	assertRegions(t, hits, m.width, m.height)
	if m.focus != "websiteManager.add" || m.siteManager.offset != 0 || !strings.Contains(ansi.Strip(screen), m.tr("dashboard.website.empty")) ||
		len(hits) != 2 || !hasAction(hits, "websiteManager.add") || !hasAction(hits, "websiteManager.close") {
		t.Fatal("empty manager lost its focus, message or actions", hits)
	}
}
