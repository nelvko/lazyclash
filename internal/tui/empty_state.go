package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Empty states share placement, text wrapping and action geometry, while each
// page remains responsible for choosing the state from its own data source.
// Bubbles has no empty-state component; existing inputs and viewports retain
// ownership of editing and scrolling around this small view.
type emptyStateOptions struct {
	Title, Detail, ActionID, ActionLabel, Surface string
	Error, Disabled                               bool
}

func (m Model) emptyState(width, height int, o emptyStateOptions) dashboardBlock {
	if width <= 0 || height <= 0 {
		return dashboardBlock{}
	}
	if o.Surface == "" {
		o.Surface = bg
	}
	textWidth := min(60, width)
	color := text
	if o.Error {
		color = errorColor
	}
	var b dashboardBlock
	for _, line := range strings.Split(wrapText(clean(strings.ReplaceAll(o.Title, "\n", " ")), textWidth), "\n") {
		b.lines = append(b.lines, themed(line, color, o.Surface, false))
	}
	hasAction := o.ActionID != "" && o.ActionLabel != ""
	actionRows := 0
	if hasAction {
		actionRows = 1
	}
	// Keep the title and recovery action visible even in a short viewport.
	b.lines = b.lines[:min(len(b.lines), max(0, height-actionRows))]
	if o.Detail != "" {
		detail := strings.Split(wrapText(clean(strings.ReplaceAll(o.Detail, "\n", " ")), textWidth), "\n")
		space := height - len(b.lines) - actionRows
		if space > 0 {
			for _, line := range detail[:min(len(detail), min(3, space))] {
				b.lines = append(b.lines, themed(line, muted, o.Surface, false))
			}
		}
	}
	if hasAction {
		if len(b.lines)+2 <= height {
			b.blank()
		}
		options := dashboardButtonOptions(o.ActionID, m.navigationOptions(), false, o.Surface)
		options.Dim, options.Disabled = false, o.Disabled || m.busy || m.quitting
		label := clean(o.ActionLabel)
		if width >= 5 {
			label = cellCrop(label, width-4)
		}
		button := cellCrop(renderButton(label, options), width)
		if !options.Disabled {
			b.targets = append(b.targets, hitRegion{ID: o.ActionID, Y: len(b.lines), W: ansi.StringWidth(button), H: 1})
		}
		b.lines = append(b.lines, button)
	}
	b = centerEmptyState(b, width, height)
	for i, line := range b.lines {
		b.lines[i] = themed(line, text, o.Surface, false)
	}
	return b
}

// Place each text row and its hit regions together, including wide characters.
// No scroll offset is retained when the contents fit the empty viewport.
func centerEmptyState(block dashboardBlock, width, height int) dashboardBlock {
	if width <= 0 || height <= 0 {
		return dashboardBlock{}
	}
	top := max(0, (height-len(block.lines))/2)
	rows := make([]string, max(height, len(block.lines)))
	shifts := make([]int, len(block.lines))
	for i := range rows {
		rows[i] = strings.Repeat(" ", width)
	}
	for i, line := range block.lines {
		line = cellCrop(line, width)
		left := max(0, (width-ansi.StringWidth(line))/2)
		shifts[i] = left
		rows[top+i] = strings.Repeat(" ", left) + cellPad(line, width-left)
	}
	var targets []hitRegion
	for _, target := range block.targets {
		if target.Y < 0 || target.Y >= len(shifts) {
			continue
		}
		target.X += shifts[target.Y]
		target.Y += top
		if clipped, ok := clipRegion(target, 0, 0, width, len(rows)); ok {
			targets = append(targets, clipped)
		}
	}
	return dashboardBlock{lines: rows, targets: targets}
}

func (m Model) coreEmptyState() (emptyStateOptions, bool) {
	if !m.snapshotReady {
		if !m.busy {
			return emptyStateOptions{Title: m.tr("empty.read_failed"), Detail: m.failure, Error: true,
				ActionID: "snapshot.retry", ActionLabel: m.tr("empty.retry")}, true
		}
		return emptyStateOptions{Title: m.tr("empty.loading")}, true
	}
	state, label := m.corePageState()
	if state == "" {
		return emptyStateOptions{}, false
	}
	o := emptyStateOptions{Title: label}
	switch state {
	case "uninstalled", "stopped":
		o.ActionID, o.ActionLabel = m.corePageAction()
	case "unavailable":
		o.Title, o.Error = m.tr("empty.read_failed"), true
		o.Detail = label
		if m.snapshot.ServiceError != "" {
			o.Detail = m.locale.Diagnostic(m.snapshot.ServiceError, m.snapshot.ServiceDiagnostic)
		}
	}
	return o, true
}
