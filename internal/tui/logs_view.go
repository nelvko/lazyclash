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

func (m Model) logControl(id string) dashboardBlock {
	keys := logLevelKeys[:]
	label := m.tr(logLevelKeys[m.logs.level]) + " ▾"
	if id == "logs.view" {
		keys = []string{"logs.detailed", "logs.compact"}
		label = m.tr(map[bool]string{false: "logs.detailed", true: "logs.compact"}[m.logs.compact])
	}
	o := m.searchControlOptions(id)
	o.Kind = buttonText
	for _, key := range keys {
		value := m.tr(key)
		if id == "logs.level" {
			value += " ▾"
		}
		o.Width = max(o.Width, ansi.StringWidth(value))
	}
	o.Disabled = m.busy || m.quitting
	value := renderButton(label, o)
	b := dashboardBlock{lines: []string{value}}
	if !o.Disabled {
		b.targets = []hitRegion{{ID: id, W: ansi.StringWidth(value), H: 1}}
	}
	return b
}

func (m Model) logSearchButtons() dashboardBlock {
	return m.searchMatchingButtons("logs", m.logs.caseSensitive, m.logs.wholeWord, m.logs.regex)
}

func (m Model) logFilterActive() bool {
	return strings.TrimSpace(m.logs.search.Value()) != "" || m.logs.level != 0
}

func (m Model) logCount() (full, compact string) {
	if !m.logFilterActive() {
		return "", ""
	}
	entries, _ := m.logsDisplayEntries()
	matched := 0
	match, err := m.logMatcher()
	for _, entry := range entries {
		if err == nil && match(entry) {
			matched++
		}
	}
	return m.tr("logs.filtered_count", matched, len(entries)), fmt.Sprintf("%d / %d", matched, len(entries))
}

// Reserve the bounded cache's maximum ratio even while hidden. Incoming events
// and entering or clearing a query must not resize the search frame.
func (m Model) logCountWidth() int {
	return ansi.StringWidth(m.tr("logs.filtered_count", retainedLogLimit, retainedLogLimit))
}

func (m Model) logSearchToolbar() listSearchToolbar {
	return listSearchToolbar{scope: m.logControl("logs.level"), functions: m.logControl("logs.view"),
		countWidth: m.logCountWidth(), matchingWidth: searchControlWidth(m.logSearchButtons())}
}

func (m Model) logSearchWidth(width int) int {
	fieldWidth, _ := m.logSearchToolbar().searchGeometry(width)
	return inputFieldTextWidth(fieldWidth, searchControlWidth(m.logSearchButtons()), true)
}

func (m Model) logFilterRows(width int) int {
	return m.logSearchToolbar().rows(width, m.logFilterActive())
}

func (m Model) logFilterBar(width int) dashboardBlock {
	bar := m.logSearchToolbar()
	fieldWidth, _ := bar.searchGeometry(width)
	_, err := m.logMatcher()
	search := m.searchToolbar("logs.search", m.tr("logs.placeholder"), m.logs.search,
		m.logSearchButtons(), fieldWidth, err != nil)
	count, compactCount := m.logCount()
	return bar.render(search, width, count, compactCount)
}

func (m Model) logsToolbar(width, rows int) dashboardBlock {
	width = max(1, width)
	b := m.logFilterBar(width)
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
		prefixLimit = max(prefixLimit, min(controlsRows, max(0, height-1)))
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
