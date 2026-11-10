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
	return strings.HasPrefix(b.id, "mode.") || strings.HasPrefix(b.id, "language.") || b.id == "key-hints.toggle"
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
	return renderButton(button.label, options)
}

// Labels and actions share a line when they fit. Narrow cards keep the whole
// label and put the action on the following line, with its own hit region.
func overviewLabelControl(o dashboardOptions, width int, label string, button overviewControlButton, kind buttonKind) dashboardBlock {
	width = max(1, width)
	value := cellCrop(overviewButton(o, button, kind), width)
	buttonWidth := ansi.StringWidth(value)
	b := overviewTextBlock([]string{label}, width)
	x, y := 0, len(b.lines)
	if gap := width - ansi.StringWidth(label) - buttonWidth; gap >= 2 {
		x, y = width-buttonWidth, 0
		b.lines[0] = label + themed(strings.Repeat(" ", gap), text, panel, o.Dim) + value
	} else {
		b.lines = append(b.lines, value)
	}
	if button.enabled && strings.TrimSpace(ansi.Strip(value)) != "" {
		b.targets = append(b.targets, hitRegion{ID: button.id, X: x, Y: y, W: buttonWidth, H: 1})
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

func overviewActionPanel(o dashboardOptions, width int, title string, content dashboardBlock, action overviewControlButton) dashboardBlock {
	return overviewPanel(o, width, title, content, nil, "", &action)
}

func overviewControlPanel(o dashboardOptions, width int, title string, content []string, buttons []overviewControlButton, metadata ...string) dashboardBlock {
	detail := ""
	if len(metadata) > 0 {
		detail = metadata[0]
	}
	inner := max(1, width-4)
	if width < 4 {
		inner = max(1, width)
	}
	return overviewPanel(o, width, title, overviewTextBlock(content, inner), buttons, detail, nil)
}

func overviewPanel(o dashboardOptions, width int, title string, content dashboardBlock, buttons []overviewControlButton, metadata string, action *overviewControlButton) dashboardBlock {
	inner, left, top := max(1, width-4), 2, 1
	if width < 4 {
		inner, left, top = max(1, width), 0, 0
	}
	heading := themed(lipgloss.NewStyle().Bold(true).Render(title), text, panel, o.Dim)
	var b dashboardBlock
	if action != nil {
		b = overviewLabelControl(o, inner, heading, *action, buttonText)
	} else {
		if detail := clean(metadata); detail != "" {
			if gap := inner - ansi.StringWidth(title) - ansi.StringWidth(detail); gap >= 2 {
				heading += themed(strings.Repeat(" ", gap)+detail, muted, panel, o.Dim)
			} else {
				before := overviewTextBlock([]string{themed(detail, muted, panel, o.Dim)}, inner)
				before.append(content)
				content = before
			}
		}
		b = overviewTextBlock([]string{heading}, inner)
	}
	b.blank()
	b.append(content)
	if len(buttons) > 0 {
		b.blank()
		b.footerY = top + len(b.lines)
		b.append(overviewButtons(o, inner, buttons))
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
