package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestProxiesSearchFiltersNamesAndTypesWithoutChangingExpansion(t *testing.T) {
	m, backend := proxiesModel(t, 160, 40)
	setProxyTestGroup(&m, "")
	before := cloneProxyExpansion(m.proxies.expanded)
	m, _ = update(m, key("/"))
	if !m.proxies.editing || m.focus != "proxies.search" || !hasAction(m.targets, "proxies.search") {
		t.Fatal("search shortcut did not reveal the shared input")
	}
	m, _ = update(m, tea.PasteMsg{Content: "hk 02"})
	if m.proxies.search.Value() != "hk 02" || !hasAction(m.contentTargets(), "node.0.2") ||
		hasAction(m.contentTargets(), "node.0.1") || hasAction(m.contentTargets(), "node.0.0") {
		t.Fatalf("case-insensitive search did not filter members: query=%q targets=%+v", m.proxies.search.Value(), m.contentTargets())
	}
	if !reflect.DeepEqual(before, m.proxies.expanded) || backend.selections != 0 {
		t.Fatal("filtering changed saved expansion or selected a node")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.proxies.editing || !hasAction(m.targets, "proxies.search") {
		t.Fatal("nonempty search should remain visible after leaving editing")
	}
	m.proxyActivate("proxies.clear")
	m, _ = update(m, special(tea.KeyEscape))
	if m.proxySearchVisible() || !reflect.DeepEqual(before, m.proxies.expanded) {
		t.Fatal("clearing search did not restore the collapsed directory")
	}
	m.proxies.search.SetValue("selector")
	if got := m.proxyMembers(m.snapshot.Groups[0]); len(got) != len(m.snapshot.Groups[0].All) {
		t.Fatalf("matching group type did not retain its members: %v", got)
	}
	m.proxies.search.SetValue("vless")
	if got := m.proxyMembers(m.snapshot.Groups[0]); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("protocol type filter returned unrelated members: %v", got)
	}
}

func TestProxiesSearchOwnsTypingAndShowsNoMatches(t *testing.T) {
	m, backend := proxiesModel(t, 80, 24)
	m, _ = update(m, key("/"))
	m, _ = update(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if m.keyHints.hidden {
		t.Fatal("Ctrl+T leaked from the editor to the global hints shortcut")
	}
	for _, value := range []string{"q", "1", "s", "t", "i"} {
		m, _ = update(m, key(value))
	}
	if m.page != "proxies" || m.quitting || m.menu.open || m.wizard.open || m.proxies.testing || backend.selections != 0 {
		t.Fatal("search input leaked through to page or service shortcuts")
	}
	if m.proxies.search.Value() != "q1sti" || !strings.Contains(ansi.Strip(m.screen), m.tr("proxies.search_no_match")) {
		t.Fatalf("search text or no-match state missing: query=%q screen=%s", m.proxies.search.Value(), ansi.Strip(m.screen))
	}
	if cursor := m.inputCursor(); cursor == nil {
		t.Fatal("visible search input lost its terminal cursor")
	}
	m.browsePage("rules")
	if m.proxies.editing || m.proxies.search.Focused() {
		t.Fatal("switching pages left the proxy search editor active")
	}
}

func TestProxiesSearchPreservesOriginalMemberIDsAndUnicode(t *testing.T) {
	m, _ := proxiesModel(t, 80, 24)
	m.snapshot.Groups[0].All = []string{"东京 01", "香港 02", "香港 03"}
	m.snapshot.Groups[0].Now = "香港 03"
	m.proxies.search.SetValue("香港")
	if got := m.proxyMembers(m.snapshot.Groups[0]); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("filtered member IDs were renumbered: %v", got)
	}
	m.focusProxyGroupMember(0)
	if m.focus != "node.0.2" {
		t.Fatalf("entering filtered group did not find observed selection: %s", m.focus)
	}
	m.proxies.search.SetValue("东京")
	m.focusProxyGroupMember(0)
	if m.focus != "node.0.0" {
		t.Fatalf("hidden current selection did not fall back to a matching member: %s", m.focus)
	}
}

func TestProxiesEnteringGroupRevealsCurrentNodeWithoutSelecting(t *testing.T) {
	for _, width := range []int{80, 160} {
		for _, entry := range []string{"activate", "right", "reference"} {
			t.Run(fmt.Sprintf("%d/%s", width, entry), func(t *testing.T) {
				m, backend := proxiesModel(t, width, 24)
				group := &m.snapshot.Groups[0]
				group.All = nil
				for i := range 40 {
					group.All = append(group.All, fmt.Sprintf("node-%02d", i))
				}
				group.Now = group.All[38]
				setProxyTestGroup(&m, "")
				m.focus = "group.0"
				m.layout()
				heading := findTarget(t, m, "group.0")
				switch entry {
				case "activate":
					m, _ = update(m, special(tea.KeyEnter))
				case "right":
					m, _ = update(m, special(tea.KeyRight))
				case "reference":
					m.browseProxyReference(group.Name)
				}
				m.layout()
				if width == 160 && entry == "activate" {
					if m.focus != heading.ID || m.scroll != 0 || findTarget(t, m, heading.ID).Y != heading.Y ||
						!hasAction(m.targets, "node.0.0") || backend.selections != 0 {
						t.Fatalf("Enter moved the inline group to its selected node: focus=%s scroll=%d selections=%d", m.focus, m.scroll, backend.selections)
					}
					return
				}
				if m.focus != "node.0.38" || m.scroll == 0 || !hasAction(m.targets, m.focus) || backend.selections != 0 || m.busy {
					t.Fatalf("group entry did not reveal current node without selecting: focus=%s scroll=%d selections=%d", m.focus, m.scroll, backend.selections)
				}
			})
		}
	}
}

func TestProxiesStickyGroupFollowsVisibleMembersWithoutMovingFocus(t *testing.T) {
	m, _ := proxiesModel(t, 160, 36)
	m.snapshot.Groups = []mihomo.Group{
		{Name: "first", Type: "Selector", All: []string{"one", "two", "three", "four", "five", "six"}, Now: "one"},
		{Name: "second", Type: "Selector", Now: "tail-00"},
	}
	for i := range 42 {
		m.snapshot.Groups[1].All = append(m.snapshot.Groups[1].All, fmt.Sprintf("tail-%02d", i))
	}
	setProxyTestGroup(&m, "first")
	m.expandProxyGroup("second")
	m.focus = "node.0.0"
	m.proxies.testGroup, m.proxies.testStatus, m.proxies.testStatusVisible = "first", "done", true
	m.proxies.completed, m.proxies.total = 6, 6
	hits := m.contentTargets()
	first := listSearchTarget(t, hits, "group.0")
	second := listSearchTarget(t, hits, "group.1")
	g := dashboardGeometry(m.dashboardOptions())
	for _, state := range []struct {
		name  string
		row   int
		group string
	}{
		{"at top", 0, ""},
		{"heading partly visible", first.Y + first.H - 1, ""},
		{"first heading offscreen", first.Y + first.H, "first"},
		{"last visible first-group member", second.Y - 1, "first"},
		{"second heading visible", second.Y, ""},
		{"second heading offscreen", second.Y + second.H, "second"},
		{"back to first heading", first.Y, ""},
	} {
		t.Run(state.name, func(t *testing.T) {
			m.scroll = state.row
			m.layout()
			if m.scroll != state.row || m.focus != "node.0.0" {
				t.Fatalf("scroll fixture lost offset or focus: scroll=%d want=%d focus=%s", m.scroll, state.row, m.focus)
			}
			if got := dashboardGeometry(m.dashboardOptions()); got.y != g.y || got.height != g.height {
				t.Fatalf("sticky context changed the viewport: got y=%d h=%d, want y=%d h=%d", got.y, got.height, g.y, g.height)
			}
			plain := ansi.Strip(m.screen)
			for _, group := range []string{"first", "second"} {
				label := m.tr("proxies.search_group", group)
				if strings.Contains(plain, label) != (group == state.group) {
					t.Fatalf("at offset %d, want sticky group %q, checked label %q", m.scroll, state.group, label)
				}
			}
			if state.group != "" {
				node := "node.0.0"
				if state.group == "second" {
					node = "node.1.0"
				} else if state.row == second.Y-1 {
					node = "node.0.5"
				}
				hit := findTarget(t, m, node)
				if hit.Y < g.y || hit.Y+hit.H > g.y+g.height {
					t.Fatalf("sticky member outside body: target=%+v top=%d height=%d", hit, g.y, g.height)
				}
			}
		})
	}
}

func TestProxiesStickyGroupClampsAfterContentShrinks(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.snapshot.Groups = []mihomo.Group{
		{Name: "first", Type: "Selector", All: []string{"one"}, Now: "one"},
		{Name: "second", Type: "Selector", All: []string{"two"}, Now: "two"},
	}
	setProxyTestGroup(&m, "first")
	m.expandProxyGroup("second")
	m.scroll = 100000
	o := m.dashboardOptions()
	m.populatePageBody(&o)
	if len(strings.Split(o.Body, "\n")) > dashboardGeometry(o).height {
		t.Fatal("fixture should fit after the group contents shrink")
	}
	m.layout()
	plain := ansi.Strip(m.screen)
	if m.scroll != 0 || strings.Contains(plain, m.tr("proxies.search_group", "first")) ||
		strings.Contains(plain, m.tr("proxies.search_group", "second")) {
		t.Fatal("a stale offset showed a sticky label even though the shortened body fits")
	}
}

func TestProxiesNarrowMemberViewKeepsItsGroupTitle(t *testing.T) {
	m := proxyHoverModel(t, 48)
	m.width, m.height = 80, 24
	setProxyTestGroup(&m, "")
	m.layout()
	label := m.tr("proxies.search_group", "PROXY")
	if strings.Contains(ansi.Strip(m.screen), label) {
		t.Fatal("narrow directory duplicated the group name")
	}
	m, _ = click(m, findTarget(t, m, "group.0"))
	before := strings.Split(ansi.Strip(m.screen), "\n")
	groupTitleRow := -1
	for index, row := range before {
		if strings.Contains(row, label) {
			groupTitleRow = index
			break
		}
	}
	if groupTitleRow < 0 || m.proxies.activeGroup != "PROXY" || hasAction(m.targets, "group.1") {
		t.Fatal("narrow group page did not show its own title")
	}
	g := dashboardGeometry(m.dashboardOptions())
	for range 3 {
		m, _ = update(m, tea.MouseWheelMsg{X: g.x + 5, Y: g.y + 4, Button: tea.MouseWheelDown})
	}
	after := strings.Split(ansi.Strip(m.screen), "\n")
	if m.scroll == 0 || hasAction(m.targets, "group.0") || !strings.Contains(after[groupTitleRow], label) {
		t.Fatal("narrow group title did not stay visible above scrolled nodes")
	}
}

func TestProxiesFixedTargetsMatchSearchControls(t *testing.T) {
	for _, width := range []int{80, 160} {
		for _, query := range []string{"", "HK"} {
			m, _ := proxiesModel(t, width, 24)
			m.proxies.search.SetValue(query)
			o := m.dashboardOptions()
			m.populatePageBody(&o)
			g := dashboardGeometry(o)
			want := append([]hitRegion(nil), o.BodyTop.targets...)
			for i := range want {
				want[i].X += g.x
				want[i].Y += g.y - o.BodyTopRows
			}
			if got := m.fixedContentTargets(); !reflect.DeepEqual(got, want) {
				t.Fatalf("%d columns query %q: logical search targets differ from painted controls: got=%+v want=%+v", width, query, got, want)
			}
		}
	}
}
