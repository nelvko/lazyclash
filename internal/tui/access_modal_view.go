package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (s modalSurface) accessHeader(title, phase string, inset int) {
	inner := s.bounds.W - 2*inset
	s.put(inset, 1, themed(lipgloss.NewStyle().Bold(true).Render(cellCrop(title, inner)), pageTitleForeground, modalBackground, false))
	if phase != "" && ansi.StringWidth(title)+2+ansi.StringWidth(phase) <= inner {
		s.put(s.bounds.W-inset-ansi.StringWidth(phase), 1, themed(phase, muted, modalBackground, false))
	}
	s.divider(2)
}

// Wrap each column independently so multi-line changes keep their label.
func (d *wizardDocument) accessReviewRow(label, value string) {
	value = clean(value)
	if d.width < 48 {
		d.line(themed(label, muted, modalBackground, false))
		for _, line := range strings.Split(wrapText(value, d.width-2), "\n") {
			d.lines = append(d.lines, "  "+line)
		}
		return
	}
	labelWidth := min(27, d.width/3)
	labels := strings.Split(wrapText(label, labelWidth), "\n")
	values := strings.Split(wrapText(value, d.width-labelWidth-2), "\n")
	for i := range max(len(labels), len(values)) {
		var left, right string
		if i < len(labels) {
			left = labels[i]
		}
		if i < len(values) {
			right = values[i]
		}
		d.lines = append(d.lines, themed(cellPad(left, labelWidth), muted, modalBackground, false)+"  "+right)
	}
}
