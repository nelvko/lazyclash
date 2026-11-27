package tui

import (
	"cmp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type overviewField struct{ label, value, color string }

func overviewFieldGrid(o dashboardOptions, fields []overviewField, width, columns int) []string {
	if len(fields) == 0 {
		return nil
	}
	columns = max(1, min(columns, len(fields)))
	gap := themed("  ", text, panel, o.Dim)
	cellWidth := max(1, (width-ansi.StringWidth(gap)*(columns-1))/columns)
	var lines []string
	for i := 0; i < len(fields); i += columns {
		cells := make([][]string, columns)
		for j, field := range fields[i:min(i+columns, len(fields))] {
			label := themed(field.label, muted, panel, o.Dim)
			value := themed(lipgloss.NewStyle().Bold(true).Render(clean(field.value)), cmp.Or(field.color, text), panel, o.Dim)
			valueRows := strings.Split(wrapText(value, cellWidth), "\n")
			if i > 0 {
				cells[j] = append(cells[j], "")
			}
			cells[j] = append(cells[j], strings.Split(wrapText(label, cellWidth), "\n")...)
			cells[j] = append(cells[j], valueRows...)
		}
		lines = append(lines, overviewFieldRow(cells, cellWidth, gap)...)
	}
	return lines
}

// Align text blocks by their first row, preserving each group's display width.
func overviewFieldRow(cells [][]string, cellWidth int, gap string) []string {
	height := 0
	for _, cell := range cells {
		height = max(height, len(cell))
	}
	var lines []string
	for y := 0; y < height; y++ {
		var row []string
		for _, cell := range cells {
			value := ""
			if y < len(cell) {
				value = cell[y]
			}
			row = append(row, cellPad(value, cellWidth))
		}
		lines = append(lines, strings.Join(row, gap))
	}
	return lines
}
