package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

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

func TestProxyGroupTypeHintAndClick(t *testing.T) {
	m, backend := proxiesModel(t, 160, 40)
	m.snapshot.Groups[0].Type = "Relay"
	m.locale = i18n.New("zh-Hans")
	m.layout()
	hit := findTarget(t, m, "proxies.type.0")
	motion := tea.MouseMotionMsg{X: hit.X, Y: hit.Y}
	m, _ = update(m, motion)
	if !strings.Contains(ansi.Strip(m.screen), "type: relay（已弃用）") || m.focus != "group.0" || hasAction(m.focusTargets(), hit.ID) {
		t.Fatal("the type hint must describe the group without changing keyboard navigation")
	}
	m, _ = click(m, hit)
	if m.focus != "group.0" || m.proxyGroupExpanded("PROXY") || backend.selections != 0 {
		t.Fatalf("clicking the label did not just collapse its own group: focus=%s expanded=%v", m.focus, m.proxies.expanded)
	}
	m, _ = update(m, motion)
	m, _ = update(m, tea.MouseWheelMsg{X: hit.X, Y: hit.Y, Button: tea.MouseWheelDown})
	if strings.Contains(ansi.Strip(m.screen), "type: relay") {
		t.Fatal("the hover hint followed a group when scrolling")
	}
	m.snapshot.Groups[0].Type = "FutureGroup"
	m.layout()
	if m.proxyGroupTypeLabel("FutureGroup") != "FutureGroup" || hasAction(m.targets, hit.ID) {
		t.Fatal("unknown API types should retain their name without a fabricated config value")
	}
}
