package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestProxiesBodyOmitsEmptyToolbarRows(t *testing.T) {
	for _, size := range [][2]int{{200, 40}, {160, 20}, {100, 24}, {80, 24}, {50, 18}, {40, 16}, {20, 7}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m, _ := proxiesModel(t, size[0], size[1])
			setProxyTestGroup(&m, "")
			width := dashboardBodyWidth(m.dashboardOptions())
			toolbar := m.proxiesToolbar(width)
			body, hits := m.proxiesBody(width)
			if size[0] >= 50 {
				if len(toolbar.lines) != 0 || len(toolbar.targets) != 0 {
					t.Fatalf("empty toolbar occupied rows: %+v", toolbar)
				}
				firstGroupY := -1
				for _, hit := range hits {
					if hit.ID == "group.0" && (firstGroupY < 0 || hit.Y < firstGroupY) {
						firstGroupY = hit.Y
					}
				}
				if firstGroupY != 0 {
					t.Fatalf("first group started at row %d instead of the body top", firstGroupY)
				}
			} else {
				if len(toolbar.targets) == 0 || toolbar.targets[0].Y != 0 {
					t.Fatalf("narrow mode choices retained a leading blank row: %+v", toolbar.targets)
				}
				fixed := m.proxiesFixedToolbar(m.dashboardOptions())
				if !strings.HasPrefix(strings.Join(fixed.lines, "\n"), strings.Join(toolbar.lines, "\n")) || hasAction(hits, "mode.rule") {
					t.Fatal("narrow modes must lead the fixed toolbar and stay outside the scrolling body")
				}
			}
			assertRegions(t, hits, width, len(strings.Split(body, "\n")))
		})
	}
}

func TestProxiesCollapseAllUsesExistingTitleRow(t *testing.T) {
	const action = "proxies.collapse_all"
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{sidebarMinWidth, 20}, {160, 40}} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, size[0], size[1]), func(t *testing.T) {
				m, _ := proxiesModel(t, size[0], size[1])
				m.locale = i18n.New(language)
				setProxyTestGroup(&m, "")
				m.layout()
				g := dashboardGeometry(m.dashboardOptions())
				first := findTarget(t, m, "group.0")
				if hasAction(m.targets, action) || hasAction(m.focusTargets(), action) {
					t.Fatal("closed directory exposed a Collapse all control")
				}

				m, _ = click(m, first)
				collapse := findTarget(t, m, action)
				mode := findTarget(t, m, "mode.rule")
				if collapse.Y != g.headerY || collapse.X+collapse.W >= mode.X || !hasAction(m.focusTargets(), action) {
					t.Fatalf("Collapse all is not a keyboard and mouse action beside the title: collapse=%+v mode=%+v", collapse, mode)
				}
				row := ansi.Strip(strings.Split(m.screen, "\n")[collapse.Y])
				if !strings.Contains(row, m.tr("dashboard.page.proxies")) ||
					!strings.Contains(row, m.tr(action)) {
					t.Fatalf("title and Collapse all were not painted together: %q", row)
				}
				if got := findTarget(t, m, first.ID); got.Y != first.Y {
					t.Fatalf("opening the first group moved its heading from row %d to %d", first.Y, got.Y)
				}
				if hasAction(m.contentTargets(), action) {
					t.Fatal("Collapse all occupied a scrolling body target")
				}
				for _, target := range m.targets {
					if target.ID != action && regionsOverlap(collapse, target) {
						t.Fatalf("Collapse all overlaps %s: %+v / %+v", target.ID, collapse, target)
					}
				}

				// Reach the header control by cycling backward from the group.
				for range len(m.focusTargets()) {
					m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
					if m.focus == action {
						break
					}
				}
				if m.focus != action {
					t.Fatal("Tab could not reach Collapse all")
				}
				m, _ = update(m, special(tea.KeyEnter))
				if m.hasExpandedProxyGroups() || hasAction(m.targets, action) || m.focus == action {
					t.Fatal("keyboard activation retained open groups or focus on the removed control")
				}
				if got := findTarget(t, m, first.ID); got.Y != first.Y {
					t.Fatalf("collapsing all moved the first heading from row %d to %d", first.Y, got.Y)
				}
			})
		}
	}
}

func TestProxiesDirectModeOmitsRedundantModeNotice(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m, _ := proxiesModel(t, 160, 40)
		m.locale = i18n.New(language)
		m.snapshot.Mode.Desired, m.snapshot.Mode.Actual = "direct", "direct"
		setProxyTestGroup(&m, "")
		body, _ := m.proxiesBody(120)
		if strings.Contains(ansi.Strip(body), "Direct mode:") || strings.Contains(ansi.Strip(body), "Direct 模式：") {
			t.Fatal("Direct mode retained its legacy explanatory paragraph")
		}
		if strings.Contains(ansi.Strip(body), m.tr("dashboard.settings.actualMode", m.tr("dashboard.mode.direct"))) {
			t.Fatal("matching Direct mode added an unnecessary observed-mode notice")
		}
		m.snapshot.Mode.Desired = "rule"
		body, _ = m.proxiesBody(120)
		if !strings.Contains(ansi.Strip(body), m.tr("dashboard.settings.actualMode", m.tr("dashboard.mode.direct"))) {
			t.Fatal("different running Direct mode lost its diagnostic")
		}
	}
}
