package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func assertRuleExpansionHint(t *testing.T, m Model, action string) {
	t.Helper()
	want := ""
	if action != "" {
		want = m.tr("navigation.rules." + action)
	}
	footer := ansi.Strip(strings.Split(m.screen, "\n")[m.height-1])
	for _, hint := range []string{m.contextHelp(), footer} {
		if want == "" && strings.Contains(hint, "Enter") || want != "" && !strings.Contains(hint, want) {
			t.Fatalf("wanted action %q in hint %q", action, hint)
		}
	}
}

func TestRulesExpansionHintsFollowVisibleContent(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{80, 160} {
			for _, test := range []struct {
				name       string
				rule       mihomo.Rule
				expandable bool
			}{
				{"short", mihomo.Rule{Type: "Domain", Payload: "example.com", Proxy: "DIRECT", Size: -1}, false},
				{"match", mihomo.Rule{Type: "Match", Proxy: "DIRECT", Size: -1}, false},
				{"payload", mihomo.Rule{Type: "Domain", Payload: strings.Repeat("中文条件", 30), Proxy: "DIRECT", Size: -1}, true},
				{"type", mihomo.Rule{Type: strings.Repeat("LongType", 20), Payload: "example.com", Proxy: "DIRECT", Size: -1}, true},
				{"target", mihomo.Rule{Type: "Domain", Payload: "example.com", Proxy: strings.Repeat("目标", 60), Size: -1}, true},
				{"empty-set", mihomo.Rule{Type: "RuleSet", Payload: "test", Proxy: "DIRECT", Size: 0}, true},
			} {
				t.Run(fmt.Sprintf("%s/%d/%s", language, width, test.name), func(t *testing.T) {
					m := rulesModel(t, &fakeBackend{})
					m.locale, m.width, m.height = i18n.New(language), width, 24
					m.rules.snapshot.Rules = []mihomo.Rule{test.rule}
					m.reconcileRules()
					m.layout()
					if !test.expandable {
						assertRuleExpansionHint(t, m, "")
						before := m.screen
						m, _ = update(m, special(tea.KeyEnter))
						if m.rules.expanded != "" || m.screen != before {
							t.Fatal("fully visible rule changed on Enter")
						}
						return
					}
					assertRuleExpansionHint(t, m, "expand")
					m, _ = update(m, special(tea.KeyEnter))
					if m.rules.expanded != ruleIdentity(test.rule) {
						t.Fatal("Enter did not reveal hidden content")
					}
					assertRuleExpansionHint(t, m, "collapse")
					m, _ = update(m, special(tea.KeyEnter))
					if m.rules.expanded != "" {
						t.Fatal("Enter did not collapse the rule")
					}
					assertRuleExpansionHint(t, m, "expand")
				})
			}
		}
	}
}

func TestRulesExpansionHintsFollowResizeAndFocus(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	m.width, m.height = 220, 24
	m.rules.snapshot.Rules[0].Payload = strings.Repeat("x", 80)
	m.reconcileRules()
	m.layout()
	assertRuleExpansionHint(t, m, "")
	m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	assertRuleExpansionHint(t, m, "expand")
	m, _ = update(m, special(tea.KeyEnter))
	assertRuleExpansionHint(t, m, "collapse")
	m, _ = update(m, special(tea.KeyDown))
	assertRuleExpansionHint(t, m, "")
	expanded := m.rules.expanded
	m, _ = update(m, special(tea.KeyEnter))
	if m.rules.expanded != expanded {
		t.Fatal("activating a short rule changed another rule's expansion")
	}
	m, _ = update(m, special(tea.KeyUp))
	m, _ = update(m, tea.WindowSizeMsg{Width: 220, Height: 24})
	assertRuleExpansionHint(t, m, "collapse")
	m, _ = update(m, special(tea.KeyEnter))
	assertRuleExpansionHint(t, m, "")
	m, _ = update(m, key("/"))
	if m.contextHelp() != m.tr("navigation.rules.searching") {
		t.Fatal("search input advertised a row expansion")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.rules.editing || m.rules.expanded != "" {
		t.Fatal("ending search expanded a rule")
	}
}
