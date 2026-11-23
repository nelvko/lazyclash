package tui

const (
	proxyNodeMinWidth    = 29
	proxyNodeContentRows = 2
	proxyColumnGap       = 1
	proxyMaxColumns      = 4
)

type proxyGridLayout struct {
	indent, columns, cardWidth int
	cardPadding, rowGap        int
}

func (m *Model) compactProxyCards() bool {
	return m.proxiesNarrow() || m.height <= 24
}

func (m *Model) proxyCardPadding() int {
	if m.compactProxyCards() || m.buttonPlain() {
		return 0
	}
	return 1
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

func (m *Model) proxyGrid(bodyWidth int) proxyGridLayout {
	indent := 2
	if m.proxiesNarrow() {
		indent = 0
	}
	grid := layoutProxyGrid(bodyWidth, indent)
	grid.cardPadding = m.proxyCardPadding()
	if grid.cardPadding == 0 && !m.compactProxyCards() {
		grid.rowGap = 1
	}
	return grid
}

func proxyCardHeight(padding int) int {
	return proxyNodeContentRows + 2*padding
}

func (g proxyGridLayout) stride() int {
	return proxyCardHeight(g.cardPadding) + g.rowGap
}
