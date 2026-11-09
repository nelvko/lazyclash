package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// These are core-wide counters from one /connections observation. Filtering
// only changes the list; pause freezes the entire observation, including memory.
func (m Model) connectionSummary(width int) []string {
	fields := []struct{ label, value, color string }{
		{m.tr("connections.summary_download"), "--", accent},
		{m.tr("connections.summary_upload"), "--", warning},
		{m.tr("connections.summary_memory"), "--", text},
		{m.tr("connections.summary_active"), "--", text},
	}
	if snapshot := m.connectionSnapshot(); snapshot != nil {
		fields[0].value = formatBytes(snapshot.DownloadTotal)
		fields[1].value = formatBytes(snapshot.UploadTotal)
		fields[2].value = formatBytes(int64(snapshot.Memory))
		if snapshot.Memory > 1<<63-1 {
			fields[2].value = fmt.Sprintf("%.1f EiB", float64(snapshot.Memory)/(1<<60))
		}
		fields[3].value = fmt.Sprint(len(snapshot.Connections))
	}
	// Use a fixed value budget so translated labels determine the breakpoints
	// and the grid remains stable as samples change.
	minimum := 1
	for _, field := range fields {
		minimum = max(minimum, ansi.StringWidth(field.label)+14)
	}
	columns := 1
	if width >= minimum*4+6 {
		columns = 4
	} else if width >= minimum*2+2 {
		columns = 2
	}
	cellWidth := max(1, (width-2*(columns-1))/columns)
	var lines []string
	for i := 0; i < len(fields); i += columns {
		var cells []string
		for _, field := range fields[i : i+columns] {
			cell := themed(field.label+": ", muted, bg, false) + themed(field.value, field.color, bg, false)
			cells = append(cells, cellPad(cell, cellWidth))
		}
		lines = append(lines, cellCrop(strings.Join(cells, themed("  ", text, bg, false)), width))
	}
	return lines
}
