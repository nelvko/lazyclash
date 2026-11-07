package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func (m Model) ruleFilterButtons() dashboardBlock {
	var b dashboardBlock
	line := ""
	for _, item := range []struct {
		id, label string
		selected  bool
	}{{"rules.case", "Aa", m.rules.caseSensitive}, {"rules.word", "ab", m.rules.wholeWord}, {"rules.regex", ".*", m.rules.regex}} {
		o := m.searchControlOptions(item.id)
		o.Disabled = m.busy
		o.Selected = item.selected
		value := renderRuleSearchToggle(item.label, o)
		if !o.Disabled {
			b.targets = append(b.targets, hitRegion{ID: item.id, X: ansi.StringWidth(line), W: ansi.StringWidth(value), H: 1})
		}
		line += value
	}
	b.lines = []string{line}
	return b
}

// One status cell per toggle keeps adjacent labels one cell apart. Plain
// terminals combine selection and focus in that cell; color uses text emphasis.
// The whole-word icon is always underlined, including when the option is off.
func renderRuleSearchToggle(label string, o buttonOptions) string {
	foreground := muted
	if o.Selected {
		foreground = accent
	}
	if o.Hovered && !o.Disabled && !o.Selected {
		foreground = text
	}
	if o.Disabled {
		foreground = disabled
	}
	marker := " "
	if o.Plain && o.Selected {
		marker = "*"
	}
	if o.Focused && !o.Disabled {
		marker = "›"
		if o.Plain && o.Selected {
			marker = "+"
		}
	}
	face := lipgloss.NewStyle().Bold(o.Selected).Underline(label == "ab").Render(label)
	return themed(marker, focus, bg, o.Dim) + themed(face, foreground, bg, o.Dim)
}

func (m Model) ruleSearchWidth(width int) int {
	return inputFieldTextWidth(min(inputFieldMaxWidth, max(1, width)), searchControlWidth(m.ruleFilterButtons()), true)
}

func (m Model) rulesToolbar(width, rows int) dashboardBlock {
	_, matchErr := m.ruleMatcher()
	b := m.searchToolbar("rules.search", m.tr("rules.search_placeholder"), m.rules.search,
		m.ruleFilterButtons(), width, matchErr != nil)
	if m.searchToolbarGap() > 0 {
		b.lines = append([]string{""}, b.lines...)
		for i := range b.targets {
			b.targets[i].Y++
		}
	}
	status := m.rulesStatus()
	if m.rules.received && m.rules.snapshot.State == "ready" {
		status = m.tr("rules.filtered_count", len(m.filteredRules()), len(m.rules.snapshot.Rules))
	}
	line := themed(cellCrop(status, width), muted, bg, false)
	for _, item := range []struct{ id, key string }{{"rules.case", "rules.filter_case"}, {"rules.word", "rules.filter_word"}, {"rules.regex", "rules.filter_regex"}, {"rules.clear", "rules.filter_clear"}} {
		if matchErr == nil && (m.focus == item.id || m.hover == item.id) {
			hint := " · " + m.tr(item.key)
			if ansi.StringWidth(status+hint) <= width {
				line += themed(hint, accent, bg, false)
			}
			break
		}
	}
	diagnostic := ""
	if matchErr != nil {
		diagnostic = themed(cellCrop(m.tr("rules.invalid_regex"), width), errorColor, bg, false)
	}
	b.lines = append(b.lines, line, diagnostic)
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

type ruleBodyWindow struct {
	dashboardBlock
	start, total, prefix int
}

func (m Model) rulesBody(width int) (string, []hitRegion) {
	body := m.rulesBodyWindow(width, 0, -1)
	return strings.Join(body.lines, "\n"), body.targets
}

func (m Model) rulesTargets(width int) []hitRegion {
	return m.rulesBodyWindow(width, 0, 0).targets
}

func (m Model) rulesListPrefix(width int) int {
	return m.rulesBodyWindow(width, 0, 0).prefix
}

// Geometry covers the complete list; only rows intersecting the requested
// window are styled. A negative height requests the complete body for export.
func (m Model) rulesBodyWindow(width, start, height int) ruleBodyWindow {
	var lines []string
	var hits []hitRegion
	static := func() ruleBodyWindow {
		return ruleBodyWindow{dashboardBlock: dashboardBlock{lines: lines, targets: hits}, total: len(lines), prefix: len(lines)}
	}
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
	// Very short terminals fold the fixed toolbar into the scrollable body.
	if m.dashboardOptions().BodyTopRows == 0 {
		bar := m.rulesToolbar(width, m.filterToolbarRows(width))
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
	if m.rules.received && m.rules.snapshot.State != "ready" {
		addButton("rules.retry", m.tr("rules.retry_label"))
	}
	list := m.filteredRules()
	if _, err := m.ruleMatcher(); err != nil {
		addText(m.tr("rules.invalid_regex"), errorColor)
		return static()
	}
	if m.rules.received && m.rules.snapshot.State == "ready" && len(list) == 0 {
		message := m.tr("rules.empty")
		if m.rules.search.Value() != "" {
			message = m.tr("rules.no_matches")
		}
		addText(message, muted)
	}
	if len(list) == 0 {
		return static()
	}
	prefix := len(lines)
	numberWidth := 1
	for _, r := range m.rules.snapshot.Rules {
		numberWidth = max(numberWidth, len(strconv.Itoa(r.Index+1)))
	}
	firstTarget := len(hits)
	total := prefix
	for _, r := range list {
		h := m.ruleHeight(r, width, numberWidth)
		hits = append(hits, hitRegion{ID: "rule." + strconv.Itoa(r.Index), Y: total, W: width, H: h})
		total += h + 1
	}
	total--
	if height < 0 {
		start, height = 0, total
	} else {
		start = max(0, min(start, max(0, total-height)))
	}
	end := min(total, start+height)
	leading := lines
	lines = nil
	for i := max(0, start); i < min(prefix, end); i++ {
		lines = append(lines, leading[i])
	}
	for index, r := range list {
		target := hits[firstTarget+index]
		row, h := target.Y, target.H
		if row >= end || row+h+1 <= start {
			continue
		}
		if row+h > start {
			for offset, line := range m.ruleRows(r, width, numberWidth) {
				if row+offset >= start && row+offset < end {
					lines = append(lines, line)
				}
			}
		}
		if index < len(list)-1 && row+h >= start && row+h < end {
			lines = append(lines, themed(strings.Repeat("─", max(0, width)), divider, bg, false))
		}
	}
	return ruleBodyWindow{dashboardBlock: dashboardBlock{lines: lines, targets: hits}, start: start, total: total, prefix: prefix}
}

// Bubbles textinput owns editing and horizontal scrolling. The dashboard owns
// list scrolling; a small row renderer preserves its shared keyboard/mouse model
// and the core's order without introducing a second list viewport.
func (m Model) ruleRows(r mihomo.Rule, width, numberWidth int) []string {
	id := "rule." + strconv.Itoa(r.Index)
	surface, marker, secondMarker := bg, "  ", "  "
	markerColor := focus
	if m.focus == id {
		marker = "› "
		surface = panel
	}
	if m.hover == id {
		surface = hoverBackground
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
	payloadLines := []string{cellCrop(clean(payload), available)}
	expanded := m.rules.expanded == ruleIdentity(r)
	if expanded {
		payloadLines = strings.Split(wrapText(clean(payload), available), "\n")
	}
	var rows []string
	for i, line := range payloadLines {
		gutter := themed(marker, markerColor, surface, false) + themed(number, muted, surface, false)
		if i > 0 {
			gutter = themed(secondMarker, markerColor, surface, false) + themed(strings.Repeat(" ", indent-2), muted, surface, false)
		}
		rows = append(rows, themed(cellPad(gutter+themed(cellPad(line, available), primaryColor, surface, false), width), text, surface, false))
	}
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
	typeWidth := ruleTypeColumnWidth(proxy, available)
	meta := themed(cellPad(clean(r.Type), typeWidth), muted, surface, false) + themed(" → ", muted, surface, false) + themed(cellCrop(proxy, max(1, available-typeWidth-3)), proxyColor, surface, false)
	if r.Extra != nil && r.Extra.Disabled {
		// Keep the disabled state visible even when the target is long.
		badge := m.tr("rules.disabled_suffix")
		meta = cellCrop(meta, max(0, available-ansi.StringWidth(badge))) + themed(badge, warning, surface, false)
	}
	metaLines := []string{meta}
	if expanded {
		meta = themed(cellPad(clean(r.Type), max(typeWidth, ansi.StringWidth(clean(r.Type)))), muted, surface, false) + themed(" → ", muted, surface, false) + themed(proxy, proxyColor, surface, false)
		if r.Extra != nil && r.Extra.Disabled {
			meta += themed(m.tr("rules.disabled_suffix"), warning, surface, false)
		}
		metaLines = strings.Split(wrapText(meta, available), "\n")
	}
	for _, line := range metaLines {
		second := themed(secondMarker, markerColor, surface, false) + themed(strings.Repeat(" ", indent-2), muted, surface, false) + line
		rows = append(rows, themed(cellPad(second, width), text, surface, false))
	}
	if expanded {
		for _, line := range m.ruleEntryCountLines(r, available) {
			value := themed(secondMarker, markerColor, surface, false) + themed(strings.Repeat(" ", indent-2), muted, surface, false) + themed(line, muted, surface, false)
			rows = append(rows, themed(cellPad(value, width), text, surface, false))
		}
	}
	return rows
}

func (m Model) ruleHeight(r mihomo.Rule, width, numberWidth int) int {
	if m.rules.expanded == "" || m.rules.expanded != ruleIdentity(r) {
		return 2
	}
	available := max(1, width-(2+numberWidth+2)-1)
	payload := r.Payload
	if payload == "" {
		payload = m.tr("rules.empty_payload")
		if strings.EqualFold(r.Type, "Match") {
			payload = m.tr("rules.match_all")
		}
	}
	proxy := clean(r.Proxy)
	if proxy == "" {
		proxy = "--"
	}
	meta := cellPad(clean(r.Type), max(ruleTypeColumnWidth(proxy, available), ansi.StringWidth(clean(r.Type)))) + " → " + proxy
	if r.Extra != nil && r.Extra.Disabled {
		meta += m.tr("rules.disabled_suffix")
	}
	return len(strings.Split(wrapText(clean(payload), available), "\n")) + len(strings.Split(wrapText(meta, available), "\n")) + len(m.ruleEntryCountLines(r, available))
}
func ruleTypeColumnWidth(proxy string, available int) int {
	proxyWidth := min(ansi.StringWidth(proxy), max(1, available/2))
	return min(20, max(1, available-proxyWidth-3))
}

func (m Model) ruleEntryCountLines(r mihomo.Rule, width int) []string {
	if r.Size < 0 {
		return nil
	}
	return strings.Split(wrapText(m.tr("rules.entries", r.Size), width), "\n")
}
