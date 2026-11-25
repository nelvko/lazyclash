package tui

const trafficChartGapMark = "┄"

// Gap marks occupy the time axis only between observed frames or at an
// explicitly failed frame. Nothing before the first or after the last frame
// tells us whether collection was running there.
func trafficChartGapColumns(g trafficChartGeometry, history []trafficPoint) []bool {
	if g.columns == 0 {
		return nil
	}
	var gaps []bool
	mark := func(first, last int) {
		if first > last {
			return
		}
		if gaps == nil {
			gaps = make([]bool, g.columns)
		}
		for i := first; i <= last; i++ {
			gaps[i] = true
		}
	}
	var previous trafficPoint
	previousColumn, lastValidColumn := -1, -1
	for _, point := range history {
		if point.At.IsZero() || point.At.Before(g.start) || point.At.After(g.now) || !point.At.After(previous.At) {
			continue
		}
		x, _ := g.position(point.At, 0)
		column := x / 2
		valid := g.valid(point)
		if previousColumn >= 0 && (!g.valid(previous) || !valid || point.At.Sub(previous.At) > trafficHistoryMaxGap) {
			mark(previousColumn+1, column-1)
		}
		// On narrow plots multiple frames can share a character. Give a real
		// observation (including zero) precedence over a gap in that cell.
		if valid {
			lastValidColumn = column
			if gaps != nil {
				gaps[column] = false
			}
		} else if column != lastValidColumn {
			mark(column, column)
		}
		previous, previousColumn = point, column
	}
	return gaps
}
