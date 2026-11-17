package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type logPosition struct {
	entry uint64
	row   int
}

type logDocument struct {
	lines     []string
	positions []logPosition
	matched   int
}

func logLevel(level string) (string, string) {
	switch strings.ToLower(level) {
	case "error", "fatal", "panic":
		return strings.ToUpper(level), errorColor
	case "warn", "warning":
		return "WARN", warning
	case "info":
		return "INFO", info
	default:
		return strings.ToUpper(level), muted
	}
}

func (m Model) logMatches(entry mihomo.LogEntry) bool {
	match, err := m.logMatcher()
	return err == nil && match(entry)
}

func (m Model) logMatcher() (func(mihomo.LogEntry) bool, error) {
	match, err := compileSearchMatcher(m.logs.search.Value(), m.logs.caseSensitive, m.logs.wholeWord, m.logs.regex)
	if err != nil {
		return nil, err
	}
	return func(entry mihomo.LogEntry) bool {
		level, _ := logLevel(entry.Level)
		if m.logs.level != 0 && level != []string{"", "INFO", "WARN", "ERROR"}[m.logs.level] {
			// Fatal events belong to the error filter as well.
			if m.logs.level != 3 || level != "FATAL" && level != "PANIC" {
				return false
			}
		}
		return match(entry.Level + " " + entry.Message)
	}, nil
}

func (m Model) logDocument(width int) logDocument {
	width = max(1, width)
	var doc logDocument
	match, err := m.logMatcher()
	if err != nil {
		return doc
	}
	entries, first := m.logsDisplayEntries()
	for index, entry := range entries {
		if !match(entry) {
			continue
		}
		doc.matched++
		stamp := "--:--:--"
		if !entry.Time.IsZero() {
			stamp = entry.Time.Local().Format("15:04:05")
		}
		level, color := logLevel(entry.Level)
		message := entry.Message
		var rows []string
		prefix := themed(stamp+"  ", muted, bg, false) + themed(cellPad(level, 5)+"  ", color, bg, false)
		if m.logs.compact {
			message = strings.ReplaceAll(strings.ReplaceAll(message, "\r", " "), "\n", " ")
			rows = []string{cellCrop(prefix+themed(message, text, bg, false), width)}
		} else {
			prefixWidth := ansi.StringWidth(prefix)
			if width >= prefixWidth+12 {
				for index, line := range strings.Split(wrapText(themed(message, text, bg, false), width-prefixWidth), "\n") {
					gutter := prefix
					if index > 0 {
						gutter = themed(strings.Repeat(" ", prefixWidth), muted, bg, false)
					}
					rows = append(rows, gutter+line)
				}
			} else {
				rows = append(rows, cellCrop(prefix, width))
				rows = append(rows, strings.Split(wrapText(themed(message, text, bg, false), width), "\n")...)
			}
		}
		for row, value := range rows {
			doc.lines = append(doc.lines, value)
			doc.positions = append(doc.positions, logPosition{entry: first + uint64(index), row: row})
		}
	}
	return doc
}

func (m Model) logViewport(doc logDocument, width, height int) viewport.Model {
	v := viewport.New(viewport.WithWidth(max(1, width)), viewport.WithHeight(max(1, height)))
	v.SetContentLines(append([]string(nil), doc.lines...))
	if !m.logs.holding {
		v.GotoBottom()
		return v
	}
	for index, position := range doc.positions {
		if position.entry < m.logs.anchor.entry {
			continue
		}
		// Reflow or Compact mode may shorten the anchored entry. Keep the
		// same entry and clamp its row instead of skipping to the next one.
		for index+1 < len(doc.positions) && doc.positions[index+1].entry == m.logs.anchor.entry && doc.positions[index+1].row <= m.logs.anchor.row {
			index++
		}
		v.SetYOffset(index)
		return v
	}
	v.GotoBottom()
	return v
}

func (m Model) logControls() dashboardBlock {
	var b dashboardBlock
	buttons := ""
	for _, item := range []struct{ id, label string }{
		{"logs.level", m.tr([]string{"logs.level_all", "logs.level_info", "logs.level_warn", "logs.level_error"}[m.logs.level])},
		{"logs.view", m.tr(map[bool]string{false: "logs.detailed", true: "logs.compact"}[m.logs.compact])},
	} {
		o := m.searchControlOptions(item.id)
		o.Kind = buttonText
		keys := []string{"logs.level_all", "logs.level_info", "logs.level_warn", "logs.level_error"}
		if item.id == "logs.view" {
			keys = []string{"logs.detailed", "logs.compact"}
		}
		for _, key := range keys {
			o.Width = max(o.Width, ansi.StringWidth(m.tr(key)))
		}
		o.Disabled = m.busy || m.quitting
		value := renderButton(item.label, o)
		if !o.Disabled {
			b.targets = append(b.targets, hitRegion{ID: item.id, X: ansi.StringWidth(buttons), W: ansi.StringWidth(value), H: 1})
		}
		buttons += value
	}
	b.lines = []string{buttons}
	return b
}

func (m Model) logSearchButtons() dashboardBlock {
	return m.searchMatchingButtons("logs", m.logs.caseSensitive, m.logs.wholeWord, m.logs.regex)
}

// A zero controlsX places the options below the complete input frame.
func (m Model) logSearchGeometry(width int) (fieldWidth, controlsX int) {
	width = max(1, width)
	fieldWidth = min(inputFieldMaxWidth, width-searchControlWidth(m.logControls())-2)
	if fieldWidth >= 24 && inputFieldInlineTrailing(fieldWidth, searchControlWidth(m.logSearchButtons()), true) {
		return fieldWidth, fieldWidth + 2
	}
	return min(inputFieldMaxWidth, width), 0
}

func (m Model) logSearchWidth(width int) int {
	fieldWidth, _ := m.logSearchGeometry(width)
	return inputFieldTextWidth(fieldWidth, searchControlWidth(m.logSearchButtons()), true)
}

func (m Model) logFilterRows(width int) int {
	fieldWidth, controlsX := m.logSearchGeometry(width)
	rows := inputFieldRows(fieldWidth, searchControlWidth(m.logSearchButtons()), true)
	if controlsX == 0 {
		rows++
	}
	return rows
}

func (m Model) logFilterBar(width int) dashboardBlock {
	fieldWidth, controlsX := m.logSearchGeometry(width)
	_, err := m.logMatcher()
	b := m.searchToolbar("logs.search", m.tr("logs.placeholder"), m.logs.search,
		m.logSearchButtons(), fieldWidth, err != nil)
	controls := m.logControls()
	controlsY := 1
	if controlsX == 0 {
		controlsY = len(b.lines)
		b.lines = append(b.lines, cellCrop(controls.lines[0], width))
	} else {
		b.lines[1] += themed("  ", text, bg, false) + controls.lines[0]
	}
	for _, target := range controls.targets {
		target.X += controlsX
		target.Y += controlsY
		b.targets = append(b.targets, target)
	}
	return b
}

func (m Model) logsToolbar(width, rows int) dashboardBlock {
	width = max(1, width)
	b := m.logFilterBar(width)
	count := 0
	entries, _ := m.logsDisplayEntries()
	match, matchErr := m.logMatcher()
	for _, entry := range entries {
		if matchErr == nil && match(entry) {
			count++
		}
	}
	status := m.tr("logs.summary", count, len(entries))
	hint := ""
	if m.logs.editing {
		hint = m.tr("logs.search_edit")
	} else if !m.logs.observedAt.IsZero() {
		hint = m.tr("logs.observed", m.logs.observedAt.Local().Format("15:04:05"))
	}
	for _, item := range []struct{ id, key string }{{"logs.level", "logs.level_hint"}, {"logs.view", "logs.view_hint"}, {"logs.reset-search", "logs.reset_hint"},
		{"logs.case", "logs.filter_case"}, {"logs.word", "logs.filter_word"}, {"logs.regex", "logs.filter_regex"}} {
		if m.hover == item.id || m.focus == item.id {
			hint = m.tr(item.key)
			break
		}
	}
	if hint != "" && ansi.StringWidth(status+" · "+hint) <= width {
		status += " · " + hint
	}
	b.lines = append(b.lines, themed(cellCrop(status, width), muted, bg, false))
	if matchErr != nil {
		b.lines = append(b.lines, themed(cellCrop(m.tr("logs.invalid_regex"), width), errorColor, bg, false))
	}
	b.lines = b.lines[:min(rows, len(b.lines))]
	var targets []hitRegion
	for _, target := range b.targets {
		if clipped, ok := clipRegion(target, 0, 0, width, len(b.lines)); ok && !m.busy {
			targets = append(targets, clipped)
		}
	}
	b.targets = targets
	return b
}

func (m Model) logsPrefix(width int) dashboardBlock {
	var b dashboardBlock
	if m.dashboardOptions().BodyTopRows == 0 {
		b = m.logsToolbar(width, m.filterToolbarRows(width))
	}
	state, status := m.corePageState()
	if state != "" {
		b.lines = append(b.lines, themed(cellCrop(status, width), muted, bg, false))
		if entries, _ := m.logsDisplayEntries(); len(entries) > 0 {
			b.lines = append(b.lines, themed(cellCrop(m.tr("logs.ended"), width), muted, bg, false))
		}
	}
	if m.logs.err != nil && state == "" {
		value := m.tr("logs.unavailable", m.localizeError(m.logs.err))
		lines := strings.Split(wrapText(themed(value, errorColor, bg, false), max(1, width)), "\n")
		b.lines = append(b.lines, lines[:min(2, len(lines))]...)
	}
	if m.logs.recovered && !m.logs.reconnecting && state == "" {
		b.lines = append(b.lines, themed(cellCrop(m.tr("logs.recovered"), width), muted, bg, false))
	}
	return b
}

func (m Model) logsCapacity(width int) int {
	return max(1, dashboardGeometry(m.dashboardOptions()).height-len(m.logsPrefix(width).lines)-1)
}

func (m Model) logsStatus() (string, string) {
	if !m.snapshotReady {
		if m.busy {
			return m.tr("empty.loading"), muted
		}
		return m.tr("empty.read_failed"), errorColor
	}
	if state, status := m.corePageState(); state != "" {
		return status, muted
	}
	if m.logs.reconnecting {
		return m.tr("logs.reconnecting"), warning
	}
	if m.logs.paused {
		return m.tr("logs.display_paused"), warning
	}
	if !m.logs.active {
		return m.tr("logs.opening"), muted
	}
	if len(m.logs.entries) == 0 {
		return m.tr("logs.waiting"), info
	}
	if m.logs.holding {
		return m.tr("logs.browsing"), info
	}
	return m.tr("logs.capturing"), info
}

func (m Model) logsBody(width int) (string, []hitRegion) {
	width = max(1, width)
	doc := m.logDocument(width)
	b := m.logsPrefix(width)
	if len(doc.lines) == 0 {
		// Empty-state diagnostics belong with the centered message, rather than
		// appearing a second time above an otherwise empty viewport.
		b = dashboardBlock{}
		if m.dashboardOptions().BodyTopRows == 0 {
			b = m.logsToolbar(width, m.filterToolbarRows(width))
		}
	}
	height := dashboardGeometry(m.dashboardOptions()).height
	// Keep the input and external controls together before spending a row
	// on the footer. A tiny terminal still needs one row for log content.
	prefixLimit := max(0, height-2)
	if m.dashboardOptions().BodyTopRows == 0 {
		controlsRows := m.logFilterRows(width)
		if height > controlsRows {
			prefixLimit = max(prefixLimit, controlsRows)
		}
	}
	if len(b.lines) > prefixLimit {
		b.lines = b.lines[:prefixLimit]
		visible := b.targets[:0]
		for _, target := range b.targets {
			if clipped, ok := clipRegion(target, 0, 0, width, len(b.lines)); ok {
				visible = append(visible, clipped)
			}
		}
		b.targets = visible
	}
	capacity := max(1, height-len(b.lines)-1)
	if len(doc.lines) == 0 {
		empty := m.emptyState(width, capacity, m.logsEmptyState())
		for _, target := range empty.targets {
			target.Y += len(b.lines)
			b.targets = append(b.targets, target)
		}
		doc.lines = empty.lines
	}
	v := m.logViewport(doc, width, capacity)
	b.targets = append(b.targets, hitRegion{ID: "logs.list", Y: len(b.lines), W: width, H: capacity})
	b.lines = append(b.lines, strings.Split(v.View(), "\n")...)
	if len(b.lines) < height {
		status, color := m.logsStatus()
		label := m.tr("logs.latest")
		if (m.logs.holding || m.logs.paused) && m.logs.unread > 0 {
			label += fmt.Sprintf(" +%d", m.logs.unread)
		}
		if width < 60 {
			label = "↓"
			if (m.logs.holding || m.logs.paused) && m.logs.unread > 0 {
				label += fmt.Sprintf(" +%d", m.logs.unread)
			}
		}
		o := dashboardButtonOptions("logs.latest", m.navigationOptions(), false, bg)
		o.Disabled = m.busy
		button := renderButton(label, o)
		buttonWidth := min(width, ansi.StringWidth(button))
		line := themed(cellPad(status, max(0, width-buttonWidth-1)), color, bg, false)
		if buttonWidth < width {
			line += themed(" ", text, bg, false)
		}
		if !m.busy {
			b.targets = append(b.targets, hitRegion{ID: "logs.latest", X: width - buttonWidth, Y: len(b.lines), W: buttonWidth, H: 1})
		}
		b.lines = append(b.lines, line+cellCrop(button, buttonWidth))
	}
	return strings.Join(b.lines, "\n"), b.targets
}

func (m Model) logsEmptyState() emptyStateOptions {
	entries, _ := m.logsDisplayEntries()
	if len(entries) == 0 {
		if core, ok := m.coreEmptyState(); ok {
			if core.ActionID != "snapshot.retry" {
				// The page header owns the installed core's recovery controls;
				// the initial snapshot failure has a separate recovery action.
				core.ActionID, core.ActionLabel = "", ""
			}
			return core
		}
	}
	if state, _ := m.corePageState(); m.logs.err != nil && state == "" {
		// Reconnect remains in the page header instead of adding a duplicate
		// target to the empty viewport.
		return emptyStateOptions{Title: m.tr("empty.read_failed"), Detail: m.localizeError(m.logs.err), Error: true}
	}
	recovered := ""
	if state, _ := m.corePageState(); m.logs.recovered && !m.logs.reconnecting && state == "" {
		recovered = m.tr("logs.recovered")
	}
	if _, err := m.logMatcher(); err != nil {
		return emptyStateOptions{Title: m.tr("logs.invalid_regex"), Detail: recovered, Error: true,
			ActionID: "logs.reset-filters", ActionLabel: m.tr("empty.clear_filters")}
	}
	if strings.TrimSpace(m.logs.search.Value()) != "" || m.logs.level != 0 {
		state := emptyStateOptions{Title: m.tr("logs.no_matches"), Detail: recovered, ActionID: "logs.reset-filters", ActionLabel: m.tr("empty.clear_filters")}
		if core, ok := m.coreEmptyState(); ok && len(entries) > 0 {
			state.Detail = core.Title + " · " + m.tr("logs.ended")
		}
		return state
	}
	if m.logs.paused {
		return emptyStateOptions{Title: m.tr("logs.display_paused"), Detail: recovered}
	}
	if m.logs.reconnecting {
		return emptyStateOptions{Title: m.tr("logs.reconnecting"), Detail: m.tr("empty.logs.reconnect")}
	}
	if !m.logs.active {
		return emptyStateOptions{Title: m.tr("logs.opening"), Detail: recovered}
	}
	detail := m.tr("empty.logs.waiting")
	if recovered != "" {
		detail = recovered + " · " + detail
	}
	return emptyStateOptions{Title: m.tr("empty.logs.none"), Detail: detail}
}
