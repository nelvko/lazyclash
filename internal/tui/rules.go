package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// rulesSource identifies the applied configuration and the running process.
// The wall-clock StartedAt anchor is intentionally excluded: the runtime owner
// recalibrates it without changing the process.
type rulesSource struct {
	generation, profile, controller string
	installed, configured, running  bool
	serviceUnavailable              bool
	pid                             int
	started                         uint64
}

func (m *Model) currentRulesSource() rulesSource {
	return rulesSource{
		generation: m.snapshot.Generation, profile: m.snapshot.Applied, controller: m.snapshot.Controller,
		installed: m.snapshot.Installed, configured: m.snapshot.ConfigReady, running: m.snapshot.Service.Active,
		pid: m.snapshot.Service.PID, started: m.snapshot.Service.StartTimestampMonotonic,
		serviceUnavailable: m.snapshot.ServiceError != "",
	}
}

type rulesState struct {
	source                    rulesSource
	snapshot                  app.RuleSnapshot
	search                    textinput.Model
	selected                  string
	selectedIndex             int
	list                      *rulesList
	expanded                  string
	generation                uint64
	active, pending, received bool
	editing                   bool
	caseSensitive, wholeWord  bool
	regex                     bool
	err                       error
	cancel                    context.CancelFunc
}
type rulesResultMsg struct {
	generation uint64
	snapshot   app.RuleSnapshot
	err        error
}

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
func (m *Model) rulesVisible() bool {
	return m.page == "rules" && !m.providers.open && !m.busy && !m.quitting && !m.ports.open && !m.wizard.open && !m.wizard.networkOpen && m.form == "" && m.ctx.Err() == nil
}
func (m *Model) syncRules() tea.Cmd {
	if !m.rulesVisible() {
		m.stopRules()
		return nil
	}
	source := m.currentRulesSource()
	if m.rules.source != source {
		m.stopRules()
		m.rules.source = source
		m.rules.snapshot, m.rules.err, m.rules.received = app.RuleSnapshot{}, nil, false
		m.rules.list = nil
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
	if !m.rules.active || !m.rules.pending || v.generation != m.rules.generation || m.rules.source != m.currentRulesSource() {
		return nil
	}
	if m.rules.cancel != nil {
		m.rules.cancel()
		m.rules.cancel = nil
	}
	m.rules.pending, m.rules.received = false, true
	m.rules.err = v.err
	m.rules.list = nil
	if v.err != nil {
		m.rules.snapshot = app.RuleSnapshot{State: "unavailable", Error: v.err.Error(), Diagnostic: v.err}
	} else {
		m.rules.snapshot = v.snapshot
	}
	m.reconcileRules()
	return nil
}
func (m *Model) retryRules() tea.Cmd {
	m.stopRules()
	if m.rules.snapshot.State != "ready" {
		m.rules.received = false
	}
	return m.syncRules()
}

// Identity excludes counters, so refreshing a rule's observations keeps focus.
// Its index alone is insufficient: applying another profile can reuse indexes.
func ruleIdentity(r mihomo.Rule) string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%s", r.Index, r.Type, r.Payload, r.Proxy)
}
func (m *Model) filteredRules() []mihomo.Rule {
	return m.rulesList().rows
}

func (m *Model) reconcileRules() {
	previousIndex := m.rules.selectedIndex
	list := m.rulesList()
	if index, found := list.identityPosition(m.rules.selected); found {
		m.rules.selectedIndex = index
	} else {
		m.rules.selected = ""
		if len(list.rows) > 0 {
			m.rules.selectedIndex = min(m.rules.selectedIndex, len(list.rows)-1)
			m.rules.selected = ruleIdentity(list.rows[m.rules.selectedIndex])
		}
	}
	if _, found := list.identityPosition(m.rules.expanded); !found {
		m.rules.expanded = ""
	}
	if m.page == "rules" && strings.HasPrefix(m.focus, "rule.") {
		previous := m.focus
		m.focus = "content"
		if m.rules.selected != "" {
			m.focus = ruleID(list.rows[m.rules.selectedIndex])
			if previous != m.focus || previousIndex != m.rules.selectedIndex {
				m.revealRule()
			}
		}
	}
}

// Logical focus remains valid when wheel scrolling hides the selected rule.
func (m *Model) rulesFocusValid() bool {
	list := m.rulesList()
	if strings.HasPrefix(m.focus, "rule.") {
		_, found := list.positions[m.focus]
		return found
	}
	width := dashboardBodyWidth(m.dashboardOptions())
	targets := m.rulesBodyLeading(width, list).targets
	for _, target := range append(targets, m.fixedContentTargets()...) {
		if target.ID == m.focus {
			return true
		}
	}
	return false
}

func (m *Model) revealRule() {
	list := m.rulesList()
	index, found := list.identityPosition(m.rules.selected)
	if !found {
		return
	}
	g := dashboardGeometry(m.dashboardOptions())
	leading := m.rulesBodyLeading(g.width, list)
	layout := m.rulesRowLayout(g.width, len(leading.lines), list)
	target := layout.target(list.rows[index], index)
	target.H = min(target.H, max(1, g.height))
	m.revealBodyTarget(target)
}
func (m *Model) focusRulesSearch() tea.Cmd {
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
	case "right":
		if strings.HasPrefix(m.focus, "rule.") && !m.busy {
			m.openRuleProxy(m.focus)
			return true, nil
		}
	case "esc":
		if m.rules.expanded != "" {
			m.rules.expanded = ""
			return true, nil
		}
	case "g", "ctrl+r":
		return true, m.retryRules()
	case "up", "k", "down", "j":
		list := m.rulesList()
		if len(list.rows) == 0 {
			return true, nil
		}
		index, _ := list.identityPosition(m.rules.selected)
		delta := 1
		if k.String() == "up" || k.String() == "k" {
			delta = -1
		}
		m.focusRule(list, max(0, min(len(list.rows)-1, index+delta)))
		m.revealRule()
		return true, nil
	}
	return false, nil
}

const ruleProxyActionPrefix = "rules.proxy."

func (m *Model) ruleProxyGroup(name string) (int, bool) {
	if name == "" || m.busy || m.quitting || m.proxiesDirect() || !m.proxiesReady() {
		return 0, false
	}
	index, _, found := m.proxyGroup(name)
	return index, found
}

func (m *Model) openRuleProxy(ruleID string) {
	rule, found := m.selectRule(ruleID)
	if !found {
		return
	}
	index, found := m.ruleProxyGroup(rule.Proxy)
	if !found {
		return
	}
	m.browsePage("proxies")
	m.closeProxySearch()
	if !m.proxyGroupInMode(rule.Proxy) && len(m.proxies.history) == 0 {
		// Use the same directory exception as a reference within Proxies.
		m.proxies.history = append(m.proxies.history, m.proxyLocation())
	}
	m.locateProxyGroup(index)
}

func (m *Model) ruleActivate(id string) tea.Cmd {
	if id == "rules.providers" {
		return m.openProviders(providerRules)
	}
	if index, ok := strings.CutPrefix(id, ruleProxyActionPrefix); ok {
		m.openRuleProxy("rule." + index)
		return nil
	}
	if id != "rules.search" {
		m.rules.editing = false
		m.rules.search.Blur()
	}
	switch {
	case id == "rules.search":
		return m.focusRulesSearch()
	case id == "rules.clear" || id == "rules.empty.clear":
		m.rules.search.Reset()
		m.rules.selectedIndex = 0
		m.reconcileRules()
		return m.focusRulesSearch()
	case id == "rules.case" || id == "rules.word" || id == "rules.regex":
		m.toggleRuleFilter(id)
	case id == "rules.retry":
		return m.retryRules()
	case strings.HasPrefix(id, "rule."):
		if r, ok := m.selectRule(id); ok {
			identity := ruleIdentity(r)
			if m.rules.expanded == identity {
				m.rules.expanded = ""
			} else if m.ruleCanExpand(r) {
				m.rules.expanded = identity
			} else {
				return nil
			}
			m.revealRule()
		}
	}
	return nil
}

// A click positions keyboard focus without interrupting list browsing. Enter
// expands full values and optional observations in place.
func (m *Model) selectRule(id string) (mihomo.Rule, bool) {
	list := m.rulesList()
	if index, found := list.positions[id]; found {
		m.rules.editing = false
		m.rules.search.Blur()
		m.focusRule(list, index)
		return list.rows[index], true
	}
	return mihomo.Rule{}, false
}

func (m *Model) focusRule(list *rulesList, index int) {
	rule := list.rows[index]
	m.rules.selectedIndex, m.rules.selected, m.focus = index, ruleIdentity(rule), ruleID(rule)
}

func (m *Model) rulesStatus() string {
	if state, status := m.corePageState(); state != "" {
		return status
	}
	if !m.rules.received {
		return m.tr("rules.loading")
	}
	switch m.rules.snapshot.State {
	case "ready":
		return m.tr("rules.loaded")
	case "uninstalled":
		return m.tr("runtime.status.not_installed")
	case "stopped":
		return m.tr("runtime.status.stopped")
	case "unsupported":
		return m.tr("rules.unsupported")
	default:
		return m.tr("rules.unavailable")
	}
}
