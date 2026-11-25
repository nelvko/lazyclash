package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Reserve room for a loopback endpoint even in the smallest editable dialog.
func (d *wizardDocument) networkValueColumn() int {
	return min(14, max(2, d.width-16))
}

func (d *wizardDocument) networkRow(label string, value dashboardBlock) {
	x, y := d.networkValueColumn(), len(d.lines)
	labels := strings.Split(wrapText(label, max(1, x-2)), "\n")
	for row := 0; row < max(len(labels), len(value.lines)); row++ {
		left, right := "", ""
		if row < len(labels) {
			left = labels[row]
		}
		if row < len(value.lines) {
			right = value.lines[row]
		}
		d.lines = append(d.lines, themed(cellPad(left, x), muted, raised, false)+right)
	}
	for _, hit := range value.targets {
		hit.X, hit.Y = hit.X+x, hit.Y+y
		d.targets = append(d.targets, hit)
	}
}

func (d *wizardDocument) networkValue(label, value, color string) {
	lines := strings.Split(wrapText(clean(value), d.width-d.networkValueColumn()), "\n")
	for i, line := range lines {
		lines[i] = themed(line, color, raised, false)
	}
	d.networkRow(label, dashboardBlock{lines: lines})
}

// Keep the scope on the title line when both fit. Below that width it gets
// its own line, so an active editor never loses its scope when resized.
func networkHeader(title, scope string, width int) []string {
	heading := themed(ansi.NewStyle().Bold().Styled(title), pageTitleForeground, raised, false)
	if scope == "" {
		return []string{heading}
	}
	gap := width - ansi.StringWidth(title) - ansi.StringWidth(scope)
	context := themed(scope, muted, raised, false)
	if gap >= 2 {
		return []string{heading + themed(strings.Repeat(" ", gap), text, raised, false) + context}
	}
	return []string{heading, context}
}
