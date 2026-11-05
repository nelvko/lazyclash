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

func TestRulesFilterModesPreserveOrderAndUnicodeWords(t *testing.T) {
	rules := []mihomo.Rule{
		{Index: 8, Type: "DomainSuffix", Payload: "Alpha.example", Proxy: "PROXY", Size: -1},
		{Index: 2, Type: "Domain", Payload: "alpha.example", Proxy: "DIRECT", Size: -1},
		{Index: 6, Type: "Domain", Payload: "alphabet.example", Proxy: "REJECT", Size: -1},
		{Index: 3, Type: "Domain", Payload: "中文.example", Proxy: "PROXY", Size: -1},
		{Index: 4, Type: "Domain", Payload: "前中文.example", Proxy: "PROXY", Size: -1},
		{Index: 5, Type: "Domain", Payload: "中文后.example", Proxy: "PROXY", Size: -1},
		{Index: 7, Type: "Domain", Payload: "中文_1.example", Proxy: "PROXY", Size: -1},
		{Index: 9, Type: "Domain", Payload: "🚀中文.example", Proxy: "PROXY", Size: -1},
		{Index: 10, Type: "Domain", Payload: "a.b", Proxy: "PROXY", Size: -1},
		{Index: 11, Type: "Domain", Payload: "axb", Proxy: "PROXY", Size: -1},
		{Index: 12, Type: "Domain", Payload: "ab", Proxy: "PROXY", Size: -1},
	}
	for _, test := range []struct {
		name, query              string
		matchCase, word, regular bool
		want                     []int
	}{
		{name: "default case insensitive order", query: "ALPHA", want: []int{8, 2, 6}},
		{name: "case sensitive", query: "Alpha", matchCase: true, want: []int{8}},
		{name: "whole ASCII word", query: "alpha", word: true, want: []int{8, 2}},
		{name: "whole Unicode word", query: "中文", word: true, want: []int{3, 9}},
		{name: "literal punctuation", query: "a.b", want: []int{10}},
		{name: "regular expression", query: "^a.b$", regular: true, want: []int{10, 11}},
		{name: "regular case sensitive", query: "^Alpha\\.", matchCase: true, regular: true, want: []int{8}},
		{name: "regular whole word", query: "alph[a]", word: true, regular: true, want: []int{8, 2}},
		{name: "regular Unicode whole word", query: "中[文]", word: true, regular: true, want: []int{3, 9}},
		{name: "regular whole word alternatives", query: "a|ab", word: true, regular: true, want: []int{10, 12}},
		{name: "proxy field", query: "^direct$", regular: true, want: []int{2}},
		{name: "one based order", query: "#9", word: true, want: []int{8}},
		{name: "API index", query: "index:8", word: true, want: []int{8}},
		{name: "independent fields", query: "Alpha.example DomainSuffix", want: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := rulesModel(t, &fakeBackend{})
			m.rules.snapshot.Rules = rules
			m.rules.search.SetValue(test.query)
			m.rules.caseSensitive, m.rules.wholeWord, m.rules.regex = test.matchCase, test.word, test.regular
			var got []int
			for _, r := range m.filteredRules() {
				got = append(got, r.Index)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("filter %q returned %v; want %v", test.query, got, test.want)
			}
		})
	}
}

func TestRulesInvalidRegexRecoversWhileEditing(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	m, _ = update(m, key("/"))
	m, _ = update(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModAlt})
	m, _ = update(m, tea.PasteMsg{Content: "["})
	if !m.rules.regex || len(m.filteredRules()) != 0 || !strings.Contains(ansi.Strip(m.screen), m.tr("rules.invalid_regex")) || hasAction(m.targets, "rule.0") {
		t.Fatal("invalid expression did not hide results and report its error")
	}
	m, _ = update(m, special(tea.KeyBackspace))
	m, _ = update(m, tea.PasteMsg{Content: "^needle\\."})
	if list := m.filteredRules(); len(list) != 1 || list[0].Index != 1 || strings.Contains(ansi.Strip(m.screen), m.tr("rules.invalid_regex")) {
		t.Fatal("correcting the expression did not restore current matches")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.rules.editing || m.focus != "rule.1" {
		t.Fatal("finishing corrected search did not select its result")
	}
}

func TestRulesSearchTabVisitsFiltersAndClearRestoresInput(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	m, _ = update(m, tea.WindowSizeMsg{Width: 160, Height: 45})
	m, _ = update(m, key("/"))
	m, _ = update(m, tea.PasteMsg{Content: "NEEDLE"})
	for _, id := range []string{"rules.case", "rules.word", "rules.regex", "rules.clear"} {
		m, _ = update(m, special(tea.KeyTab))
		if m.focus != id || m.rules.editing || m.rules.search.Focused() {
			t.Fatalf("Tab should visit %s after leaving input; focus=%s editing=%t", id, m.focus, m.rules.editing)
		}
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.focus != "rules.search" || !m.rules.editing || !m.rules.search.Focused() || m.rules.search.Value() != "" || len(m.filteredRules()) != len(sampleRules()) {
		t.Fatal("clear did not restore a focused empty search and complete results")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.focus != "rules.retry" || m.rules.editing || m.rules.search.Focused() {
		t.Fatalf("Shift+Tab did not return to the preceding fixed control: %s", m.focus)
	}
}

func longRedesignedRules(t *testing.T, width, height int) Model {
	t.Helper()
	m := rulesModel(t, &fakeBackend{})
	m.rules.snapshot.Rules = nil
	for i := 0; i < 60; i++ {
		m.rules.snapshot.Rules = append(m.rules.snapshot.Rules, mihomo.Rule{Index: i, Type: "DomainSuffix", Payload: fmt.Sprintf("%02d.中文🚀.example.invalid", i), Proxy: "中文代理", Size: -1})
	}
	m.reconcileRules()
	m, _ = update(m, tea.WindowSizeMsg{Width: width, Height: height})
	return m
}

func TestRulesFixedToolbarRetainsScrollDuringTabNavigation(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {80, 24}, {60, 20}, {40, 16}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := longRedesignedRules(t, size[0], size[1])
			before := findTarget(t, m, "rules.search")
			for i := 0; i < 25; i++ {
				m, _ = update(m, special(tea.KeyDown))
			}
			if m.scroll == 0 || m.focus != "rule.25" {
				t.Fatal("long list did not scroll to its selected rule")
			}
			scroll := m.scroll
			for _, id := range []string{"rules.clear", "rules.regex", "rules.word", "rules.case", "rules.search", "rules.retry"} {
				m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
				if m.focus != id || m.scroll != scroll {
					t.Fatalf("fixed control %s changed list viewport: focus=%s scroll=%d want=%d", id, m.focus, m.scroll, scroll)
				}
			}
			after := findTarget(t, m, "rules.search")
			if before != after {
				t.Fatalf("search moved with list: before=%+v after=%+v", before, after)
			}
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
		})
	}
}

func TestRulesBothRowLinesOpenTheSameRule(t *testing.T) {
	for _, row := range []int{0, 1} {
		t.Run(fmt.Sprint(row), func(t *testing.T) {
			m := rulesModel(t, &fakeBackend{})
			target := findTarget(t, m, "rule.1")
			if target.H != 2 {
				t.Fatalf("rule should occupy two interactive lines: %+v", target)
			}
			m, _ = update(m, tea.MouseClickMsg{X: target.X + target.W/2, Y: target.Y + row, Button: tea.MouseLeft})
			if m.rules.detail == nil || m.rules.detail.Index != 1 || hasAction(m.targets, "rules.search") {
				t.Fatal("rule line did not open its details without the list toolbar")
			}
			m, _ = update(m, special(tea.KeyEscape))
			if m.rules.detail != nil || m.focus != "rule.1" || !hasAction(m.targets, "rules.search") {
				t.Fatal("return from details did not restore rule and toolbar")
			}
		})
	}
}

func TestRulesDisabledAndMatchRowsRetainMeaning(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	m.rules.snapshot.Rules[0].Extra = &mihomo.RuleExtra{Disabled: true}
	m.layout()
	plain := ansi.Strip(m.screen)
	if !strings.Contains(plain, m.tr("rules.disabled_suffix")) || !strings.Contains(plain, m.tr("rules.match_all")) || !strings.Contains(plain, "DIRECT") {
		t.Fatal("disabled state or empty MATCH meaning disappeared from list")
	}
	if m.rules.snapshot.Rules[2].Payload != "" {
		t.Fatal("list presentation changed the raw MATCH payload")
	}
	m, _ = click(m, findTarget(t, m, "rule.2"))
	if !strings.Contains(ansi.Strip(m.screen), m.tr("rules.payload", m.tr("rules.empty_payload"))) {
		t.Fatal("details did not preserve empty MATCH payload")
	}
}

func TestRulesResizeAndDetailsPreserveFilteredSelection(t *testing.T) {
	m := longRedesignedRules(t, 160, 45)
	m.rules.search.SetValue("example")
	m.rules.wholeWord, m.rules.regex = true, true
	m.reconcileRules()
	for i := 0; i < 20; i++ {
		m, _ = update(m, special(tea.KeyDown))
	}
	identity, generation := m.rules.selected, m.rules.generation
	for _, size := range [][2]int{{80, 24}, {60, 20}, {40, 16}, {120, 18}, {20, 7}, {5, 2}, {160, 45}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		assertCanvas(t, m.screen, size[0], size[1])
		assertRegions(t, m.targets, size[0], size[1])
		if m.rules.selected != identity || m.rules.generation != generation || m.rules.search.Value() != "example" || !m.rules.wholeWord || !m.rules.regex {
			t.Fatalf("resize to %v changed search, selection or request", size)
		}
		if size[0] >= 20 && size[1] > 7 && !hasAction(m.targets, "rule.20") {
			t.Fatalf("resize to %v concealed selected row", size)
		}
	}
	scroll := m.scroll
	m, _ = update(m, special(tea.KeyEnter))
	if m.rules.detail == nil || m.rules.detail.Index != 20 {
		t.Fatal("selected filtered rule did not open")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.rules.detail != nil || m.rules.selected != identity || m.focus != "rule.20" || m.scroll != scroll || m.rules.search.Value() != "example" || !m.rules.wholeWord || !m.rules.regex {
		t.Fatal("details return lost selected rule, viewport or search modes")
	}
}
