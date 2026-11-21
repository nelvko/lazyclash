package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type ruleSearch struct {
	query                           string
	caseSensitive, wholeWord, regex bool
}

// Only accepted data and the matching options change the ordered list. Each
// result is read-only so copies of Model can safely share it.
type rulesList struct {
	first       *mihomo.Rule
	count       int
	ready       bool
	search      ruleSearch
	rows        []mihomo.Rule
	positions   map[string]int
	numberWidth int
	matchErr    error
}

func (m *Model) rulesList() *rulesList {
	source := m.rules.snapshot.Rules
	var first *mihomo.Rule
	if len(source) > 0 {
		first = &source[0]
	}
	search := ruleSearch{strings.TrimSpace(m.rules.search.Value()), m.rules.caseSensitive, m.rules.wholeWord, m.rules.regex}
	ready := m.rules.received && m.rules.snapshot.State == "ready"
	if list := m.rules.list; list != nil && list.first == first && list.count == len(source) && list.ready == ready && list.search == search {
		return list
	}

	list := &rulesList{first: first, count: len(source), ready: ready, search: search, numberWidth: 1}
	match, err := compileSearchMatcher(search.query, search.caseSensitive, search.wholeWord, search.regex)
	list.matchErr = err
	if ready && err == nil {
		if search.query == "" {
			list.rows = source
		}
		for _, rule := range source {
			list.numberWidth = max(list.numberWidth, len(strconv.Itoa(rule.Index+1)))
			if search.query != "" && matchesRule(rule, match) {
				list.rows = append(list.rows, rule)
			}
		}
		list.positions = make(map[string]int, len(list.rows))
		for index, rule := range list.rows {
			list.positions[ruleID(rule)] = index
		}
	}
	m.rules.list = list
	return list
}

func ruleID(rule mihomo.Rule) string {
	return "rule." + strconv.Itoa(rule.Index)
}

func (list *rulesList) identityPosition(identity string) (int, bool) {
	if identity == "" {
		return 0, false
	}
	index, _, _ := strings.Cut(identity, "\x00")
	position, found := list.positions["rule."+index]
	if !found || ruleIdentity(list.rows[position]) != identity {
		return 0, false
	}
	return position, true
}

// Each field is matched independently. A pattern cannot span field boundaries.
func matchesRule(rule mihomo.Rule, match func(string) bool) bool {
	return match(rule.Type) || match(rule.Payload) || match(rule.Proxy) ||
		match("#"+strconv.Itoa(rule.Index+1)) || match("index:"+strconv.Itoa(rule.Index))
}

func (m *Model) toggleRuleFilter(id string) {
	switch id {
	case "rules.case":
		m.rules.caseSensitive = !m.rules.caseSensitive
	case "rules.word":
		m.rules.wholeWord = !m.rules.wholeWord
	case "rules.regex":
		m.rules.regex = !m.rules.regex
	}
	m.scroll = 0
	m.reconcileRules()
}

func (m *Model) ruleFilterKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	id := map[string]string{"alt+c": "rules.case", "alt+w": "rules.word", "alt+r": "rules.regex"}[k.String()]
	if id == "" {
		return false, nil
	}
	m.toggleRuleFilter(id)
	return true, nil
}
