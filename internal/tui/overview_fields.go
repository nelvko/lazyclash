package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type overviewField struct{ label, value string }

func overviewFieldGrid(o dashboardOptions, fields []overviewField, width, columns int) []string {
	if len(fields) == 0 {
		return nil
	}
	columns = max(1, min(columns, len(fields)))
	cellWidth := max(1, (width-2*(columns-1))/columns)
	var lines []string
	for i := 0; i < len(fields); i += columns {
		cells := make([][]string, min(columns, len(fields)-i))
		for j := range cells {
			field := fields[i+j]
			label := themed(field.label, muted, panel, o.Dim)
			value := themed(lipgloss.NewStyle().Bold(true).Render(clean(field.value)), text, panel, o.Dim)
			valueRows := strings.Split(wrapText(value, cellWidth), "\n")
			cells[j] = append(strings.Split(wrapText(label, cellWidth), "\n"), valueRows...)
		}
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, overviewFieldRow(o, cells, cellWidth)...)
	}
	return lines
}

// Align text blocks by their first row, preserving each group's display width.
func overviewFieldRow(o dashboardOptions, cells [][]string, cellWidth int) []string {
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
		lines = append(lines, strings.Join(row, themed("  ", text, panel, o.Dim)))
	}
	return lines
}
