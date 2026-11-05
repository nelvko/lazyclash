package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type rulesState struct {
	snapshot                    app.RuleSnapshot
	search                      textinput.Model
	selected                    string
	selectedIndex, detailScroll int
	detail                      *mihomo.Rule
	generation                  uint64
	active, pending, received   bool
	editing                     bool
	caseSensitive, wholeWord    bool
	regex                       bool
	err                         error
	cancel                      context.CancelFunc
}
type rulesResultMsg struct {
	generation uint64
	snapshot   app.RuleSnapshot
	err        error
}
type rulesTickMsg struct{ generation uint64 }

func (m *Model) stopRules() {
	if m.rules.cancel != nil {
		m.rules.cancel()
		m.rules.cancel = nil
	}
	if m.rules.active || m.rules.pending {
		m.rules.generation++
	}
	m.rules.active, m.rules.pending = false, false
}
func (m Model) rulesVisible() bool {
	return m.page == "rules" && !m.busy && !m.quitting && !m.helpOpen && !m.ports.open && !m.wizard.open && !m.wizard.networkOpen && m.form == "" && m.ctx.Err() == nil
}
func (m *Model) syncRules() tea.Cmd {
	if !m.rulesVisible() {
		m.stopRules()
		return nil
	}
	if m.rules.active {
		return nil
	}
	m.rules.active = true
	return m.rulesCmd()
}
func (m *Model) rulesCmd() tea.Cmd {
	if m.rules.pending {
		return nil
	}
	m.rules.generation++
	ctx, cancel := context.WithTimeout(m.ctx, 8*time.Second)
	m.rules.cancel, m.rules.pending = cancel, true
	id, backend := m.rules.generation, m.backend
	return func() tea.Msg {
		s, err := backend.Rules(ctx)
		return rulesResultMsg{generation: id, snapshot: s, err: err}
	}
}
func (m *Model) acceptRules(v rulesResultMsg) tea.Cmd {
	if !m.rules.active || !m.rules.pending || v.generation != m.rules.generation {
		return nil
	}
	if m.rules.cancel != nil {
		m.rules.cancel()
		m.rules.cancel = nil
	}
	m.rules.pending, m.rules.received = false, true
	m.rules.err = v.err
	if v.err != nil {
		m.rules.snapshot = app.RuleSnapshot{State: "unavailable", Error: v.err.Error(), Diagnostic: v.err}
	} else {
		m.rules.snapshot = v.snapshot
	}
	m.reconcileRules()
	id := m.rules.generation
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return rulesTickMsg{generation: id} })
}
func (m *Model) retryRules() tea.Cmd {
	m.stopRules()
	m.rules.received = false
	return m.syncRules()
}

// Identity excludes counters, so refreshing a rule's observations keeps focus.
// Its index alone is insufficient: applying another profile can reuse indexes.
func ruleIdentity(r mihomo.Rule) string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%s", r.Index, r.Type, r.Payload, r.Proxy)
}
func (m Model) filteredRules() []mihomo.Rule {
	if !m.rules.received || m.rules.snapshot.State != "ready" {
		return nil
	}
	if strings.TrimSpace(m.rules.search.Value()) == "" {
		return m.rules.snapshot.Rules
	}
	match, err := m.ruleMatcher()
	if err != nil {
		return nil
	}
	var result []mihomo.Rule
	for _, r := range m.rules.snapshot.Rules {
		if match(r) {
			result = append(result, r)
		}
	}
	return result
}
func (m Model) ruleCurrent(r mihomo.Rule) bool {
	if !m.rules.received || m.rules.snapshot.State != "ready" {
		return false
	}
	for _, candidate := range m.rules.snapshot.Rules {
		if ruleIdentity(candidate) == ruleIdentity(r) {
			return true
		}
	}
	return false
}
func (m *Model) reconcileRules() {
	previousIndex := m.rules.selectedIndex
	list := m.filteredRules()
	found := false
	for index, r := range list {
		if ruleIdentity(r) == m.rules.selected {
			m.rules.selectedIndex, found = index, true
			break
		}
	}
	if !found {
		m.rules.selected = ""
		if len(list) > 0 {
			m.rules.selectedIndex = min(m.rules.selectedIndex, len(list)-1)
			m.rules.selected = ruleIdentity(list[m.rules.selectedIndex])
		}
	}
	if m.rules.detail != nil && m.rules.snapshot.State == "ready" {
		for _, r := range m.rules.snapshot.Rules {
			if ruleIdentity(r) == ruleIdentity(*m.rules.detail) {
				copy := r
				m.rules.detail = &copy
				break
			}
		}
	}
	if m.page == "rules" && m.rules.detail == nil && strings.HasPrefix(m.focus, "rule.") {
		previous := m.focus
		m.focus = "content"
		for _, r := range list {
			if ruleIdentity(r) == m.rules.selected {
				m.focus = "rule." + strconv.Itoa(r.Index)
				if previous != m.focus || previousIndex != m.rules.selectedIndex {
					m.revealRule()
				}
				break
			}
		}
	}
}
func (m *Model) revealRule() {
	width := dashboardBodyWidth(m.dashboardOptions())
	prefix := m.rulesListPrefix(width)
	for index, r := range m.filteredRules() {
		if ruleIdentity(r) != m.rules.selected {
			continue
		}
		m.revealBodyTarget(hitRegion{Y: prefix + index*3, H: 2})
		return
	}
}
func (m *Model) focusRulesSearch() tea.Cmd {
	m.rules.detail = nil
	m.rules.editing = true
	m.focus, m.scroll = "rules.search", 0
	return m.rules.search.Focus()
}
func (m *Model) ruleSearchKey(k tea.KeyPressMsg) tea.Cmd {
	if handled, cmd := m.ruleFilterKey(k); handled {
		return cmd
	}
	switch k.String() {
	case "tab", "shift+tab":
		m.rules.editing = false
		m.rules.search.Blur()
		if k.String() == "tab" {
			m.cycleFocus(1)
		} else {
			m.cycleFocus(-1)
		}
		return nil
	case "enter", "esc":
		m.rules.editing = false
		m.rules.search.Blur()
		m.focus = "rule."
		m.reconcileRules()
		return nil
	}
	var cmd tea.Cmd
	m.rules.search, cmd = m.rules.search.Update(k)
	m.scroll = 0
	m.reconcileRules()
	return cmd
}
func (m *Model) ruleKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	if handled, cmd := m.ruleFilterKey(k); handled {
		return true, cmd
	}
	switch k.String() {
	case "/":
		return true, m.focusRulesSearch()
	case "esc":
		if m.rules.detail != nil {
			m.backToRules()
			return true, nil
		}
	case "g", "ctrl+r":
		return true, m.retryRules()
	case "up", "k", "down", "j":
		if m.rules.detail == nil {
			list := m.filteredRules()
			if len(list) == 0 {
				return true, nil
			}
			index := 0
			for i, r := range list {
				if ruleIdentity(r) == m.rules.selected {
					index = i
					break
				}
			}
			delta := 1
			if k.String() == "up" || k.String() == "k" {
				delta = -1
			}
			m.rules.selectedIndex = max(0, min(len(list)-1, index+delta))
			r := list[m.rules.selectedIndex]
			m.rules.selected, m.focus = ruleIdentity(r), "rule."+strconv.Itoa(r.Index)
			m.revealRule()
			return true, nil
		}
	}
	return false, nil
}
func (m *Model) ruleActivate(id string) tea.Cmd {
	if id != "rules.search" {
		m.rules.editing = false
		m.rules.search.Blur()
	}
	switch {
	case id == "rules.search":
		return m.focusRulesSearch()
	case id == "rules.clear":
		m.rules.search.Reset()
		m.rules.selectedIndex = 0
		m.reconcileRules()
		return m.focusRulesSearch()
	case id == "rules.case" || id == "rules.word" || id == "rules.regex":
		m.toggleRuleFilter(id)
	case id == "rules.retry":
		return m.retryRules()
	case id == "rules.back":
		m.backToRules()
	case strings.HasPrefix(id, "rule."):
		if r, ok := m.selectRule(id); ok {
			m.rules.detailScroll, m.rules.detail = m.scroll, &r
			m.scroll, m.focus = 0, "rules.back"
		}
	}
	return nil
}

// A click positions keyboard focus without interrupting list browsing. Enter
// remains an explicit way to inspect full values and optional observations.
func (m *Model) selectRule(id string) (mihomo.Rule, bool) {
	for index, r := range m.filteredRules() {
		if id == "rule."+strconv.Itoa(r.Index) {
			m.rules.editing = false
			m.rules.search.Blur()
			m.rules.selectedIndex, m.rules.selected, m.focus = index, ruleIdentity(r), id
			return r, true
		}
	}
	return mihomo.Rule{}, false
}
func (m *Model) backToRules() {
	m.rules.detail = nil
	m.scroll, m.focus = m.rules.detailScroll, "content"
	m.reconcileRules()
	for _, r := range m.filteredRules() {
		if ruleIdentity(r) == m.rules.selected {
			m.focus = "rule." + strconv.Itoa(r.Index)
			break
		}
	}
}

func (m Model) rulesStatus() string {
	if !m.rules.received {
		return m.tr("rules.loading")
	}
	switch m.rules.snapshot.State {
	case "ready":
		return m.tr("rules.loaded")
	case "uninstalled":
		return m.tr("rules.uninstalled")
	case "stopped":
		return m.tr("rules.stopped")
	case "unsupported":
		return m.tr("rules.unsupported")
	default:
		return m.tr("rules.unavailable")
	}
}
