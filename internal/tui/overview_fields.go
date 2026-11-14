package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type overviewField struct{ label, value, detail, detailValue, color string }

// Keep each current value and its secondary detail in the same column.
func overviewFieldGrid(o dashboardOptions, fields []overviewField, width, columns int) []string {
	if len(fields) == 0 {
		return nil
	}
	columns = max(1, min(columns, len(fields)))
	minimum := 1
	for _, field := range fields {
		if field.detailValue != "" {
			minimum = max(minimum, ansi.StringWidth(field.value), ansi.StringWidth(field.detailValue))
		}
	}
	for columns > 1 && (width-2*(columns-1))/columns < minimum {
		columns--
	}
	cellWidth := max(1, (width-2*(columns-1))/max(1, columns))
	var lines []string
	for i := 0; i < len(fields); i += columns {
		cells := make([][]string, min(columns, len(fields)-i))
		height := 0
		for j := range cells {
			field := fields[i+j]
			labelColor := muted
			if field.color != "" {
				labelColor = field.color
			}
			label := themed(field.label, labelColor, panel, o.Dim)
			value := themed(lipgloss.NewStyle().Bold(true).Render(clean(field.value)), text, panel, o.Dim)
			valueRows := strings.Split(wrapText(value, cellWidth), "\n")
			if field.detailValue != "" {
				valueRows = []string{cellCrop(value, cellWidth)}
			}
			cells[j] = append(strings.Split(wrapText(label, cellWidth), "\n"), valueRows...)
			if field.detail != "" {
				cells[j] = append(cells[j], "")
				detail := field.detail
				if field.detailValue != "" && ansi.StringWidth(detail+"  "+field.detailValue) <= cellWidth {
					detail += "  " + field.detailValue
				}
				cells[j] = append(cells[j], strings.Split(wrapText(themed(detail, muted, panel, o.Dim), cellWidth), "\n")...)
				if field.detailValue != "" && detail == field.detail {
					cells[j] = append(cells[j], themed(cellCrop(field.detailValue, cellWidth), muted, panel, o.Dim))
				}
			}
			height = max(height, len(cells[j]))
		}
		if i > 0 {
			lines = append(lines, "")
		}
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
	}
	return lines
}
