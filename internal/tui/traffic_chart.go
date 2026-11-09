package tui

import (
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

type trafficChartCell struct{ up, down uint8 }

type trafficChartGeometry struct {
	columns, height, axis, dotsX, dotsY int
	scale                               trafficChartScale
	now, start                          time.Time
}

func newTrafficChartGeometry(width, height int, scale trafficChartScale, now time.Time) trafficChartGeometry {
	if scale.maximum <= 0 || math.IsNaN(scale.maximum) || math.IsInf(scale.maximum, 0) {
		scale = makeTrafficChartScale(0)
	}
	axis := max(12, max(ansi.StringWidth(scale.top), ansi.StringWidth(scale.mid))+1)
	if width-axis < 8 {
		axis = 0
	}
	columns := max(0, width-axis)
	return trafficChartGeometry{columns: columns, height: height, axis: axis, dotsX: columns * 2, dotsY: height * 4, scale: scale, now: now, start: now.Add(-trafficHistoryWindow)}
}

// Values, grid lines and tick labels all originate in this full 2x4 dot plane.
// Rounding the final coordinate gives at most half a dot error. Integer text
// labels occupy the character cell containing their grid line (nearest center).
func (g trafficChartGeometry) rateY(rate float64) int {
	if g.dotsY <= 1 {
		return 0
	}
	fraction := max(0.0, min(1.0, rate/g.scale.maximum))
	return max(0, min(g.dotsY-1, int(math.Round((1-fraction)*float64(g.dotsY-1)))))
}

func (g trafficChartGeometry) position(at time.Time, rate float64) (int, int) {
	x := int(math.Round(float64(at.Sub(g.start)) / float64(trafficHistoryWindow) * float64(g.dotsX-1)))
	return max(0, min(g.dotsX-1, x)), g.rateY(rate)
}

func (g trafficChartGeometry) valid(point trafficPoint) bool {
	return point.Valid && point.Up >= 0 && point.Down >= 0 && !point.At.IsZero() && !point.At.Before(g.start) && !point.At.After(g.now)
}

// height is unchanged: the plot uses every Braille dot in its character rows,
// followed by one existing time-label row. Original remains Bresenham width 1.
func overviewTrafficChart(o dashboardOptions, width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	now := o.TrafficNow
	if now.IsZero() {
		for _, point := range o.TrafficHistory {
			if point.At.After(now) {
				now = point.At
			}
		}
	}
	scale := o.TrafficScale
	if scale.maximum <= 0 {
		maximum, _ := trafficWindowMaximum(o.TrafficHistory, now)
		scale = makeTrafficChartScale(maximum)
	}
	g := newTrafficChartGeometry(width, height, scale, now)
	return trafficChartRows(o, g, trafficChartOriginalCells(o.TrafficHistory, g))
}

func trafficChartOriginalCells(history []trafficPoint, g trafficChartGeometry) []trafficChartCell {
	cells := make([]trafficChartCell, g.columns*g.height)
	var previous trafficPoint
	havePrevious := false
	for _, point := range history {
		if !g.valid(point) {
			havePrevious = false
			continue
		}
		delta := point.At.Sub(previous.At)
		connected := havePrevious && delta > 0 && delta <= trafficHistoryMaxGap
		for _, series := range []struct {
			before, current int64
			upload          bool
		}{{previous.Up, point.Up, true}, {previous.Down, point.Down, false}} {
			x, y := g.position(point.At, float64(series.current))
			if connected {
				// Zero is still valid history. Suppress only an all-zero segment; both
				// nonzero-to-zero and zero-to-nonzero transitions retain their endpoints.
				if series.before != 0 || series.current != 0 {
					px, py := g.position(previous.At, float64(series.before))
					trafficChartStroke(cells, g.columns, g.height, px, py, x, y, series.upload)
				}
			} else if series.current != 0 {
				trafficChartDot(cells, g.columns, g.height, x, y, series.upload)
			}
		}
		previous, havePrevious = point, true
	}
	return cells
}

func trafficChartRows(o dashboardOptions, g trafficChartGeometry, cells []trafficChartCell) []string {
	labels := make([]string, g.height)
	grid := make([]uint8, g.height)
	// First and last labels take priority in a tiny plot where ticks share a row.
	ticks := []struct {
		value float64
		label string
	}{{g.scale.maximum / 2, g.scale.mid}, {g.scale.maximum, g.scale.top}, {0, "0"}}
	for _, tick := range ticks {
		y := g.rateY(tick.value)
		labels[y/4] = tick.label
		grid[y/4] |= trafficChartDotBits(y%4, 0) | trafficChartDotBits(y%4, 1)
	}
	colors := trafficCurveColors()
	rows := make([]string, g.height+1)
	for row := range g.height {
		var line strings.Builder
		if g.axis > 0 {
			line.WriteString(themed(strings.Repeat(" ", g.axis-1-ansi.StringWidth(labels[row]))+labels[row]+" ", muted, panel, o.Dim))
		}
		color, run := "", strings.Builder{}
		flush := func() {
			if run.Len() > 0 {
				line.WriteString(themed(run.String(), color, panel, o.Dim))
				run.Reset()
			}
		}
		for column := range g.columns {
			cell := cells[row*g.columns+column]
			glyph, foreground := ' ', divider
			if grid[row] != 0 {
				glyph = rune(0x2800 + int(grid[row]))
			}
			if mask := cell.up | cell.down; mask != 0 {
				glyph = rune(0x2800 + int(mask))
				switch {
				case cell.up != 0 && cell.down != 0:
					foreground = colors.overlap
				case cell.up != 0:
					foreground = colors.upload
				default:
					foreground = colors.download
				}
			}
			if foreground != color {
				flush()
				color = foreground
			}
			run.WriteRune(glyph)
		}
		flush()
		rows[row] = line.String()
	}
	nowLabel := o.Locale.T("dashboard.overview.trafficNow")
	if nowLabel == "dashboard.overview.trafficNow" {
		nowLabel = "Now"
	}
	rows[g.height] = themed(strings.Repeat(" ", g.axis)+trafficChartTicks(g.columns, nowLabel), muted, panel, o.Dim)
	return rows
}

func trafficChartDotBits(y, x int) uint8 {
	bits := [4][2]uint8{{1, 8}, {2, 16}, {4, 32}, {64, 128}}
	return bits[y][x]
}

func trafficChartDot(cells []trafficChartCell, columns, rows, x, y int, upload bool) {
	if x < 0 || x >= columns*2 || y < 0 || y >= rows*4 {
		return
	}
	cell := &cells[(y/4)*columns+x/2]
	if upload {
		cell.up |= trafficChartDotBits(y%4, x%2)
	} else {
		cell.down |= trafficChartDotBits(y%4, x%2)
	}
}

func trafficChartStroke(cells []trafficChartCell, columns, rows, x, y, endX, endY int, upload bool) {
	dx, dy := int(math.Abs(float64(endX-x))), -int(math.Abs(float64(endY-y)))
	sx, sy := -1, -1
	if x < endX {
		sx = 1
	}
	if y < endY {
		sy = 1
	}
	err := dx + dy
	for {
		trafficChartDot(cells, columns, rows, x, y, upload)
		if x == endX && y == endY {
			return
		}
		twice := 2 * err
		if twice >= dy {
			err += dy
			x += sx
		}
		if twice <= dx {
			err += dx
			y += sy
		}
	}
}

func trafficChartTicks(width int, now string) string {
	cells := make([]string, width)
	occupied := make([]bool, width)
	for i := range cells {
		cells[i] = " "
	}
	put := func(x int, value string) {
		w := ansi.StringWidth(value)
		if x < 0 || x+w > width {
			return
		}
		for i := max(0, x-1); i < min(width, x+w+1); i++ {
			if occupied[i] {
				return
			}
		}
		for _, char := range value {
			w := ansi.StringWidth(string(char))
			cells[x] = string(char)
			for i := 0; i < w; i++ {
				occupied[x+i] = true
				if i > 0 {
					cells[x+i] = ""
				}
			}
			x += w
		}
	}
	put(width-ansi.StringWidth(now), now)
	if width >= ansi.StringWidth(now)+6 {
		put(0, "-60s")
	}
	center := func(fraction float64, label string) {
		x := int(math.Round(fraction*float64(width*2-1))) / 2
		put(x-ansi.StringWidth(label)/2, label)
	}
	if width >= ansi.StringWidth(now)+12 {
		center(0.5, "-30s")
	}
	if width >= 56 {
		center(0.25, "-45s")
		center(0.75, "-15s")
	}
	return strings.Join(cells, "")
}
