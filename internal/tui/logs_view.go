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
	case "gap":
		return "GAP", warning
	case "info":
		return "INFO", info
	default:
		return strings.ToUpper(level), muted
	}
}

func (m Model) logMessage(entry mihomo.LogEntry) string {
	if entry.Level == "gap" && entry.Message == logCaptureGap {
		return m.tr(logCaptureGap)
	}
	return entry.Message
}

func (m Model) logMatches(entry mihomo.LogEntry) bool {
	// Gaps describe the observation itself and must survive display filters.
	if entry.Level == "gap" {
		return true
	}
	level, _ := logLevel(entry.Level)
	if m.logs.level != 0 && level != []string{"", "INFO", "WARN", "ERROR"}[m.logs.level] {
		// Fatal events belong to the error filter as well.
		if m.logs.level != 3 || level != "FATAL" && level != "PANIC" {
			return false
		}
	}
	query := strings.ToLower(strings.TrimSpace(m.logs.search.Value()))
	return query == "" || strings.Contains(strings.ToLower(entry.Level+" "+m.logMessage(entry)), query)
}

func (m Model) logDocument(width int) logDocument {
	width = max(1, width)
	var doc logDocument
	for index, entry := range m.logs.entries {
		if !m.logMatches(entry) {
			continue
		}
		doc.matched++
		stamp := "--:--:--"
		if !entry.Time.IsZero() {
			stamp = entry.Time.Local().Format("15:04:05")
		}
		level, color := logLevel(entry.Level)
		message := m.logMessage(entry)
		var rows []string
		if m.logs.compact && entry.Level != "gap" {
			prefix := themed(stamp+"  ", muted, bg, false) + themed(cellPad(level, 5)+"  ", color, bg, false)
			rows = []string{cellCrop(prefix+themed(message, text, bg, false), width)}
		} else {
			rows = append(rows, cellCrop(themed(stamp+"  ", muted, bg, false)+themed(level, color, bg, false), width))
			messageColor := text
			if entry.Level == "gap" {
				messageColor = warning
			}
			rows = append(rows, strings.Split(wrapText(themed(message, messageColor, bg, false), width), "\n")...)
			rows = append(rows, themed(strings.Repeat("─", width), border, bg, false))
		}
		for row, value := range rows {
			doc.lines = append(doc.lines, value)
			doc.positions = append(doc.positions, logPosition{entry: m.logs.first + uint64(index), row: row})
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

func (m Model) logsToolbar(width, rows int) dashboardBlock {
	width = max(1, width)
	var b dashboardBlock
	buttons := ""
	for _, item := range []struct{ id, label string }{
		{"logs.level", m.tr([]string{"logs.level_all", "logs.level_info", "logs.level_warn", "logs.level_error"}[m.logs.level])},
		{"logs.view", m.tr(map[bool]string{false: "logs.detailed", true: "logs.compact"}[m.logs.compact])},
		{"logs.reset-search", "×"},
	} {
		o := dashboardButtonOptions(item.id, m.navigationOptions(), false, bg)
		o.Disabled = m.busy
		value := renderButton(item.label, o)
		b.targets = append(b.targets, hitRegion{ID: item.id, X: ansi.StringWidth(buttons), W: ansi.StringWidth(value), H: 1})
		buttons += value
	}
	b.lines = []string{buttons}
	b = m.searchToolbar("logs.search", m.tr("logs.placeholder"), m.logs.search,
		b, width, width >= 68, false)
	count := 0
	for _, entry := range m.logs.entries {
		if m.logMatches(entry) {
			count++
		}
	}
	status := m.tr("logs.summary", count, len(m.logs.entries))
	if m.logs.editing {
		status = m.tr("logs.search_edit")
	} else if !m.logs.observedAt.IsZero() {
		observed := " · " + m.tr("logs.observed", m.logs.observedAt.Local().Format("15:04:05"))
		if ansi.StringWidth(status+observed) <= width {
			status += observed
		}
	}
	for _, item := range []struct{ id, key string }{{"logs.level", "logs.level_hint"}, {"logs.view", "logs.view_hint"}, {"logs.reset-search", "logs.reset_hint"}} {
		if m.hover == item.id || m.focus == item.id {
			status = m.tr(item.key)
			break
		}
	}
	b.lines = append(b.lines, themed(cellCrop(status, width), muted, bg, false), "")
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
		b = m.logsToolbar(width, filterToolbarRows(width))
	}
	if m.logs.err != nil {
		value := m.tr("logs.unavailable", m.localizeError(m.logs.err))
		lines := strings.Split(wrapText(themed(value, errorColor, bg, false), max(1, width)), "\n")
		b.lines = append(b.lines, lines[:min(2, len(lines))]...)
	}
	return b
}

func (m Model) logsCapacity(width int) int {
	return max(1, dashboardGeometry(m.dashboardOptions()).height-len(m.logsPrefix(width).lines)-1)
}

func (m Model) logsStatus() (string, string) {
	if m.logs.paused {
		return m.tr("logs.paused"), warning
	}
	if m.logs.ended {
		return m.tr("logs.ended"), muted
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
	b := m.logsPrefix(width)
	height := dashboardGeometry(m.dashboardOptions()).height
	// Even a tiny terminal must leave a row for the log viewport.
	if len(b.lines) >= height-1 {
		b.lines = b.lines[:max(0, height-2)]
		visible := b.targets[:0]
		for _, target := range b.targets {
			if clipped, ok := clipRegion(target, 0, 0, width, len(b.lines)); ok {
				visible = append(visible, clipped)
			}
		}
		b.targets = visible
	}
	doc := m.logDocument(width)
	if len(doc.lines) == 0 {
		message := m.tr("logs.empty")
		if m.logs.paused || m.logs.ended {
			message = m.tr("logs.empty_inactive")
		}
		if len(m.logs.entries) > 0 || m.logs.search.Value() != "" || m.logs.level != 0 {
			message = m.tr("logs.no_matches")
		}
		doc.lines = strings.Split(wrapText(themed(message, muted, bg, false), width), "\n")
	}
	capacity := max(1, height-len(b.lines)-1)
	v := m.logViewport(doc, width, capacity)
	b.targets = append(b.targets, hitRegion{ID: "logs.list", Y: len(b.lines), W: width, H: capacity})
	b.lines = append(b.lines, strings.Split(v.View(), "\n")...)
	if len(b.lines) < height {
		status, color := m.logsStatus()
		label := m.tr("logs.latest")
		if m.logs.holding && m.logs.unread > 0 {
			label += fmt.Sprintf(" +%d", m.logs.unread)
		}
		if width < 60 {
			label = "↓"
			if m.logs.holding && m.logs.unread > 0 {
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
