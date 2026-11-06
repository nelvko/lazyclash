package tui

import (
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// Each field is matched independently, so a pattern cannot accidentally span
// the payload/type/proxy boundaries. Filtering never changes matching order.
func (m Model) ruleMatcher() (func(mihomo.Rule) bool, error) {
	query := strings.TrimSpace(m.rules.search.Value())
	if query == "" {
		return func(mihomo.Rule) bool { return true }, nil
	}
	pattern := query
	if !m.rules.regex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if m.rules.wholeWord {
		// Boundaries belong to the expression so alternatives can satisfy them:
		// a|ab must still match the complete word "ab". Go's \b is ASCII-only.
		pattern = `(?:^|[^\p{L}\p{N}_])(?:` + pattern + `)(?:$|[^\p{L}\p{N}_])`
	}
	if !m.rules.caseSensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return func(r mihomo.Rule) bool {
		for _, value := range []string{fmt.Sprintf("#%d", r.Index+1), fmt.Sprintf("index:%d", r.Index), r.Type, r.Payload, r.Proxy} {
			if re.MatchString(value) {
				return true
			}
		}
		return false
	}, nil
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
