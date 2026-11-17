package tui

import "github.com/charmbracelet/x/ansi"

// Pages supply their existing controls and Bubbles input. This layout keeps
// scope before search, and list functions after its reserved count space.
type listSearchToolbar struct {
	scope, functions          dashboardBlock
	countWidth, matchingWidth int
}

func (bar listSearchToolbar) searchGeometry(width int) (fieldWidth, offset int) {
	width = max(1, width)
	offset = searchControlWidth(bar.scope) + 2
	suffix := searchControlWidth(bar.functions) + 2
	if bar.countWidth > 0 {
		suffix += bar.countWidth + 2
	}
	fieldWidth = min(inputFieldMaxWidth, width-offset-suffix)
	if len(bar.scope.lines) == 1 && fieldWidth >= 24 &&
		(bar.matchingWidth == 0 || inputFieldInlineTrailing(fieldWidth, bar.matchingWidth, true)) {
		return fieldWidth, offset
	}
	// Keep the entire input on the next row. Reserve an inline count only
	// when doing so leaves useful text space and the matching options fit.
	fieldWidth = width - bar.countWidth - 2
	if bar.countWidth == 0 || fieldWidth < 12 ||
		bar.matchingWidth > 0 && !inputFieldInlineTrailing(fieldWidth, bar.matchingWidth, true) {
		fieldWidth = width
	}
	return min(inputFieldMaxWidth, fieldWidth), 0
}

func (bar listSearchToolbar) stackedFunctionPosition(width int) (x, y int) {
	y = max(0, len(bar.scope.lines)-1)
	if len(bar.scope.lines) > 0 {
		x = ansi.StringWidth(bar.scope.lines[y]) + 2
	}
	if x+searchControlWidth(bar.functions) > width {
		x, y = 0, len(bar.scope.lines)
	}
	return x, y
}

// Height queries use the same geometry without rendering another Bubbles
// viewport or matching the retained records on every dashboard layout pass.
func (bar listSearchToolbar) rows(width int, countVisible bool) int {
	width = max(1, width)
	fieldWidth, offset := bar.searchGeometry(width)
	rows := inputFieldRows(fieldWidth, bar.matchingWidth, true)
	if offset == 0 {
		_, y := bar.stackedFunctionPosition(width)
		rows += max(len(bar.scope.lines), y+len(bar.functions.lines))
	}
	if countVisible && offset+fieldWidth+2+bar.countWidth > width {
		rows++
	}
	return rows
}

func (bar listSearchToolbar) render(search dashboardBlock, width int, count, compactCount string) dashboardBlock {
	width = max(1, width)
	fieldWidth, offset := bar.searchGeometry(width)
	if count != "" && offset+fieldWidth+2+bar.countWidth <= width {
		search.lines[1] += themed("  "+count, muted, bg, false)
	} else if count != "" {
		if ansi.StringWidth(count) > width {
			count = compactCount
		}
		search.lines = append(search.lines, themed(cellCrop(count, width), muted, bg, false))
	}
	var b dashboardBlock
	place := func(block dashboardBlock, x, y int) {
		for i, line := range block.lines {
			for len(b.lines) <= y+i {
				b.lines = append(b.lines, "")
			}
			b.lines[y+i] = themed(cellPad(b.lines[y+i], x), text, bg, false) + line
		}
		for _, target := range block.targets {
			target.X += x
			target.Y += y
			b.targets = append(b.targets, target)
		}
	}
	if offset > 0 {
		place(bar.scope, 0, 1)
		place(search, offset, 0)
		x := offset + fieldWidth + 2
		if bar.countWidth > 0 {
			x += bar.countWidth + 2
		}
		place(bar.functions, x, 1)
	} else {
		place(bar.scope, 0, 0)
		x, y := bar.stackedFunctionPosition(width)
		place(bar.functions, x, y)
		b.append(search)
	}
	for i, line := range b.lines {
		b.lines[i] = cellCrop(line, width)
	}
	return b
}
