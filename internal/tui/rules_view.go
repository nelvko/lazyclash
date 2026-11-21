package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func (m *Model) ruleFilterButtons() dashboardBlock {
	return m.searchMatchingButtons("rules", m.rules.caseSensitive, m.rules.wholeWord, m.rules.regex)
}

func (m *Model) ruleSearchWidth(width int) int {
	return inputFieldTextWidth(m.ruleSearchFieldWidth(width), searchControlWidth(m.ruleFilterButtons()), true)
}

func (m *Model) ruleSearchFieldWidth(width int) int {
	if m.rules.received && m.rules.snapshot.State == "ready" {
		// Reserve the widest filtered count so typing does not move the frame.
		countWidth := 2*len(strconv.Itoa(len(m.rules.snapshot.Rules))) + 3
		if width-countWidth-2 >= 12 {
			width -= countWidth + 2
		}
	}
	return min(inputFieldMaxWidth, max(1, width))
}

func (m *Model) ruleCount() string {
	if !m.rules.received || m.rules.snapshot.State != "ready" {
		return ""
	}
	total := len(m.rules.snapshot.Rules)
	if strings.TrimSpace(m.rules.search.Value()) == "" {
		return m.tr("rules.count", total)
	}
	return m.tr("rules.filtered_count", len(m.filteredRules()), total)
}

func (m *Model) rulesToolbar(width, rows int) dashboardBlock {
	_, matchErr := m.ruleMatcher()
	fieldWidth := m.ruleSearchFieldWidth(width)
	b := m.searchToolbar("rules.search", m.tr("rules.search_placeholder"), m.rules.search,
		m.ruleFilterButtons(), fieldWidth, matchErr != nil)
	count := m.ruleCount()
	if count != "" && fieldWidth+2+ansi.StringWidth(count) <= width {
		b.lines[1] += themed("  "+count, muted, bg, false)
	}
	for _, item := range []struct{ id, key string }{{"rules.case", "rules.filter_case"}, {"rules.word", "rules.filter_word"}, {"rules.regex", "rules.filter_regex"}, {"rules.clear", "rules.filter_clear"}} {
		if matchErr == nil && (m.focus == item.id || m.hover == item.id) {
			hint := " · " + m.tr(item.key)
			if ansi.StringWidth(b.lines[1])+ansi.StringWidth(hint) <= width {
				b.lines[1] += themed(hint, accent, bg, false)
			}
			break
		}
	}
	if matchErr != nil {
		b.lines = append(b.lines, themed(cellCrop(m.tr("rules.invalid_regex"), width), errorColor, bg, false))
	}
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

func (m *Model) rulesBody(width int) (string, []hitRegion) {
	body := m.rulesBodyWindow(width, 0, -1)
	return strings.Join(body.lines, "\n"), body.targets
}

func (m *Model) rulesTargets(width int) []hitRegion {
	return m.rulesBodyWindow(width, 0, 0).targets
}

func (m *Model) rulesListPrefix(width int) int {
	return m.rulesBodyWindow(width, 0, 0).prefix
}

// Geometry covers the complete list; only rows intersecting the requested
// window are styled. A negative height requests the complete body for export.
func (m *Model) rulesBodyWindow(width, start, height int) ruleBodyWindow {
	var lines []string
	var hits []hitRegion
	static := func() ruleBodyWindow {
		return ruleBodyWindow{dashboardBlock: dashboardBlock{lines: lines, targets: hits}, total: len(lines), prefix: len(lines)}
	}
	addText := func(value, color string) {
		lines = append(lines, strings.Split(wrapText(themed(clean(value), color, bg, false), max(1, width)), "\n")...)
	}
	// Very short terminals fold the fixed toolbar into the scrollable body.
	if m.dashboardOptions().BodyTopRows == 0 {
		bar := m.rulesToolbar(width, m.filterToolbarRows(width))
		lines, hits = bar.lines, bar.targets
	}
	list := m.filteredRules()
	if len(list) == 0 {
		state := m.rulesEmptyState()
		available := max(1, dashboardGeometry(m.dashboardOptions()).height-len(lines))
		body := m.emptyState(width, available, state)
		for _, hit := range body.targets {
			hit.Y += len(lines)
			hits = append(hits, hit)
		}
		lines = append(lines, body.lines...)
		return static()
	}
	// Keep usable rules and their read diagnostic together after a refresh
	// failure; a failed observation must not turn existing rows into an empty list.
	if m.rules.err != nil {
		addText(m.localizeError(m.rules.err), errorColor)
	} else if m.rules.snapshot.Error != "" {
		addText(m.locale.Diagnostic(m.rules.snapshot.Error, m.rules.snapshot.Diagnostic), errorColor)
	}
	prefix := len(lines)
	numberWidth := m.ruleNumberWidth()
	firstTarget := len(hits)
	total := prefix
	for _, r := range list {
		h := m.ruleHeight(r, width, numberWidth)
		hits = append(hits, hitRegion{ID: "rule." + strconv.Itoa(r.Index), Y: total, W: width, H: h})
		total += h
	}
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
	var links []hitRegion
	for index, r := range list {
		target := hits[firstTarget+index]
		row, h := target.Y, target.H
		if row >= end || row+h <= start {
			continue
		}
		block := m.ruleRowBlock(r, width, numberWidth, m.rules.expanded == ruleIdentity(r))
		lines = append(lines, block.lines[max(0, start-row):min(h, end-row)]...)
		for _, link := range block.targets {
			link.Y += row
			if clipped, ok := clipRegion(link, 0, start, width, end-start); ok {
				links = append(links, clipped)
			}
		}
	}
	// Links take precedence over the containing row for mouse hits. A
	// geometry-only pass (height zero) still has one keyboard target per row.
	return ruleBodyWindow{dashboardBlock: dashboardBlock{lines: lines, targets: append(links, hits...)}, start: start, total: total, prefix: prefix}
}

func (m *Model) rulesEmptyState() emptyStateOptions {
	coreState := func() (emptyStateOptions, bool) {
		state, ok := m.coreEmptyState()
		if ok && state.Error && state.ActionID == "" {
			state.ActionID, state.ActionLabel = "rules.retry", m.tr("rules.retry_label")
		}
		return state, ok
	}
	// Snapshot errors take priority over search, including a ready snapshot
	// whose refresh failed before it returned any rows.
	detail := ""
	if m.rules.err != nil {
		detail = m.localizeError(m.rules.err)
	} else if m.rules.snapshot.Error != "" {
		detail = m.locale.Diagnostic(m.rules.snapshot.Error, m.rules.snapshot.Diagnostic)
	}
	if detail != "" {
		if state, ok := coreState(); ok {
			return state
		}
		return emptyStateOptions{Title: m.tr("empty.rules.failed"), Detail: detail, Error: true,
			ActionID: "rules.retry", ActionLabel: m.tr("rules.retry_label")}
	}
	if !m.rules.received || m.rules.snapshot.State != "ready" || len(m.rules.snapshot.Rules) == 0 {
		if state, ok := coreState(); ok {
			return state
		}
	}
	if !m.rules.received {
		return emptyStateOptions{Title: m.tr("rules.loading"), Detail: m.tr("rules.description")}
	}
	if m.rules.snapshot.State != "ready" {
		title := m.rulesStatus()
		if m.rules.snapshot.State == "unavailable" {
			title = m.tr("empty.rules.failed")
		}
		return emptyStateOptions{Title: title, Error: true,
			ActionID: "rules.retry", ActionLabel: m.tr("rules.retry_label")}
	}
	if _, err := m.ruleMatcher(); err != nil {
		return emptyStateOptions{Title: m.tr("rules.invalid_regex"), Detail: m.tr("empty.rules.invalid_search"), Error: true,
			ActionID: "rules.empty.clear", ActionLabel: m.tr("empty.rules.clear_search")}
	}
	if len(m.rules.snapshot.Rules) > 0 && strings.TrimSpace(m.rules.search.Value()) != "" {
		return emptyStateOptions{Title: m.tr("rules.no_matches"),
			ActionID: "rules.empty.clear", ActionLabel: m.tr("empty.rules.clear_search")}
	}
	return emptyStateOptions{Title: m.tr("rules.empty")}
}

func (m *Model) ruleNumberWidth() int {
	width := 1
	for _, r := range m.rules.snapshot.Rules {
		width = max(width, len(strconv.Itoa(r.Index+1)))
	}
	return width
}

// Compare the focused row's rendered text so expansion follows actual clipping,
// including narrow metadata, full-width characters, and optional entry counts.
func (m *Model) ruleCanExpand(r mihomo.Rule) bool {
	width, numberWidth := dashboardBodyWidth(m.dashboardOptions()), m.ruleNumberWidth()
	collapsed := ansi.Strip(strings.Join(m.ruleRowBlock(r, width, numberWidth, false).lines, "\n"))
	expanded := ansi.Strip(strings.Join(m.ruleRowBlock(r, width, numberWidth, true).lines, "\n"))
	return collapsed != expanded
}

func (m *Model) ruleRows(r mihomo.Rule, width, numberWidth int) []string {
	return m.ruleRowBlock(r, width, numberWidth, m.rules.expanded == ruleIdentity(r)).lines
}

// Bubbles textinput owns editing and horizontal scrolling. The dashboard owns
// list scrolling; a small row renderer preserves its shared keyboard/mouse model
// and the core's order without introducing a second list viewport.
func (m *Model) ruleRowBlock(r mihomo.Rule, width, numberWidth int, expanded bool) dashboardBlock {
	index := strconv.Itoa(r.Index)
	id := "rule." + index
	surface, marker := bg, "  "
	if m.focus == id {
		marker = "› "
		surface = panel
	}
	if m.hover == id || m.hover == ruleProxyActionPrefix+index {
		surface = hoverBackground
	}
	if columns, ok := ruleTableColumns(width, numberWidth); ok {
		return m.ruleTableRows(r, width, numberWidth, columns, surface, marker, expanded)
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
	if expanded {
		payloadLines = strings.Split(wrapText(clean(payload), available), "\n")
	}
	var rows []string
	for i, line := range payloadLines {
		gutter := strings.Repeat(" ", indent)
		if i == 0 {
			gutter = themed(marker, focus, surface, false) + themed(number, muted, surface, false)
		}
		rows = append(rows, themed(cellPad(gutter+themed(line, primaryColor, surface, false), width), text, surface, false))
	}
	proxy := clean(r.Proxy)
	if proxy == "" {
		proxy = "--"
	}
	ruleType := clean(r.Type)
	typeWidth := ruleTypeColumnWidth(proxy, available)
	if expanded {
		typeWidth = max(typeWidth, ansi.StringWidth(ruleType))
	}
	meta := m.ruleProxyBlock(r, proxy, themed(cellPad(ruleType, typeWidth)+" → ", muted, surface, false), available, expanded, surface)
	for i, line := range meta.lines {
		meta.lines[i] = themed(cellPad(strings.Repeat(" ", indent)+line, width), text, surface, false)
	}
	for i := range meta.targets {
		meta.targets[i].X += indent
	}
	block := dashboardBlock{lines: rows}
	block.append(meta)
	if expanded {
		for _, line := range m.ruleEntryCountLines(r, available) {
			block.lines = append(block.lines, themed(cellPad(strings.Repeat(" ", indent)+line, width), muted, surface, false))
		}
	}
	return block
}

func (m *Model) ruleHeight(r mihomo.Rule, width, numberWidth int) int {
	if m.rules.expanded != "" && m.rules.expanded == ruleIdentity(r) {
		return len(m.ruleRows(r, width, numberWidth))
	}
	if _, table := ruleTableColumns(width, numberWidth); table {
		return 1
	}
	return 2
}

// Keep the payload flexible while aligning rule types and targets across rows.
// Narrow screens retain the two-line reading order instead of tiny columns.
func ruleTableColumns(width, numberWidth int) ([3]int, bool) {
	available := width - (2 + numberWidth + 2) - 1
	proxy := min(24, max(16, available/4))
	columns := [3]int{available - 20 - proxy - 5, 20, proxy}
	return columns, width >= 80 && columns[0] >= 20
}

func (m *Model) ruleTableRows(r mihomo.Rule, width, numberWidth int, columns [3]int, surface, marker string, expanded bool) dashboardBlock {
	payload := clean(r.Payload)
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
	payloadColor := text
	if r.Extra != nil && r.Extra.Disabled {
		payloadColor = muted
	}
	target := m.ruleProxyBlock(r, proxy, "", columns[2], expanded, surface)
	values := []string{themed(payload, payloadColor, surface, false), themed(clean(r.Type), muted, surface, false)}
	cells := [3][]string{nil, nil, target.lines}
	height := len(target.lines)
	for i, value := range values {
		cells[i] = []string{cellCrop(value, columns[i])}
		if expanded {
			cells[i] = strings.Split(wrapText(value, columns[i]), "\n")
		}
		height = max(height, len(cells[i]))
	}
	indent := 2 + numberWidth + 2
	rows := make([]string, 0, height+1)
	for row := range height {
		gutter := themed(strings.Repeat(" ", indent), muted, surface, false)
		if row == 0 {
			gutter = themed(marker, focus, surface, false) + themed(fmt.Sprintf("%*d  ", numberWidth, r.Index+1), muted, surface, false)
		}
		line := gutter
		for i, column := range cells {
			value := ""
			if row < len(column) {
				value = column[row]
			}
			line += cellPad(value, columns[i])
			if i == 0 {
				line += "  "
			} else if i == 1 {
				separator := "   "
				if row == 0 {
					separator = " → "
				}
				line += themed(separator, muted, surface, false)
			}
		}
		rows = append(rows, themed(cellPad(line, width), text, surface, false))
	}
	if expanded {
		for _, line := range m.ruleEntryCountLines(r, width-indent-1) {
			rows = append(rows, themed(cellPad(strings.Repeat(" ", indent)+line, width), muted, surface, false))
		}
	}
	for i := range target.targets {
		target.targets[i].X += indent + columns[0] + 2 + columns[1] + 3
	}
	return dashboardBlock{lines: rows, targets: target.targets}
}

// The target name owns its painted cells. Keeping it separate from the type,
// arrow and badge also gives each wrapped fragment its own mouse region.
func (m *Model) ruleProxyBlock(r mihomo.Rule, label, prefix string, width int, expanded bool, surface string) dashboardBlock {
	color := accent
	switch strings.ToUpper(r.Proxy) {
	case "DIRECT":
		color = success
	case "REJECT", "REJECT-DROP":
		color = errorColor
	}
	badge := ""
	if r.Extra != nil && r.Extra.Disabled {
		badge = m.tr("rules.disabled_suffix")
	}
	block := dashboardBlock{}
	labelWidth := width - ansi.StringWidth(prefix)
	var labels []string
	if expanded {
		if prefix != "" && labelWidth < min(2, ansi.StringWidth(label)) {
			block.lines = strings.Split(wrapText(prefix, width), "\n")
			prefix, labelWidth = "", width
		}
		labels = strings.Split(ansi.Hardwrap(label, labelWidth, true), "\n")
		for i := range labels {
			labels[i] = cellCrop(labels[i], labelWidth)
		}
	} else {
		// Reserve space for the disabled marker before clipping the label.
		room := max(0, width-ansi.StringWidth(badge))
		prefix = cellCrop(prefix, room)
		labels = []string{cellCrop(label, room-ansi.StringWidth(prefix))}
	}
	x := ansi.StringWidth(prefix)
	_, linked := m.ruleProxyGroup(r.Proxy)
	action := ruleProxyActionPrefix + strconv.Itoa(r.Index)
	for i, line := range labels {
		if i > 0 {
			prefix = strings.Repeat(" ", x)
		}
		if cells := ansi.StringWidth(line); linked && cells > 0 {
			block.targets = append(block.targets, hitRegion{ID: action, X: x, Y: len(block.lines), W: cells, H: 1})
			if m.hover == action {
				line = ansi.NewStyle().Underline(true).Styled(line)
			}
		}
		block.lines = append(block.lines, prefix+themed(line, color, surface, false))
	}
	last := len(block.lines) - 1
	remaining := width - ansi.StringWidth(block.lines[last])
	if expanded && ansi.StringWidth(badge) > remaining {
		block.lines = append(block.lines, strings.Split(wrapText(themed(strings.TrimSpace(badge), warning, surface, false), width), "\n")...)
	} else {
		block.lines[last] += themed(cellCrop(badge, remaining), warning, surface, false)
	}
	return block
}

func ruleTypeColumnWidth(proxy string, available int) int {
	proxyWidth := min(ansi.StringWidth(proxy), max(1, available/2))
	return min(20, max(1, available-proxyWidth-3))
}

func (m *Model) ruleEntryCountLines(r mihomo.Rule, width int) []string {
	if r.Size < 0 {
		return nil
	}
	return strings.Split(wrapText(m.tr("rules.entries", r.Size), width), "\n")
}
