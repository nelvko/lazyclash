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
		width -= ansi.StringWidth(m.ruleFilterButtons().lines[0]) + 2
	}
	return searchInputWidth(width)
}

func (m Model) rulesToolbar(width, rows int) dashboardBlock {
	if m.rules.view == "providers" {
		return m.ruleProvidersToolbar(width, rows)
	}
	if m.rules.view == "custom" {
		return m.customRulesToolbar(width, rows)
	}
	_, matchErr := m.ruleMatcher()
	b := m.searchToolbar("rules.search", m.tr("rules.search_placeholder"), m.rules.search,
		m.ruleFilterButtons(), width, width >= 68, matchErr != nil)
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
	if m.rules.view == "providers" {
		return m.ruleProvidersBodyWindow(width, start, height)
	}
	if m.rules.view == "custom" {
		return m.customRulesBodyWindow(width, start, height)
	}
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
		bar := m.rulesToolbar(width, filterToolbarRows(width))
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
	if ruleIdentity(r) == m.rules.selected {
		marker, secondMarker = "▌ ", "▌ "
		surface = panel
	}
	if m.hover == id {
		surface = hoverBackground
	}
	markerColor := accent
	if m.focus == id {
		marker, secondMarker, markerColor = "› ", "  ", focus
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
	proxyWidth := min(ansi.StringWidth(proxy), max(1, available/2))
	typeWidth := min(20, max(1, available-proxyWidth-3))
	meta := themed(cellPad(clean(r.Type), typeWidth), muted, surface, false) + themed(" → ", muted, surface, false) + themed(cellCrop(proxy, max(1, available-typeWidth-3)), proxyColor, surface, false)
	if r.Extra != nil && r.Extra.Disabled {
		// Keep the disabled state visible even when the target is long.
		badge := m.tr("rules.disabled_suffix")
		meta = cellCrop(meta, max(0, available-ansi.StringWidth(badge))) + themed(badge, warning, surface, false)
	}
	metaLines := []string{meta}
	if expanded {
		meta = themed(clean(r.Type), muted, surface, false) + themed(" → ", muted, surface, false) + themed(proxy, proxyColor, surface, false)
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
		for _, line := range m.ruleObservationLines(r, available) {
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
	meta := clean(r.Type) + " → " + proxy
	if r.Extra != nil && r.Extra.Disabled {
		meta += m.tr("rules.disabled_suffix")
	}
	return len(strings.Split(wrapText(clean(payload), available), "\n")) + len(strings.Split(wrapText(meta, available), "\n")) + len(m.ruleObservationLines(r, available))
}
func (m Model) ruleObservationLines(r mihomo.Rule, width int) []string {
	var fields []string
	if r.Size >= 0 {
		fields = append(fields, fmt.Sprintf(m.tr("rules.entries"), r.Size))
	}
	if r.Extra != nil {
		state := m.tr("rules.enabled")
		if r.Extra.Disabled {
			state = m.tr("rules.disabled")
		}
		fields = append(fields, m.tr("rules.state", state), fmt.Sprintf(m.tr("rules.hits"), r.Extra.HitCount, r.Extra.MissCount))
	}
	if len(fields) == 0 {
		return nil
	}
	return strings.Split(wrapText(clean(strings.Join(fields, " · ")), width), "\n")
}
