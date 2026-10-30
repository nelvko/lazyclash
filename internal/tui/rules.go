package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type rulesState struct {
	snapshot                  app.RuleSnapshot
	search                    textinput.Model
	selected                  string
	detail                    *mihomo.Rule
	generation                uint64
	active, pending, received bool
	editing                   bool
	cancel                    context.CancelFunc
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
	if m.rules.active {
		m.rules.generation++
	}
	m.rules.active, m.rules.pending, m.rules.received = false, false, false
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
	if v.err != nil {
		m.rules.snapshot = app.RuleSnapshot{State: "unavailable", Error: clean(v.err.Error())}
	} else {
		m.rules.snapshot = v.snapshot
	}
	m.reconcileRules()
	id := m.rules.generation
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return rulesTickMsg{generation: id} })
}
func (m *Model) retryRules() tea.Cmd {
	m.stopRules()
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
	query := strings.ToLower(strings.TrimSpace(m.rules.search.Value()))
	var result []mihomo.Rule
	for _, r := range m.rules.snapshot.Rules {
		value := fmt.Sprintf("#%d index:%d %s %s %s", r.Index+1, r.Index, r.Type, r.Payload, r.Proxy)
		if query == "" || strings.Contains(strings.ToLower(value), query) {
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
	list := m.filteredRules()
	found := false
	for _, r := range list {
		if ruleIdentity(r) == m.rules.selected {
			found = true
			break
		}
	}
	if !found {
		m.rules.selected = ""
		if len(list) > 0 {
			m.rules.selected = ruleIdentity(list[0])
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
	if m.rules.detail == nil && strings.HasPrefix(m.focus, "rule.") {
		m.focus = "rules.search"
		for _, r := range list {
			if ruleIdentity(r) == m.rules.selected {
				m.focus = "rule." + strconv.Itoa(r.Index)
				m.revealRule()
				break
			}
		}
	}
}
func (m *Model) revealRule() {
	g := dashboardGeometry(dashboardOptions{Width: m.width, Height: m.height, Page: m.page})
	_, hits := m.rulesBody(g.width)
	height := max(1, g.height)
	for _, r := range m.filteredRules() {
		if ruleIdentity(r) != m.rules.selected {
			continue
		}
		for _, hit := range hits {
			if hit.ID == "rule."+strconv.Itoa(r.Index) {
				if hit.Y < m.scroll {
					m.scroll = hit.Y
				}
				if hit.Y+hit.H > m.scroll+height {
					m.scroll = max(0, hit.Y+hit.H-height)
				}
				return
			}
		}
	}
}
func (m *Model) focusRulesSearch() tea.Cmd {
	m.rules.detail = nil
	m.rules.editing = true
	m.focus, m.scroll = "rules.search", 0
	return m.rules.search.Focus()
}
func (m *Model) ruleSearchKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "enter", "esc", "tab", "shift+tab":
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
	switch k.String() {
	case "/":
		return true, m.focusRulesSearch()
	case "esc":
		if m.rules.detail != nil {
			m.rules.detail = nil
			m.scroll, m.focus = 0, "rule."
			m.reconcileRules()
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
			r := list[max(0, min(len(list)-1, index+delta))]
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
	case id == "rules.retry":
		return m.retryRules()
	case id == "rules.back":
		m.rules.detail = nil
		m.scroll, m.focus = 0, "rule."
		m.reconcileRules()
	case strings.HasPrefix(id, "rule."):
		for _, r := range m.filteredRules() {
			if id == "rule."+strconv.Itoa(r.Index) {
				copy := r
				m.rules.selected, m.rules.detail = ruleIdentity(r), &copy
				m.scroll, m.focus = 0, "rules.back"
				break
			}
		}
	}
	return nil
}
func (m Model) rulesStatus() string {
	if !m.rules.received {
		return "Loading rules…"
	}
	switch m.rules.snapshot.State {
	case "ready":
		return "Rules loaded"
	case "uninstalled":
		return "Core not installed"
	case "stopped":
		return "Core stopped"
	case "unsupported":
		return "Service management unavailable"
	default:
		return "Rules unavailable"
	}
}
func (m Model) rulesBody(width int) (string, []hitRegion) {
	var lines []string
	var hits []hitRegion
	addText := func(value, color string) {
		lines = append(lines, strings.Split(wrapText(themed(clean(value), color, bg, false), max(1, width)), "\n")...)
	}
	addButton := func(id, label string) {
		value := dashboardButton(id, label, dashboardOptions{Focus: m.focus, Hover: m.hover}, false)
		hits = append(hits, hitRegion{ID: id, Y: len(lines), W: min(width, ansi.StringWidth(value)), H: 1})
		lines = append(lines, cellCrop(value, width))
	}
	if r := m.rules.detail; r != nil {
		addText("Rule details", text)
		if m.ruleCurrent(*r) {
			addText("Current API observation · Read only", muted)
		} else {
			addText("Last observed rule · no longer confirmed current", warning)
		}
		addButton("rules.back", "Back to rules")
		addText(fmt.Sprintf("Order: #%d (API index %d)", r.Index+1, r.Index), text)
		addText("Type: "+r.Type, text)
		payload := r.Payload
		if payload == "" {
			payload = "(empty)"
		}
		addText("Payload: "+payload, text)
		proxy := r.Proxy
		if proxy == "" {
			proxy = "--"
		}
		addText("Proxy: "+proxy, text)
		if r.Size >= 0 {
			addText(fmt.Sprintf("Entries: %d", r.Size), text)
		}
		if r.Extra != nil {
			state := "Enabled"
			if r.Extra.Disabled {
				state = "Disabled"
			}
			addText("State: "+state, text)
			addText(fmt.Sprintf("Hits / misses: %d / %d", r.Extra.HitCount, r.Extra.MissCount), text)
		}
		addText("Loaded by the core; matching order applies in Rule mode. Read only.", muted)
		return strings.Join(lines, "\n"), hits
	}
	addText("Effective rules", text)
	addText(m.rulesStatus(), muted)
	addText("Loaded by the core; matching order applies in Rule mode. Read only.", muted)
	if m.rules.editing {
		addText("Search rules (Enter / Esc finish)", accent)
		lines = append(lines, cellCrop(m.rules.search.View(), width))
		hits = append(hits, hitRegion{ID: "rules.search", Y: len(lines) - 1, W: width, H: 1})
	} else {
		addButton("rules.search", "Search rules (/): "+clean(m.rules.search.Value()))
	}
	addText("↑↓ Select · Enter Details · / Search · g Refresh", muted)
	if m.rules.snapshot.Error != "" && m.rules.received {
		addText(m.rules.snapshot.Error, errorColor)
	}
	list := m.filteredRules()
	if m.rules.received && m.rules.snapshot.State == "ready" && len(list) == 0 {
		message := "No effective rules returned by Mihomo"
		if m.rules.search.Value() != "" {
			message = "No rules match this search"
		}
		addText(message, muted)
	}
	for _, r := range list {
		prefix, color := "  ", text
		if ruleIdentity(r) == m.rules.selected {
			prefix = "› "
		}
		if m.focus == "rule."+strconv.Itoa(r.Index) {
			color = focus
		}
		label := fmt.Sprintf("%s#%d %s · %s → %s", prefix, r.Index+1, clean(r.Type), clean(r.Payload), clean(r.Proxy))
		if r.Extra != nil && r.Extra.Disabled {
			label += " [disabled]"
		}
		row := len(lines)
		lines = append(lines, themed(cellCrop(label, width), color, bg, false))
		hits = append(hits, hitRegion{ID: "rule." + strconv.Itoa(r.Index), Y: row, W: width, H: 1})
	}
	return strings.Join(lines, "\n"), hits
}
