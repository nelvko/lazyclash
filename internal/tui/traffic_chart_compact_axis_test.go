package tui

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTrafficChartCompactAxisShowsRateUnitOnce(t *testing.T) {
	scale := makeTrafficChartScale(1 << 20)
	for _, test := range []struct {
		width, axis int
		top, mid    string
	}{
		{72, 12, "1.25 MiB/s", "0.625 MiB/s"},
		{71, 10, "1.25MiB/s", "0.625"},
		{18, 10, "1.25MiB/s", "0.625"},
	} {
		g := newTrafficChartGeometry(test.width, 4, scale, trafficChartTestNow())
		if g.axis != test.axis || g.columns != test.width-test.axis {
			t.Fatalf("width %d: got axis %d, plot %d", test.width, g.axis, g.columns)
		}
		rows := overviewTrafficChart(dashboardOptions{TrafficScale: scale}, test.width, g.height)
		for rate, label := range map[float64]string{scale.maximum: test.top, scale.maximum / 2: test.mid, 0: "0"} {
			row := ansi.Strip(rows[g.rateY(rate)/4])
			if got := strings.TrimSpace(ansi.Cut(row, 0, g.axis)); got != label {
				t.Errorf("width %d rate %g: got label %q, want %q", test.width, rate, got, label)
			}
		}
	}
}

func TestTrafficChartCompactAxisKeepsPlotWidthWhenRangeChanges(t *testing.T) {
	for _, peak := range []int64{0, 1 << 10, 800 << 10, 1 << 20, 800 << 20, 1 << 40, math.MaxInt64} {
		for _, width := range []int{17, 18, 40, 71} {
			g := newTrafficChartGeometry(width, 4, makeTrafficChartScale(peak), trafficChartTestNow())
			axis := 10
			if width == 17 {
				axis = 0
			}
			if g.axis != axis || g.columns != width-axis {
				t.Errorf("width %d peak %d: got axis %d, plot %d", width, peak, g.axis, g.columns)
			}
		}
	}
}
