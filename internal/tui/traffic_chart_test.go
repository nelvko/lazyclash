package tui

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func trafficChartTestNow() time.Time { return time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC) }
func trafficChartFixedScale(maximum float64) trafficChartScale {
	return trafficChartScale{maximum: maximum, top: fmt.Sprintf("%g B/s", maximum), mid: fmt.Sprintf("%g B/s", maximum/2)}
}
func trafficChartDataDot(cells []trafficChartCell, g trafficChartGeometry, x, y int, upload bool) bool {
	cell := cells[(y/4)*g.columns+x/2]
	mask := cell.down
	if upload {
		mask = cell.up
	}
	return mask&trafficChartDotBits(y%4, x%2) != 0
}
func trafficChartColorPrefix(color string) string {
	prefix, _, _ := strings.Cut(themed("x", color, panel, false), "x")
	return prefix
}

func TestTrafficChartSharesOneLinearDotPlaneForSamplesTicksAndGrid(t *testing.T) {
	now := trafficChartTestNow()
	for _, height := range []int{1, 2, 3, 4, 5, 6, 9, 12} {
		for _, maximum := range []float64{1e-12, 100, 2 * (1 << 20), 1e20} {
			g := newTrafficChartGeometry(80, height, trafficChartFixedScale(maximum), now)
			if g.rateY(0) != height*4-1 || g.rateY(maximum) != 0 {
				t.Fatalf("height %d: endpoints not at full dot boundaries", height)
			}
			before := g.rateY(0)
			for i := 0; i <= 8; i++ {
				value := maximum * float64(i) / 8
				y := g.rateY(value)
				exact := (1 - float64(i)/8) * float64(height*4-1)
				if math.Abs(float64(y)-exact) > .50001 || y < 0 || y >= height*4 || y > before {
					t.Fatalf("height %d value %g coordinate %d want~%g", height, value, y, exact)
				}
				before = y
			}
			distances := []int{}
			for i := 1; i <= 4; i++ {
				distances = append(distances, g.rateY(maximum*float64(i-1)/4)-g.rateY(maximum*float64(i)/4))
			}
			if slices.Max(distances)-slices.Min(distances) > 1 {
				t.Fatalf("equal values received unequal dot intervals: %v", distances)
			}
			rows := trafficChartRows(dashboardOptions{}, g, make([]trafficChartCell, g.columns*g.height))
			for _, value := range []float64{0, maximum / 2, maximum} {
				y := g.rateY(value)
				glyphs := []rune(ansi.Strip(rows[y/4]))[g.axis:]
				want := trafficChartDotBits(y%4, 0) | trafficChartDotBits(y%4, 1)
				if uint8(glyphs[0]-0x2800)&want != want {
					t.Fatalf("grid and value %g disagree at dot %d", value, y)
				}
			}
			if height >= 3 {
				for value, label := range map[float64]string{maximum: g.scale.top, maximum / 2: g.scale.mid, 0: "0"} {
					row := ansi.Strip(rows[g.rateY(value)/4])
					if !strings.Contains(ansi.Cut(row, 0, g.axis), label) {
						t.Fatalf("label %q not in its grid cell", label)
					}
				}
			}
		}
	}
}

func TestTrafficChartPlacesSamplesOnTheRealSixtySecondAxis(t *testing.T) {
	now := trafficChartTestNow()
	g := newTrafficChartGeometry(48, 6, trafficChartFixedScale(100), now)
	var history []trafficPoint
	for _, age := range []time.Duration{60 * time.Second, 30 * time.Second, 0} {
		history = append(history, trafficPoint{At: now.Add(-age), Up: 100, Down: 50, Valid: true})
	}
	cells := trafficChartOriginalCells(history, g)
	for _, dotX := range []int{0, g.dotsX / 2, g.dotsX - 1} {
		if !trafficChartDataDot(cells, g, dotX, 0, true) || !trafficChartDataDot(cells, g, dotX, 12, false) {
			t.Fatalf("actual time/rate missing at x=%d", dotX)
		}
	}
	count := 0
	for _, cell := range cells {
		if cell.up|cell.down != 0 {
			count++
		}
	}
	if count != 6 {
		t.Fatalf("distant observations were connected: %d populated cells", count)
	}
	rows := trafficChartRows(dashboardOptions{}, g, cells)
	ticks := ansi.Strip(rows[g.height])
	for _, label := range []string{"-60s", "-30s", "Now"} {
		if !strings.Contains(ticks, label) {
			t.Fatalf("missing %s tick", label)
		}
	}
	if !strings.HasSuffix(ticks, "Now") || !strings.HasPrefix(ticks[g.axis:], "-60s") {
		t.Fatal("window endpoints shifted")
	}
}

func TestTrafficChartZeroRulesPreserveTransitionsAndSourceHistory(t *testing.T) {
	now := trafficChartTestNow()
	for _, test := range []struct {
		name        string
		rates       []int64
		valid       []bool
		wantColumns []bool
	}{
		{"all zero", []int64{0, 0, 0, 0}, nil, []bool{false, false, false}},
		{"zero to nonzero", []int64{0, 100}, nil, []bool{true}},
		{"nonzero to zero", []int64{100, 0}, nil, []bool{true}},
		{"zero plateau", []int64{100, 0, 0, 100}, nil, []bool{true, false, true}},
		{"missing is not zero", []int64{100, 0, 100}, []bool{true, false, true}, []bool{false, false}},
	} {
		for _, upload := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/upload=%t", test.name, upload), func(t *testing.T) {
				g := newTrafficChartGeometry(132, 6, trafficChartFixedScale(100), now)
				var history []trafficPoint
				for i, rate := range test.rates {
					p := trafficPoint{At: now.Add(time.Duration(i-len(test.rates)+1) * time.Second), Valid: test.valid == nil || test.valid[i]}
					if upload {
						p.Up = rate
					} else {
						p.Down = rate
					}
					history = append(history, p)
				}
				before := slices.Clone(history)
				cells := trafficChartOriginalCells(history, g)
				for i, want := range test.wantColumns {
					x, _ := g.position(history[i].At.Add(500*time.Millisecond), 0)
					got := false
					for y := 0; y < g.dotsY; y++ {
						got = got || trafficChartDataDot(cells, g, x, y, upload)
					}
					if got != want {
						t.Fatalf("segment %d visible=%t want %t", i, got, want)
					}
				}
				for _, cell := range cells {
					other := cell.up
					if upload {
						other = cell.down
					}
					if other != 0 {
						t.Fatal("zero direction produced visible data")
					}
				}
				if !slices.Equal(history, before) {
					t.Fatal("rendering changed zero samples or timestamps")
				}
				if test.name == "all zero" {
					rows := trafficChartRows(dashboardOptions{}, g, cells)
					colors := trafficCurveColors()
					for _, color := range []string{colors.upload, colors.download, colors.overlap} {
						if strings.Contains(strings.Join(rows, "\n"), trafficChartColorPrefix(color)) {
							t.Fatal("zero-only chart painted colored/overlapping data")
						}
					}
				}
			})
		}
	}
}

func TestTrafficChartInvalidAndLongGapsRemainDisconnected(t *testing.T) {
	now := trafficChartTestNow()
	g := newTrafficChartGeometry(252, 6, trafficChartFixedScale(100), now)
	history := []trafficPoint{
		{At: now.Add(-12 * time.Second), Up: 90, Valid: true},
		{At: now.Add(-11 * time.Second), Up: 90, Valid: true},
		{At: now.Add(-10 * time.Second)},
		{At: now.Add(-9 * time.Second), Up: 90, Valid: true},
		{At: now.Add(-5 * time.Second), Up: 90, Valid: true},
		{At: now.Add(-2 * time.Second), Up: 90, Valid: true},
	}
	cells := trafficChartOriginalCells(history, g)
	for _, test := range []struct {
		age     time.Duration
		visible bool
	}{{11500 * time.Millisecond, true}, {10500 * time.Millisecond, false}, {9500 * time.Millisecond, false}, {7 * time.Second, false}, {3500 * time.Millisecond, true}} {
		x, _ := g.position(now.Add(-test.age), 0)
		found := false
		for y := range g.dotsY {
			found = found || trafficChartDataDot(cells, g, x, y, true)
		}
		if found != test.visible {
			t.Fatalf("age %s visible=%t want%t", test.age, found, test.visible)
		}
	}
	for _, history := range [][]trafficPoint{nil, {{At: now}}, {{At: now.Add(-61 * time.Second), Up: 1, Valid: true}}, {{At: now.Add(time.Second), Up: 1, Valid: true}}, {{At: now, Up: -1, Valid: true}}} {
		for _, cell := range trafficChartOriginalCells(history, g) {
			if cell.up|cell.down != 0 {
				t.Fatal("unobserved/invalid data produced a curve")
			}
		}
	}
}

func TestTrafficChartColorsShareCharacterCellsAndNeverUseTextAsOverlap(t *testing.T) {
	g := newTrafficChartGeometry(48, 6, trafficChartFixedScale(100), trafficChartTestNow())
	colors := trafficCurveColors()
	for _, test := range []struct {
		name  string
		cell  trafficChartCell
		color string
	}{
		{"upload", trafficChartCell{up: 1}, colors.upload}, {"download", trafficChartCell{down: 1}, colors.download},
		{"same subdot", trafficChartCell{up: 1, down: 1}, colors.overlap}, {"different subdots", trafficChartCell{up: 1, down: 8}, colors.overlap},
	} {
		t.Run(test.name, func(t *testing.T) {
			cells := make([]trafficChartCell, g.columns*g.height)
			cells[g.columns+3] = test.cell
			row := trafficChartRows(dashboardOptions{}, g, cells)[1]
			glyph := string(rune(0x2800 + int(test.cell.up|test.cell.down)))
			if !strings.Contains(row, themed(glyph, test.color, panel, false)) {
				t.Fatalf("%s did not receive correct character color", test.name)
			}
			if strings.Contains(row, trafficChartColorPrefix(text)) {
				t.Fatal("curve used text color")
			}
		})
	}
}

func TestTrafficChartAxisWidthAndCoordinatesStayStableAcrossScales(t *testing.T) {
	now := trafficChartTestNow()
	var want trafficChartGeometry
	for i, peak := range []int64{0, 1023, 2048, 512 << 10, 1 << 20, math.MaxInt64} {
		g := newTrafficChartGeometry(128, 6, makeTrafficChartScale(peak), now)
		if i == 0 {
			want = g
		}
		if g.axis != want.axis || g.columns != want.columns {
			t.Fatalf("rate label changed drawing width: %+v vs%+v", g, want)
		}
	}
}

func TestTrafficChartResizesWithoutClippingLabelsOrChangingHeight(t *testing.T) {
	now := trafficChartTestNow()
	for _, language := range []string{"en", "zh-Hans"} {
		for _, width := range []int{1, 2, 3, 4, 8, 12, 16, 20, 34, 60, 160} {
			for _, height := range []int{1, 2, 3, 4, 6, 9} {
				o := dashboardOptions{Locale: i18n.New(language), TrafficNow: now, TrafficHistory: []trafficPoint{{At: now.Add(-2 * time.Second), Up: math.MaxInt64, Valid: true}, {At: now, Down: math.MaxInt64, Valid: true}}}
				rows := overviewTrafficChart(o, width, height)
				if len(rows) != height+1 {
					t.Fatalf("%s/%dx%d changed chart height", language, width, height)
				}
				for _, row := range rows {
					if ansi.StringWidth(row) != width {
						t.Fatalf("%s/%dx%d: row overflows: %q", language, width, height, ansi.Strip(row))
					}
				}
			}
		}
	}
	for _, size := range [][2]int{{0, 4}, {4, 0}, {-1, 4}, {4, -1}} {
		if len(overviewTrafficChart(dashboardOptions{}, size[0], size[1])) != 0 {
			t.Fatal("empty dimensions drew data")
		}
	}
	for _, maximum := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		g := newTrafficChartGeometry(80, 6, trafficChartFixedScale(maximum), now)
		if g.rateY(0) != 23 || g.rateY(g.scale.maximum) != 0 {
			t.Fatal("invalid scale caused bad coordinates")
		}
	}
}
