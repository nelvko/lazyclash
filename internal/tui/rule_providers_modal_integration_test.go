package tui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRulesProviderModalOwnsInputAndRestoresUnderlyingList(t *testing.T) {
	m := rulesMouseModel(t, 60, 160, 45)
	_, backend := ruleProvidersModel(t, 35)
	m.backend = backend
	m.rules.search.SetValue("中文")
	m.rules.caseSensitive = true
	m.rules.selected = ruleIdentity(m.rules.snapshot.Rules[40])
	m.rules.expanded = m.rules.selected
	m.focus = "rule.40"
	m.revealRule()
	m.layout()
	scroll, selected, expanded := m.scroll, m.rules.selected, m.rules.expanded
	backgroundNavigation := findTarget(t, m, "nav.logs")
	view := m.pageViews["rules"]
	command := m.openProviders(providerRules)
	if command == nil {
		t.Fatal("opening providers did not request a current observation")
	}
	m.acceptProviders(command().(providersResultMsg))
	m.layout()
	if !m.providers.open || !m.dashboardOptions().Dim || m.scroll != scroll {
		t.Fatal("opening modal moved or failed to dim its underlying list")
	}
	for _, input := range []tea.KeyPressMsg{key("1"), key("2"), key("r"), key("?"), special(tea.KeyLeft), {Code: 't', Mod: tea.ModCtrl}} {
		m, _ = update(m, input)
	}
	m, _ = click(m, backgroundNavigation)
	m, _ = update(m, tea.MouseWheelMsg{X: 0, Y: 0, Button: tea.MouseWheelDown})
	if !m.providers.open || m.page != "rules" || m.helpOpen || m.sidebarOpen || m.scroll != scroll {
		t.Fatal("modal let navigation, keyboard shortcuts or background wheel events escape")
	}
	if !reflect.DeepEqual(m.pageViews["rules"], view) {
		t.Fatal("modal controls overwrote the remembered rule page focus")
	}
	row := findTarget(t, m, "provider.row."+backend.providers[0].Name)
	m, _ = update(m, tea.MouseWheelMsg{X: row.X + row.W/2, Y: row.Y, Button: tea.MouseWheelDown})
	if m.providers.viewport.YOffset() == 0 || m.scroll != scroll {
		t.Fatal("provider wheel did not scroll independently from the main rules")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.providers.open || m.focus != "rules.providers" || m.scroll != scroll || m.rules.selected != selected || m.rules.expanded != expanded || m.rules.search.Value() != "中文" || !m.rules.caseSensitive {
		t.Fatal("closing provider modal lost the underlying rules query, cursor, expansion or scroll")
	}
	if m.providers.active || m.providers.pending {
		t.Fatal("closed provider modal retained observation work")
	}
}
