package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func (m Model) ruleFilterButtons() dashboardBlock {
	var b dashboardBlock
	line := ""
	navigation := m.navigationOptions()
	for _, item := range []struct {
		id, label string
		selected  bool
	}{{"rules.case", "Aa", m.rules.caseSensitive}, {"rules.word", "ab", m.rules.wholeWord}, {"rules.regex", ".*", m.rules.regex}, {"rules.clear", "×", false}} {
		o := dashboardButtonOptions(item.id, navigation, false, bg)
		o.Disabled = m.busy
		o.Selectable, o.Selected = item.id != "rules.clear", item.selected
		value := renderButton(item.label, o)
		b.targets = append(b.targets, hitRegion{ID: item.id, X: ansi.StringWidth(line), W: ansi.StringWidth(value), H: 1})
		line += value
	}
	b.lines = []string{line}
	return b
}

func (m Model) ruleSearchWidth(width int) int {
	if width >= 68 {
		width -= ansi.StringWidth(m.ruleFilterButtons().lines[0]) + 1
	}
	return max(1, width-3)
}

func (m Model) rulesToolbar(width, rows int) dashboardBlock {
	b := dashboardBlock{}
	buttons := m.ruleFilterButtons()
	input := m.rules.search
	input.Prompt, input.Placeholder = "/ ", m.tr("rules.search_placeholder")
	input.SetWidth(m.ruleSearchWidth(width))
	input.SetCursor(input.Position())
	styleInput(&input, inputBackground, false)
	fieldWidth := width
	if width >= 68 {
		fieldWidth -= ansi.StringWidth(buttons.lines[0]) + 1
	}
	b.lines = append(b.lines, themed(cellPad(input.View(), fieldWidth), text, inputBackground, false))
	b.targets = append(b.targets, hitRegion{ID: "rules.search", W: fieldWidth, H: 1})
	if width >= 68 {
		b.lines[0] += themed(" ", text, bg, false) + buttons.lines[0]
		for i := range buttons.targets {
			buttons.targets[i].X += fieldWidth + 1
		}
	} else {
		b.lines = append(b.lines, cellCrop(buttons.lines[0], width))
		for i := range buttons.targets {
			buttons.targets[i].Y++
		}
	}
	b.targets = append(b.targets, buttons.targets...)
	status, color := m.rulesStatus(), muted
	if m.rules.received && m.rules.snapshot.State == "ready" {
		status += " · " + m.tr("rules.filtered_count", len(m.filteredRules()), len(m.rules.snapshot.Rules))
	}
	if _, err := m.ruleMatcher(); err != nil {
		status, color = m.tr("rules.invalid_regex"), errorColor
	}
	for _, item := range []struct{ id, key string }{{"rules.case", "rules.filter_case"}, {"rules.word", "rules.filter_word"}, {"rules.regex", "rules.filter_regex"}, {"rules.clear", "rules.filter_clear"}} {
		if m.focus == item.id || m.hover == item.id {
			status, color = m.tr(item.key), accent
			break
		}
	}
	b.lines = append(b.lines, themed(cellCrop(status, width), color, bg, false), "")
	b.lines = b.lines[:min(rows, len(b.lines))]
	visible := b.targets[:0]
	for _, target := range b.targets {
		if clipped, ok := clipRegion(target, 0, 0, width, len(b.lines)); ok && !m.busy {
			visible = append(visible, clipped)
		}
	}
	b.targets = visible
	return b
}

func (m Model) rulesBody(width int) (string, []hitRegion) {
	var lines []string
	var hits []hitRegion
	addText := func(value, color string) {
		lines = append(lines, strings.Split(wrapText(themed(clean(value), color, bg, false), max(1, width)), "\n")...)
	}
	addButton := func(id, label string) {
		options := dashboardButtonOptions(id, m.navigationOptions(), false, bg)
		options.Disabled = m.busy
		value := renderButton(label, options)
		if !options.Disabled {
			hits = append(hits, hitRegion{ID: id, Y: len(lines), W: min(width, ansi.StringWidth(value)), H: 1})
		}
		lines = append(lines, cellCrop(value, width))
	}
	if r := m.rules.detail; r != nil {
		addText(m.tr("rules.details"), text)
		if m.ruleCurrent(*r) {
			addText(m.tr("rules.observation"), muted)
		} else {
			addText(m.tr("rules.stale"), warning)
		}
		addButton("rules.back", m.tr("rules.back"))
		addText(fmt.Sprintf(m.tr("rules.order"), r.Index+1, r.Index), text)
		addText(m.tr("rules.type", r.Type), text)
		payload := r.Payload
		if payload == "" {
			payload = m.tr("rules.empty_payload")
		}
		addText(m.tr("rules.payload", payload), text)
		proxy := r.Proxy
		if proxy == "" {
			proxy = "--"
		}
		addText(m.tr("rules.proxy", proxy), text)
		if r.Size >= 0 {
			addText(fmt.Sprintf(m.tr("rules.entries"), r.Size), text)
		}
		if r.Extra != nil {
			state := m.tr("rules.enabled")
			if r.Extra.Disabled {
				state = m.tr("rules.disabled")
			}
			addText(m.tr("rules.state", state), text)
			addText(fmt.Sprintf(m.tr("rules.hits"), r.Extra.HitCount, r.Extra.MissCount), text)
		}
		addText(m.tr("rules.description"), muted)
		return strings.Join(lines, "\n"), hits
	}
	// Very short terminals fold the fixed toolbar into the scrollable body.
	if m.dashboardOptions().BodyTopRows == 0 {
		bar := m.rulesToolbar(width, 3)
		lines, hits = bar.lines, bar.targets
	}
	if m.rules.received {
		if m.rules.err != nil {
			addText(m.localizeError(m.rules.err), errorColor)
		} else if m.rules.snapshot.Error != "" {
			addText(m.locale.Diagnostic(m.rules.snapshot.Error, m.rules.snapshot.Diagnostic), errorColor)
		}
	}
	if !m.rules.received || m.rules.snapshot.State != "ready" {
		addText(m.rulesStatus(), muted)
		addText(m.tr("rules.description"), muted)
	}
	list := m.filteredRules()
	if _, err := m.ruleMatcher(); err != nil {
		addText(m.tr("rules.invalid_regex"), errorColor)
		return strings.Join(lines, "\n"), hits
	}
	if m.rules.received && m.rules.snapshot.State == "ready" && len(list) == 0 {
		message := m.tr("rules.empty")
		if m.rules.search.Value() != "" {
			message = m.tr("rules.no_matches")
		}
		addText(message, muted)
	}
	numberWidth := 1
	for _, r := range m.rules.snapshot.Rules {
		numberWidth = max(numberWidth, len(strconv.Itoa(r.Index+1)))
	}
	for index, r := range list {
		row := len(lines)
		lines = append(lines, m.ruleRows(r, width, numberWidth)...)
		hits = append(hits, hitRegion{ID: "rule." + strconv.Itoa(r.Index), Y: row, W: width, H: 2})
		if index < len(list)-1 {
			lines = append(lines, themed(strings.Repeat("─", max(0, width)), divider, bg, false))
		}
	}
	return strings.Join(lines, "\n"), hits
}

// Bubbles textinput owns editing and horizontal scrolling. The dashboard owns
// list scrolling; a small row renderer preserves its shared keyboard/mouse model
// and the core's order without introducing a second list viewport.
func (m Model) ruleRows(r mihomo.Rule, width, numberWidth int) []string {
	id := "rule." + strconv.Itoa(r.Index)
	surface, marker := bg, "  "
	if ruleIdentity(r) == m.rules.selected {
		marker = "▌ "
		surface = panel
	}
	if m.hover == id {
		surface = hoverBackground
	}
	markerColor := accent
	if m.focus == id {
		marker, markerColor = "› ", focus
	}
	indent := 2 + numberWidth + 2
	available := max(1, width-indent-1)
	payload := r.Payload
	if payload == "" {
		payload = m.tr("rules.empty_payload")
		if strings.EqualFold(r.Type, "Match") {
			payload = m.tr("rules.match_all")
		}
	}
	primaryColor := text
	if r.Extra != nil && r.Extra.Disabled {
		primaryColor = muted
	}
	number := fmt.Sprintf("%*d  ", numberWidth, r.Index+1)
	first := themed(marker, markerColor, surface, false) + themed(number, muted, surface, false) + themed(cellPad(clean(payload), available), primaryColor, surface, false)
	proxy := clean(r.Proxy)
	if proxy == "" {
		proxy = "--"
	}
	proxyColor := accent
	switch strings.ToUpper(r.Proxy) {
	case "DIRECT":
		proxyColor = success
	case "REJECT", "REJECT-DROP":
		proxyColor = errorColor
	}
	// Reserve room for the target before clipping an unusually long rule type.
	proxyWidth := min(ansi.StringWidth(proxy), max(1, available/2))
	typeWidth := min(20, max(1, available-proxyWidth-3))
	meta := themed(cellPad(clean(r.Type), typeWidth), muted, surface, false) + themed(" → ", muted, surface, false) + themed(cellCrop(proxy, max(1, available-typeWidth-3)), proxyColor, surface, false)
	if r.Extra != nil && r.Extra.Disabled {
		// Keep the disabled state visible even when the target is long.
		badge := m.tr("rules.disabled_suffix")
		meta = cellCrop(meta, max(0, available-ansi.StringWidth(badge))) + themed(badge, warning, surface, false)
	}
	second := themed(strings.Repeat(" ", indent), muted, surface, false) + meta
	return []string{themed(cellPad(first, width), text, surface, false), themed(cellPad(second, width), text, surface, false)}
}
