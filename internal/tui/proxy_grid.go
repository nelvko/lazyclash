package tui

const (
	proxyNodeMinWidth = 36
	proxyColumnGap    = 1
	proxyMaxColumns   = 4
)

type proxyGridLayout struct {
	indent, columns, cardWidth int
}

// bodyWidth already excludes the sidebar and the dashboard's outer margins.
// Keep the same slots for short groups and incomplete final rows.
func layoutProxyGrid(bodyWidth, indent int) proxyGridLayout {
	available := max(1, bodyWidth-indent)
	columns := min(proxyMaxColumns, max(1, (available+proxyColumnGap)/(proxyNodeMinWidth+proxyColumnGap)))
	return proxyGridLayout{
		indent:    indent,
		columns:   columns,
		cardWidth: (available - (columns-1)*proxyColumnGap) / columns,
	}
}

func (m Model) proxyGrid(bodyWidth int) proxyGridLayout {
	indent := 2
	if m.proxiesNarrow() {
		indent = 0
	}
	return layoutProxyGrid(bodyWidth, indent)
}
