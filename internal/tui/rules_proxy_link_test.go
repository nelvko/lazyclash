package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestRulesProxyLinkLocatesExactGroupAndRestoresList(t *testing.T) {
	for _, test := range []struct {
		name, mode string
		width      int
		expanded   bool
	}{
		{name: "clipped table target in Global mode", mode: "global", width: 160},
		{name: "wrapped target in narrow layout", mode: "rule", width: 60, expanded: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := rulesMouseModel(t, 70, test.width, 20)
			label := strings.Repeat("实验组🚀", 7)
			name := label + "-primary"
			m.snapshot.Mode = app.ModeStatus{Actual: test.mode, Desired: test.mode}
			m.snapshot.Groups = []mihomo.Group{
				{Name: label + "-other", Type: "Selector"},
				{Name: name, Type: "Selector", Now: "active-node", All: []string{"other-node", "active-node"}},
			}
			m.syncProxies()
			rule := m.rules.snapshot.Rules[23]
			rule.Proxy, rule.Size, rule.Extra = name, -1, &mihomo.RuleExtra{Disabled: true}
			m.rules.snapshot.Rules[23] = rule
			m.rules.search.SetValue("example.invalid")
			m.selectRule("rule.23")
			m.revealRule()
			m.layout()
			if test.expanded {
				m, _ = update(m, special(tea.KeyEnter))
				if m.rules.expanded != ruleIdentity(rule) {
					t.Fatal("Enter did not expand the long target within Rules")
				}
			}

			lines := strings.Split(m.screen, "\n")
			var fragments strings.Builder
			var link hitRegion
			for _, target := range m.targets {
				if target.ID == "rules.proxy.23" {
					fragments.WriteString(ansi.Strip(ansi.Cut(lines[target.Y], target.X, target.X+target.W)))
					link = target
				}
			}
			displayed := fragments.String()
			if test.expanded && displayed != name {
				t.Fatalf("wrapped hit regions cover %q, want complete group %q", displayed, name)
			}
			if !test.expanded && (!strings.HasSuffix(displayed, "…") || !strings.HasPrefix(name, strings.TrimSuffix(displayed, "…"))) {
				t.Fatalf("clipped hit region covers %q", displayed)
			}
			if hasAction(m.focusTargets(), link.ID) || !strings.Contains(m.contextHelp(), m.tr("navigation.rules.open_proxy")) {
				t.Fatal("the proxy action must be reachable from the focused rule without an extra Tab stop")
			}
			if test.expanded {
				// Scroll the start of the rule out of view. Returning should keep
				// the same visible fragment instead of revealing the whole rule.
				m.scroll += link.Y - dashboardGeometry(m.dashboardOptions()).y
				m.layout()
				link = findTarget(t, m, link.ID)
			}

			scroll, expanded := m.scroll, m.rules.expanded
			m, _ = click(m, link)
			if m.page != "proxies" || m.proxies.activeGroup != name || m.focus != "node.1.1" || !hasAction(m.targets, "node.1.1") {
				t.Fatalf("click did not reveal the exact group's observed member: page=%s group=%q focus=%s", m.page, m.proxies.activeGroup, m.focus)
			}
			m, _ = update(m, key("5"))
			if m.page != "rules" || m.focus != "rule.23" || m.scroll != scroll ||
				m.rules.search.Value() != "example.invalid" || m.rules.expanded != expanded {
				t.Fatalf("return lost the rule browsing position: page=%s focus=%s scroll=%d want=%d", m.page, m.focus, m.scroll, scroll)
			}
			m, _ = update(m, special(tea.KeyRight))
			if m.page != "proxies" || m.proxies.activeGroup != name || m.focus != "node.1.1" || m.snapshot.Mode.Actual != test.mode {
				t.Fatalf("Right did not reopen the same group in %s mode", test.mode)
			}
		})
	}
}

func TestRulesProxyLinkRequiresAnAvailableGroup(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	m.snapshot.Mode.Actual = "rule"
	m.snapshot.Groups = []mihomo.Group{{Name: "PROXY", Type: "Selector", All: []string{"standalone"}}}
	m.snapshot.Proxies = map[string]mihomo.Proxy{"standalone": {Type: "Shadowsocks"}}
	m.rules.snapshot.Rules = append(m.rules.snapshot.Rules, mihomo.Rule{Index: 3, Type: "Domain", Payload: "single-node.com", Proxy: "standalone"})
	m.syncProxies()
	m.layout()
	if !hasAction(m.targets, "rules.proxy.0") {
		t.Fatal("the group has no link")
	}
	for _, id := range []string{"rules.proxy.1", "rules.proxy.2", "rules.proxy.3"} {
		if hasAction(m.targets, id) {
			t.Fatalf("unexpected link for a built-in target or standalone node: %s", id)
		}
	}
	for _, state := range []string{"direct", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			if state == "direct" {
				m.snapshot.Mode.Actual = "direct"
			} else {
				m.snapshot.Mode.Actual, m.snapshot.APIError = "rule", "fixture unavailable"
			}
			m.syncProxies()
			m.layout()
			if hasAction(m.targets, "rules.proxy.0") || strings.Contains(m.contextHelp(), m.tr("navigation.rules.open_proxy")) {
				t.Fatal("an unavailable group still has an action")
			}
			m, _ = update(m, special(tea.KeyRight))
			if m.page != "rules" {
				t.Fatal("Right opened an unavailable destination")
			}
		})
	}
}

func TestRulesProxyLinkHoverTracksScroll(t *testing.T) {
	m := rulesMouseModel(t, 40, 160, 24)
	m.snapshot.Mode.Actual = "rule"
	m.snapshot.Groups = []mihomo.Group{{Name: "默认代理", Type: "Selector"}}
	m.syncProxies()
	m.layout()

	link := findTarget(t, m, "rules.proxy.0")
	m, _ = update(m, tea.MouseWheelMsg{X: link.X, Y: link.Y, Button: tea.MouseWheelDown})
	if m.hover != "rules.proxy.3" || m.focus != "rule.0" {
		t.Fatalf("scroll left hover on the wrong link or moved focus: hover=%s focus=%s", m.hover, m.focus)
	}
	m, _ = update(m, tea.MouseMotionMsg{X: link.X - 2, Y: link.Y})
	if m.hover != "rule.3" {
		t.Fatalf("the routing arrow still hovers the link: hover=%s", m.hover)
	}
}
