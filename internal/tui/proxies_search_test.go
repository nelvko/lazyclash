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
	if m.proxies.search.Value() != "hk 02" || !hasAction(m.contentTargets(), "node.0.2") || hasAction(m.contentTargets(), "node.0.1") {
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
				switch entry {
				case "activate":
					m.proxyActivate("group.0")
				case "right":
					m.proxyKey(special(tea.KeyRight))
				case "reference":
					m.browseProxyReference(group.Name)
				}
				m.layout()
				if m.focus != "node.0.38" || m.scroll == 0 || !hasAction(m.targets, m.focus) || backend.selections != 0 || m.busy {
					t.Fatalf("group entry did not reveal current node without selecting: focus=%s scroll=%d selections=%d", m.focus, m.scroll, backend.selections)
				}
			})
		}
	}
}

func TestProxiesStickyGroupFollowsScrollWithoutMovingFocus(t *testing.T) {
	m, _ := proxiesModel(t, 160, 24)
	m.snapshot.Groups = []mihomo.Group{
		{Name: "first", Type: "Selector", All: []string{"one", "two", "three", "four", "five", "six"}, Now: "one"},
		{Name: "second", Type: "Selector", All: []string{"seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen"}, Now: "seven"},
	}
	for i := range 30 {
		m.snapshot.Groups[1].All = append(m.snapshot.Groups[1].All, fmt.Sprintf("tail-%02d", i))
	}
	setProxyTestGroup(&m, "first")
	m.expandProxyGroup("second")
	m.focus = "node.0.0"
	width := dashboardBodyWidth(m.dashboardOptions())
	_, hits := m.proxiesBody(width)
	for _, hit := range hits {
		if hit.ID == "node.1.0" {
			m.scroll = hit.Y
			break
		}
	}
	bar := m.proxiesFixedToolbar(width, m.dashboardOptions().BodyTopRows)
	if !strings.Contains(ansi.Strip(strings.Join(bar.lines, "\n")), "second") || m.focus != "node.0.0" {
		t.Fatalf("group label followed focus instead of scroll: %q focus=%s", ansi.Strip(strings.Join(bar.lines, "\n")), m.focus)
	}
}
