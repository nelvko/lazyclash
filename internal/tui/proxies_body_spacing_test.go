package tui

import (
	"fmt"
	"strings"
	"testing"

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
				if !strings.HasPrefix(body, strings.Join(toolbar.lines, "\n")) {
					t.Fatal("mode choices no longer lead the narrow body")
				}
			}
			assertRegions(t, hits, width, len(strings.Split(body, "\n")))
		})
	}
}

func TestProxiesCollapseToolbarStartsOnItsActionRow(t *testing.T) {
	for _, height := range []int{20, 40} {
		m, _ := proxiesModel(t, 160, height)
		width := dashboardBodyWidth(m.dashboardOptions())
		toolbar := m.proxiesToolbar(width)
		if len(toolbar.lines) != 1 || len(toolbar.targets) != 1 || toolbar.targets[0].Y != 0 {
			t.Fatalf("collapse toolbar retained empty rows: %+v", toolbar)
		}
		_, hits := m.proxiesBody(width)
		firstGroupY := -1
		for _, hit := range hits {
			if hit.ID == "group.0" && (firstGroupY < 0 || hit.Y < firstGroupY) {
				firstGroupY = hit.Y
			}
		}
		if firstGroupY != len(toolbar.lines) {
			t.Fatalf("first group started at row %d after %d toolbar rows", firstGroupY, len(toolbar.lines))
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
