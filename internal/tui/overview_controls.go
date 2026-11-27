package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type overviewControlButton struct {
	id, label         string
	enabled, selected bool
	primary           bool
}

func (b overviewControlButton) selectable() bool {
	return strings.HasPrefix(b.id, "mode.")
}

func overviewSelectionWidth(buttons []overviewControlButton) int {
	width := 0
	for _, button := range buttons {
		if button.selectable() {
			width = max(width, 6, ansi.StringWidth(button.label))
		}
	}
	return width
}

func overviewTextBlock(lines []string, width int) dashboardBlock {
	var b dashboardBlock
	for _, line := range lines {
		b.lines = append(b.lines, strings.Split(wrapText(line, max(1, width)), "\n")...)
	}
	return b
}

func overviewButton(o dashboardOptions, button overviewControlButton, kind buttonKind) string {
	options := dashboardButtonOptions(button.id, o, button.primary, panel)
	if !button.primary {
		options.Kind = kind
	}
	options.Disabled, options.Selected = !button.enabled, button.selected
	options.Selectable = button.selectable()
	options.Compact = kind == buttonIcon
	return renderButton(button.label, options)
}

// Labels and actions share a line when they fit. Narrow cards keep the whole
// label and put the action on the following line, with its own hit region.
func overviewLabelControl(o dashboardOptions, width int, label string, button overviewControlButton, kind buttonKind) dashboardBlock {
	id := ""
	if button.enabled {
		id = button.id
	}
	return overviewLabelSlot(o, width, label, overviewButton(o, button, kind), id, false)
}

func overviewLabelSlot(o dashboardOptions, width int, label, face, id string, rightAlignWrapped bool) dashboardBlock {
	width = max(1, width)
	value := cellCrop(face, width)
	buttonWidth := ansi.StringWidth(value)
	b := overviewTextBlock([]string{label}, width)
	x, y := 0, len(b.lines)
	if gap := width - ansi.StringWidth(label) - buttonWidth; gap >= 2 {
		x, y = width-buttonWidth, 0
		b.lines[0] = label + themed(strings.Repeat(" ", gap), text, panel, o.Dim) + value
	} else {
		if rightAlignWrapped {
			x = width - buttonWidth
		}
		b.lines = append(b.lines, strings.Repeat(" ", x)+value)
	}
	if id != "" && buttonWidth > 0 {
		b.targets = append(b.targets, hitRegion{ID: id, X: x, Y: y, W: buttonWidth, H: 1})
	}
	return b
}

func overviewButtons(o dashboardOptions, width int, buttons []overviewControlButton) dashboardBlock {
	width = max(1, width)
	selectionWidth := overviewSelectionWidth(buttons)
	var b dashboardBlock
	row, x := "", 0
	for _, button := range buttons {
		options := dashboardButtonOptions(button.id, o, button.primary, panel)
		options.Disabled, options.Selected = !button.enabled, button.selected
		options.Selectable = button.selectable()
		if options.Selectable {
			options.Width = selectionWidth
		}
		value := cellCrop(renderButton(button.label, options), width)
		buttonWidth := ansi.StringWidth(value)
		if x > 0 && x+buttonWidth > width {
			b.lines = append(b.lines, row)
			row, x = "", 0
		}
		if button.enabled && strings.TrimSpace(ansi.Strip(value)) != "" {
			b.targets = append(b.targets, hitRegion{ID: button.id, X: x, Y: len(b.lines), W: buttonWidth, H: 1})
		}
		row += value
		x += buttonWidth
	}
	if len(buttons) > 0 {
		b.lines = append(b.lines, row)
	}
	return b
}

func overviewInnerWidth(width int) int {
	if width < 4 {
		return max(1, width)
	}
	return max(1, width-4)
}

func overviewHeading(o dashboardOptions, title string) string {
	return themed(lipgloss.NewStyle().Bold(true).Render(title), text, panel, o.Dim)
}

func overviewActionPanel(o dashboardOptions, width int, title string, content dashboardBlock, action overviewControlButton) dashboardBlock {
	heading := overviewLabelControl(o, overviewInnerWidth(width), overviewHeading(o, title), action, buttonText)
	return overviewPanel(o, width, heading, content, nil)
}

func overviewControlPanel(o dashboardOptions, width int, title string, lines []string, buttons []overviewControlButton, metadata ...string) dashboardBlock {
	inner := overviewInnerWidth(width)
	heading := overviewHeading(o, title)
	content := overviewTextBlock(lines, inner)
	if len(metadata) > 0 {
		if detail := clean(metadata[0]); detail != "" {
			if gap := inner - ansi.StringWidth(title) - ansi.StringWidth(detail); gap >= 2 {
				heading += themed(strings.Repeat(" ", gap)+detail, muted, panel, o.Dim)
			} else {
				before := overviewTextBlock([]string{themed(detail, muted, panel, o.Dim)}, inner)
				before.append(content)
				content = before
			}
		}
	}
	return overviewPanel(o, width, overviewTextBlock([]string{heading}, inner), content, buttons)
}

func overviewPanel(o dashboardOptions, width int, b, content dashboardBlock, buttons []overviewControlButton) dashboardBlock {
	left, top := 2, 1
	if width < 4 {
		left, top = 0, 0
	}
	b.blank()
	if content.footerY > 0 {
		b.footerY = top + len(b.lines) + content.footerY
	}
	b.append(content)
	if len(buttons) > 0 {
		b.blank()
		b.footerY = top + len(b.lines)
		b.append(overviewButtons(o, overviewInnerWidth(width), buttons))
	}
	b.lines = overviewSurface(b.lines, width, len(b.lines)+2, o)
	visible := b.targets[:0]
	for _, target := range b.targets {
		target.X += left
		target.Y += top
		if target, ok := clipRegion(target, 0, 0, width, len(b.lines)); ok {
			visible = append(visible, target)
		}
	}
	b.targets = visible
	return b
}

type overviewControl struct {
	face, id string
}

// Place a face and its per-line hit regions together. Very narrow cards wrap the label
// independently of the two-cell focus marker, so tabbing never reflows a row.
func (c overviewControl) place(b *dashboardBlock, x, y, width int) {
	available := width - x
	lines := []string{c.face}
	if cells := ansi.StringWidth(c.face); cells > available {
		gutter := min(2, max(0, available-2))
		marker := ansi.Cut(c.face, 2-gutter, 2)
		lines = overviewTextBlock([]string{ansi.Cut(c.face, 2, cells)}, available-gutter).lines
		for i, line := range lines {
			lines[i] = marker + line
			marker = strings.Repeat(" ", gutter)
		}
	}
	for i, line := range lines {
		if y+i == len(b.lines) {
			b.blank()
		}
		b.lines[y+i] = cellPad(b.lines[y+i], x) + line
		if cells := ansi.StringWidth(line); c.id != "" && cells > 0 {
			b.targets = append(b.targets, hitRegion{ID: c.id, X: x, Y: y + i, W: cells, H: 1})
		}
	}
}

func (o dashboardOptions) switchControl(id string, control Switch) overviewControl {
	face := control.View(o.switchOptions(id, panel))
	if !control.Enabled() {
		id = ""
	}
	return overviewControl{id: id, face: face}
}
