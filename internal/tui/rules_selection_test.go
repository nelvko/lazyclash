package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func assertRuleSelectionGutter(t *testing.T, screen string, target hitRegion, markers []string, surface string) {
	t.Helper()
	lines := strings.Split(screen, "\n")
	for row, marker := range markers {
		line := lines[target.Y+row]
		gutter := ansi.Cut(line, target.X, target.X+2)
		if plain := ansi.Strip(gutter); plain != marker {
			t.Fatalf("rule row %d gutter=%q; want %q", row, plain, marker)
		}
		if surface == "" {
			continue
		}
		var red, green, blue uint8
		if _, err := fmt.Sscanf(surface, "#%02x%02x%02x", &red, &green, &blue); err != nil {
			t.Fatal(err)
		}
		background := fmt.Sprintf("48;2;%d;%d;%d", red, green, blue)
		lastCell := ansi.Cut(line, target.X+target.W-1, target.X+target.W)
		if !strings.Contains(lastCell, background) {
			t.Fatalf("rule row %d lost its complete %s background: %q", row, surface, lastCell)
		}
		foreground := ""
		switch marker {
		case "› ":
			foreground = focus
		}
		if foreground != "" {
			if _, err := fmt.Sscanf(foreground, "#%02x%02x%02x", &red, &green, &blue); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(gutter, fmt.Sprintf("38;2;%d;%d;%d", red, green, blue)) {
				t.Fatalf("rule row %d lost marker color %s: %q", row, foreground, gutter)
			}
		}
	}
}

func TestRulesSelectionStaysAsNavigationAnchorAcrossPages(t *testing.T) {
	m := rulesMouseModel(t, 40, 160, 45)
	m, _ = click(m, findTarget(t, m, "rule.3"))
	selected := m.rules.selected
	if m.focus != "rule.3" || m.rules.expanded != "" {
		t.Fatal("click did not select a list rule")
	}
	m.returnToNavigation()
	m.layout()
	m, _ = update(m, special(tea.KeyUp))
	if m.page != "connections" || m.focus != "nav.connections" {
		t.Fatal("navigation did not leave Rules")
	}
	m, _ = update(m, special(tea.KeyDown))
	if m.page != "rules" || m.focus != "nav.rules" || m.rules.selected != selected || m.rules.expanded != "" {
		t.Fatal("browsing back lost rule selection or navigation focus")
	}
	assertRuleSelectionGutter(t, m.screen, findTarget(t, m, "rule.3"), []string{"  ", "  "}, bg)
	m, _ = update(m, special(tea.KeyEnter))
	if m.focus != "rule.3" || m.rules.selected != selected || m.rules.expanded != "" {
		t.Fatal("entering content did not restore the selected rule focus")
	}
	assertRuleSelectionGutter(t, m.screen, findTarget(t, m, "rule.3"), []string{"› ", "  "}, panel)
	m, _ = update(m, special(tea.KeyEnter))
	if m.rules.expanded != ruleIdentity(m.rules.snapshot.Rules[3]) {
		t.Fatal("Enter on the focused rule did not expand in place")
	}
	m.returnToNavigation()
	m.layout()
	target := findTarget(t, m, "rule.3")
	markers := make([]string, target.H)
	for i := range markers {
		markers[i] = "  "
	}
	assertRuleSelectionGutter(t, m.screen, target, markers, bg)
}

func TestRulesSelectionAndHoverKeepIndependentFeedback(t *testing.T) {
	m := rulesMouseModel(t, 40, 160, 45)
	m, _ = click(m, findTarget(t, m, "rule.3"))
	m.returnToNavigation()
	m.layout()
	selected := m.rules.selected
	for _, id := range []string{"rule.3", "rule.5", "rule.3"} {
		target := findTarget(t, m, id)
		m, _ = update(m, tea.MouseMotionMsg{X: target.X + 8, Y: target.Y + 1})
		if m.rules.selected != selected || m.focus != "nav.rules" || m.rules.expanded != "" {
			t.Fatal("hover changed selection or keyboard ownership")
		}
		surface := bg
		if id == "rule.3" {
			surface = hoverBackground
		}
		assertRuleSelectionGutter(t, m.screen, findTarget(t, m, "rule.3"), []string{"  ", "  "}, surface)
		if id == "rule.5" {
			assertRuleSelectionGutter(t, m.screen, target, []string{"  ", "  "}, hoverBackground)
		}
	}
	m.enterContent()
	m.layout()
	if m.focus != "rule.3" || m.hover != "rule.3" {
		t.Fatal("content focus did not return to the selected hovered rule")
	}
	assertRuleSelectionGutter(t, m.screen, findTarget(t, m, "rule.3"), []string{"› ", "  "}, hoverBackground)
}

func TestRulesSelectionDoesNotDecorateClippedRows(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {80, 24}, {60, 20}, {40, 9}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := rulesMouseModel(t, 60, size[0], size[1])
			m.selectRule("rule.30")
			m.focus = "rules.case"
			g := dashboardGeometry(m.dashboardOptions())
			row := m.rulesListPrefix(g.width) + 30*3
			// Only the second content line remains at the top of the viewport.
			m.scroll = row + 1
			m.renderLayout()
			target := findTarget(t, m, "rule.30")
			if target.Y != g.y || target.H != 1 {
				t.Fatalf("second-line target did not clip to top: %+v", target)
			}
			assertRuleSelectionGutter(t, m.screen, target, []string{"  "}, bg)
			// Neither the remembered row nor its separator has selection decoration.
			m.scroll = row + 2
			m.renderLayout()
			line := strings.Split(m.screen, "\n")[g.y]
			if hasAction(m.targets, "rule.30") || strings.Contains(ansi.Strip(ansi.Cut(line, g.x, g.x+2)), "▌") {
				t.Fatal("selection marker leaked onto the separator")
			}
			// The first content line can also be the last visible viewport row.
			m.scroll = row - g.height + 1
			m.renderLayout()
			target = findTarget(t, m, "rule.30")
			if target.Y != g.y+g.height-1 || target.H != 1 {
				t.Fatalf("first-line target did not clip to bottom: %+v", target)
			}
			assertRuleSelectionGutter(t, m.screen, target, []string{"  "}, bg)
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
		})
	}
}

func TestRulesSelectionOnlyShowsCurrentFocusWithoutColor(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		t.Run(fmt.Sprintf("NO_COLOR=%t", noColor), func(t *testing.T) {
			m := rulesMouseModel(t, 40, 160, 45)
			m.selectRule("rule.3")
			m.focus = "rules.case"
			m.noColor, m.colorProfile = noColor, colorprofile.ASCII
			if noColor {
				m.colorProfile = colorprofile.TrueColor
			}
			m.layout()
			content := m.View().Content
			if strings.Contains(content, "\x1b") {
				t.Fatal("monochrome view retained terminal styling")
			}
			assertRuleSelectionGutter(t, content, findTarget(t, m, "rule.3"), []string{"  ", "  "}, "")
			m.focus = "rule.3"
			m.layout()
			assertRuleSelectionGutter(t, m.View().Content, findTarget(t, m, "rule.3"), []string{"› ", "  "}, "")
		})
	}
}

// Expanding a browsed rule must not move the metadata columns while revealing
// its complete values below the same payload.
func TestRulesExpansionKeepsMetadataColumns(t *testing.T) {
	for _, width := range []int{20, 34, 60, 160} {
		t.Run(fmt.Sprintf("width=%d", width), func(t *testing.T) {
			m := rulesModel(t, &fakeBackend{})
			r := sampleRules()[2]
			r.Payload, r.Proxy = "all", "组名"
			collapsed := ansi.Strip(m.ruleRows(r, width, 1)[1])
			m.rules.expanded = ruleIdentity(r)
			expanded := ansi.Strip(m.ruleRows(r, width, 1)[1])
			for _, value := range []string{"→", r.Proxy} {
				before, _, found := strings.Cut(collapsed, value)
				if !found {
					t.Fatalf("collapsed row missing %q: %q", value, collapsed)
				}
				after, _, found := strings.Cut(expanded, value)
				if !found || ansi.StringWidth(before) != ansi.StringWidth(after) {
					t.Fatalf("expansion moved %q column: collapsed=%q expanded=%q", value, collapsed, expanded)
				}
			}
		})
	}
}

func TestRulesExpandedMetadataHeightMatchesWrappedRows(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	for _, width := range []int{12, 20, 34, 60, 160} {
		for _, ruleType := range []string{"Match", "DomainSuffix", "UnusuallyLongRuleTypeName"} {
			t.Run(fmt.Sprintf("width=%d/type=%s", width, ruleType), func(t *testing.T) {
				r := sampleRules()[0]
				r.Type, r.Proxy = ruleType, "一个完整的目标组名-with-a-long-target"
				r.Size, r.Extra = 12345, &mihomo.RuleExtra{Disabled: true}
				m.rules.expanded = ruleIdentity(r)
				rows := m.ruleRows(r, width, 1)
				if height := m.ruleHeight(r, width, 1); height != len(rows) {
					t.Fatalf("geometry height=%d, rendered=%d: %q", height, len(rows), rows)
				}
				var complete strings.Builder
				for _, row := range rows {
					if cells := ansi.StringWidth(row); cells > width {
						t.Fatalf("wrapped row exceeds width %d: %d cells", width, cells)
					}
					complete.WriteString(strings.TrimSpace(ansi.Strip(ansi.Cut(row, 5, width))))
				}
				for _, value := range []string{r.Type, r.Proxy} {
					if width >= 20 && !strings.Contains(complete.String(), value) {
						t.Fatalf("expanded metadata lost full value %q: %q", value, complete.String())
					}
				}
			})
		}
	}
}
