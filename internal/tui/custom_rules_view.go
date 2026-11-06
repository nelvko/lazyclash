package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m Model) customRuleControls() []struct {
	id, label string
	enabled   bool
} {
	s := m.customRules
	ready := s.loaded && !s.loading && !s.saving && !s.applying && !s.inputOpen && s.confirm == ""
	selected := s.selected >= 0 && s.selected < len(s.draft.Rules)
	return []struct {
		id, label string
		enabled   bool
	}{
		{"rules.custom-edit", m.tr("custom_rules.edit"), ready && selected},
		{"rules.custom-delete", m.tr("custom_rules.delete"), ready && selected},
		{"rules.custom-up", m.tr("custom_rules.up"), ready && selected && s.selected > 0},
		{"rules.custom-down", m.tr("custom_rules.down"), ready && selected && s.selected < len(s.draft.Rules)-1},
	}
}
func (m Model) customRulesConfirmText() string {
	switch m.customRules.confirm {
	case "apply":
		return m.tr("custom_rules.apply_confirm")
	case "delete":
		return m.tr("custom_rules.delete_confirm")
	case "quit":
		return m.tr("custom_rules.quit_confirm")
	default:
		return m.tr("custom_rules.discard_confirm")
	}
}
func (m Model) customRulesToolbarRows(width int) int {
	count, x := 1, 0
	// Button faces always include a two-cell focus gutter and two padding cells.
	for _, key := range []string{"custom_rules.edit", "custom_rules.delete", "custom_rules.up", "custom_rules.down"} {
		w := ansi.StringWidth(m.tr(key)) + 4
		if x > 0 && x+1+w > width {
			count++
			x = 0
		}
		if x > 0 {
			x++
		}
		x += w
	}
	rows := 3 + count
	if m.customRules.inputOpen {
		if m.height < 14 {
			rows += 3
		} else {
			rows += 5
		}
	}
	if m.customRules.confirm != "" {
		rows += len(strings.Split(wrapText(m.customRulesConfirmText(), max(1, width)), "\n")) + 1
	}
	return rows
}
func (m Model) customRulesToolbar(width, rows int) dashboardBlock {
	s := m.customRules
	b := dashboardBlock{}
	name := s.draft.ProfileName
	if name == "" {
		name = m.snapshot.AppliedName
	}
	if name == "" {
		name = m.snapshot.Applied
	}
	b.lines = append(b.lines, themed(cellCrop(m.tr("custom_rules.profile", clean(name)), width), text, bg, false))
	status, color := m.tr("custom_rules.loading"), muted
	if s.loaded {
		status = m.tr("custom_rules.saved_state")
		if !s.draft.Applied {
			status = m.tr("custom_rules.saved_inactive")
		}
		if m.customRulesDirty() {
			status, color = m.tr("custom_rules.unsaved"), warning
		} else if s.draft.Pending {
			status, color = m.tr("custom_rules.pending"), accent
		}
		if s.notice != "" {
			status = s.notice
		}
	}
	if s.saving {
		status = m.tr("custom_rules.saving")
	}
	if s.applying {
		status = m.tr("custom_rules.applying")
	}
	b.lines = append(b.lines, themed(cellCrop(status, width), color, bg, false))
	line := ""
	addButton := func(id, label string, enabled bool) {
		o := dashboardButtonOptions(id, m.navigationOptions(), false, bg)
		o.Disabled = !enabled || m.busy || m.quitting
		value := renderButton(label, o)
		w := ansi.StringWidth(value)
		if line != "" && ansi.StringWidth(line)+1+w > width {
			b.lines = append(b.lines, cellCrop(line, width))
			line = ""
		}
		if line != "" {
			line += " "
		}
		x := ansi.StringWidth(line)
		if !o.Disabled {
			b.targets = append(b.targets, hitRegion{ID: id, X: x, Y: len(b.lines), W: min(w, max(0, width-x)), H: 1})
		}
		line += value
	}
	flush := func() { b.lines = append(b.lines, cellCrop(line, width)); line = "" }
	for _, item := range m.customRuleControls() {
		addButton(item.id, item.label, item.enabled)
	}
	flush()
	if s.inputOpen {
		label := m.tr("custom_rules.add_rule")
		if s.editIndex >= 0 {
			label = m.tr("custom_rules.edit_rule")
		}
		b.lines = append(b.lines, themed(cellCrop(label, width), accent, bg, false))
		input := s.input
		input.SetWidth(max(1, width-5))
		input.SetCursor(input.Position())
		styleInput(&input, inputBackground, s.err != nil)
		inputLines := inputFrame([]string{input.View()}, width, border, inputBackground)
		if m.height < 14 {
			input.SetWidth(max(1, width-1))
			input.SetCursor(input.Position())
			inputLines = []string{cellCrop(input.View(), width)}
		}
		b.targets = append(b.targets, hitRegion{ID: "rules.custom-input", Y: len(b.lines), W: width, H: len(inputLines)})
		b.lines = append(b.lines, inputLines...)
		addButton("rules.custom-submit", m.tr("custom_rules.done"), true)
		addButton("rules.custom-input-cancel", m.tr("custom_rules.cancel"), true)
		flush()
	}
	if s.confirm != "" {
		for _, v := range strings.Split(wrapText(m.customRulesConfirmText(), max(1, width)), "\n") {
			b.lines = append(b.lines, themed(v, warning, bg, false))
		}
		addButton("rules.custom-cancel", m.tr("custom_rules.cancel"), true)
		addButton("rules.custom-confirm", m.tr("custom_rules.confirm"), true)
		flush()
	}
	b.lines = append(b.lines, "")
	if rows > 0 && len(b.lines) > rows {
		b.lines = b.lines[:rows]
	}
	var targets []hitRegion
	for _, hit := range b.targets {
		if hit, ok := clipRegion(hit, 0, 0, width, len(b.lines)); ok {
			targets = append(targets, hit)
		}
	}
	b.targets = targets
	return b
}
func (m Model) customRulesBodyWindow(width, start, height int) ruleBodyWindow {
	s := m.customRules
	b := dashboardBlock{}
	if m.dashboardOptions().BodyTopRows == 0 {
		b = m.customRulesToolbar(width, 0)
	}
	if s.err != nil {
		for _, v := range strings.Split(wrapText(clean(m.localizeError(s.err)), max(1, width)), "\n") {
			b.lines = append(b.lines, themed(v, errorColor, bg, false))
		}
	}
	if !s.loaded || len(s.draft.Rules) == 0 {
		hint := m.tr("custom_rules.empty")
		if !s.loaded {
			hint = m.tr("custom_rules.persistence")
		}
		b.lines = append(b.lines, themed(cellCrop(hint, width), muted, bg, false))
		if !s.loading && !s.saving && !s.applying {
			v := renderButton(m.tr("custom_rules.reload"), dashboardButtonOptions("rules.custom-reload", m.navigationOptions(), false, bg))
			b.targets = append(b.targets, hitRegion{ID: "rules.custom-reload", Y: len(b.lines), W: min(width, ansi.StringWidth(v)), H: 1})
			b.lines = append(b.lines, cellCrop(v, width))
		}
		return ruleBodyWindow{dashboardBlock: b, total: len(b.lines), prefix: len(b.lines)}
	}
	prefix, total := len(b.lines), len(b.lines)
	numberWidth := len(strconv.Itoa(len(s.draft.Rules)))
	available := max(1, width-numberWidth-5)
	type ruleRow struct {
		at    int
		lines []string
	}
	positions := make([]ruleRow, len(s.draft.Rules))
	for index, raw := range s.draft.Rules {
		lines := strings.Split(wrapText(clean(raw), available), "\n")
		positions[index] = ruleRow{at: total, lines: lines}
		b.targets = append(b.targets, hitRegion{ID: "rules.custom-row." + strconv.Itoa(index), Y: total, W: width, H: len(lines)})
		total += len(lines) + 1
	}
	total--
	if height < 0 {
		start, height = 0, total
	} else {
		start = max(0, min(start, max(0, total-height)))
	}
	end := min(total, start+height)
	leading := b.lines
	b.lines = nil
	for i := max(0, start); i < min(prefix, end); i++ {
		b.lines = append(b.lines, leading[i])
	}
	for index, row := range positions {
		if row.at >= end || row.at+len(row.lines)+1 <= start {
			continue
		}
		surface, marker := bg, "  "
		if index == s.selected {
			surface, marker = panel, "▌ "
		}
		id := "rules.custom-row." + strconv.Itoa(index)
		markerColor := accent
		if m.focus == id {
			marker, markerColor = "› ", focus
		}
		if m.hover == id {
			surface = hoverBackground
		}
		for offset, line := range row.lines {
			if row.at+offset < start || row.at+offset >= end {
				continue
			}
			number := strings.Repeat(" ", numberWidth+2)
			mark := "  "
			if offset == 0 {
				number = fmt.Sprintf("%*d  ", numberWidth, index+1)
				mark = marker
			} else if index == s.selected && m.focus != id {
				mark = "▌ "
			}
			value := themed(mark, markerColor, surface, false) + themed(number, muted, surface, false) + themed(line, text, surface, false)
			b.lines = append(b.lines, themed(cellPad(value, width), text, surface, false))
		}
		at := row.at + len(row.lines)
		if index < len(positions)-1 && at >= start && at < end {
			b.lines = append(b.lines, themed(strings.Repeat("─", width), divider, bg, false))
		}
	}
	return ruleBodyWindow{dashboardBlock: b, start: start, total: total, prefix: prefix}
}
