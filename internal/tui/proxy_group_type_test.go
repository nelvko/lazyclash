package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestProxyReferenceCardUsesAPITypeInBothLanguages(t *testing.T) {
	for _, tc := range []struct{ language, distributed string }{
		{"en", "No fixed single node"},
		{"zh-Hans", "无固定单一节点"},
	} {
		m, _ := proxiesModel(t, 160, 40)
		m.locale = i18n.New(tc.language)
		group := &m.snapshot.Groups[1]
		for _, kind := range []string{"Selector", "URLTest", "Fallback", "LoadBalance", "Relay", "FutureGroup"} {
			group.Type = kind
			summary := "HK 02"
			if kind == "LoadBalance" || kind == "Relay" {
				summary = tc.distributed
			}
			if got, want := m.proxyNodeMeta(group.Name), kind+" · "+summary; got != want {
				t.Errorf("%s reference metadata = %q, want %q", tc.language, got, want)
			}
		}
	}
}

func TestProxySearchByGroupType(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.locale = i18n.New("zh-Hans")
	auto := m.snapshot.Groups[1]
	parent := m.snapshot.Groups[0]
	for _, query := range []string{"自动选择", "URL-TEST", "URLTest"} {
		m.proxies.search.SetValue(query)
		if got := m.proxyMembers(auto); len(got) != len(auto.All) {
			t.Fatalf("%q matched %d of the group's %d members", query, len(got), len(auto.All))
		}
		got := m.proxyMembers(parent)
		if len(got) != 1 || parent.All[got[0]] != auto.Name {
			t.Fatalf("%q should match only the reference in the parent group, got %v", query, got)
		}
	}
	m.proxies.search.SetValue("HK 02")
	if m.proxySearchMatchesNode(m.proxySearchQuery(), auto.Name) {
		t.Fatal("searching for a selected node also matched its group reference")
	}
}

// Use the rendered cell position because the type is ordinary group-header text.
func proxyLabelColumn(t *testing.T, line, label string) int {
	t.Helper()
	before, _, found := strings.Cut(line, label)
	if !found {
		t.Fatalf("missing proxy label %q in %q", label, line)
	}
	return ansi.StringWidth(before)
}

func TestProxyTypeHoverHighlightsItsGroupWithoutShowingText(t *testing.T) {
	for _, test := range []struct {
		name, label, groupID string
		width                int
	}{
		{"wide", "Relay", "group.0", 160},
		{"narrow", "URLTest", "group.1", 80},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, backend := proxiesModel(t, test.width, 40)
			m.locale = i18n.New("zh-Hans")
			m.snapshot.Groups[0].Type = "Relay"
			if m.proxiesNarrow() {
				setProxyTestGroup(&m, "")
			} else {
				m.focus = "group.1"
			}
			m.layout()
			group := findTarget(t, m, test.groupID)
			y := group.Y + 1 // Wide padded heading or the narrow metadata line.
			plain := ansi.Strip(m.screen)
			x := proxyLabelColumn(t, strings.Split(plain, "\n")[y], test.label)
			focus := m.focus
			before := themeViewCells(m)
			m, _ = update(m, tea.MouseMotionMsg{X: x, Y: y})
			if m.hover != group.ID || m.focus != focus || ansi.Strip(m.screen) != plain {
				t.Fatalf("hovering %s changed the text or keyboard focus, or missed its group: hover=%s focus=%s", test.label, m.hover, m.focus)
			}
			after := themeViewCells(m)
			right := group.X + dashboardBodyWidth(m.dashboardOptions()) - 1
			for row := group.Y; row < group.Y+group.H; row++ {
				for _, col := range []int{group.X, x, right} {
					if after.CellAt(col, row).Style.Bg == before.CellAt(col, row).Style.Bg {
						t.Fatalf("group hover did not highlight cell (%d, %d)", col, row)
					}
				}
			}
			m, _ = update(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if m.proxiesNarrow() {
				if m.proxies.activeGroup != "AUTO" || !hasAction(m.targets, "node.1.0") {
					t.Fatal("clicking the type on the second line did not open the group's members")
				}
			} else if m.proxyGroupExpanded("PROXY") || m.focus != group.ID {
				t.Fatal("clicking the type in an open header did not fold and focus its group")
			}
			if backend.selections != 0 {
				t.Fatal("clicking the group type selected a node")
			}
		})
	}
}
