package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestTrafficChartGapColumns(t *testing.T) {
	start := time.Unix(100, 0)
	g := newTrafficChartGeometry(73, 6, makeTrafficChartScale(0), start.Add(trafficHistoryWindow))
	frame := func(second int, valid bool) trafficPoint {
		return trafficPoint{At: start.Add(time.Duration(second) * time.Second), Valid: valid}
	}
	badRate := frame(12, true)
	badRate.Up = -1
	for _, tc := range []struct {
		name    string
		history []trafficPoint
		want    []int
	}{
		{"empty", nil, nil},
		{"single zero", []trafficPoint{frame(10, true)}, nil},
		{"normal zero interval", []trafficPoint{frame(10, true), frame(13, true)}, nil},
		{"unobserved interval", []trafficPoint{frame(10, true), frame(15, true)}, []int{11, 12, 13, 14}},
		{"failure breaks short interval", []trafficPoint{frame(10, true), frame(11, false), frame(13, true)}, []int{11, 12}},
		{"failed sample alone", []trafficPoint{frame(20, false)}, []int{20}},
		{"bad rate", []trafficPoint{badRate}, []int{12}},
		{"failures at either end", []trafficPoint{frame(10, false), frame(12, true), frame(15, false)}, []int{10, 11, 13, 14, 15}},
		{"outside window", []trafficPoint{frame(-1, true), frame(2, true), frame(61, false)}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gaps := trafficChartGapColumns(g, tc.history)
			var got []int
			for i, marked := range gaps {
				if marked {
					got = append(got, i)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("gap columns %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTrafficChartGapSharesColumnWithValidZero(t *testing.T) {
	start := time.Unix(100, 0)
	g := newTrafficChartGeometry(8, 4, makeTrafficChartScale(0), start.Add(trafficHistoryWindow))
	for _, firstValid := range []bool{false, true} {
		history := []trafficPoint{
			{At: start.Add(time.Second), Valid: firstValid},
			{At: start.Add(2 * time.Second), Valid: !firstValid},
			{At: start.Add(3 * time.Second), Valid: false},
		}
		if gaps := trafficChartGapColumns(g, history); slices.Contains(gaps, true) {
			t.Fatalf("valid zero shares the first character with failures, gap mask %v", gaps)
		}
	}
}

func TestTrafficChartGapMarksPreserveTimeLabels(t *testing.T) {
	for _, now := range []string{"Now", "现在"} {
		for _, width := range []int{1, 6, 12, 32, 56, 83} {
			gaps := make([]bool, width)
			for i := range gaps {
				gaps[i] = true
			}
			plain := trafficChartTicks(width, now, nil)
			marked := trafficChartTicks(width, now, gaps)
			if ansi.StringWidth(marked) != width || strings.ReplaceAll(marked, "┄", " ") != plain {
				t.Fatalf("%d columns, %s: labels moved from %q to %q", width, now, plain, marked)
			}
			if width >= 32 && !strings.Contains(marked, "┄") {
				t.Fatalf("expected visible gap marks at width %d: %q", width, marked)
			}
			for _, label := range []string{"-60s", "-45s", "-30s", "-15s", now} {
				if strings.Contains(marked, "┄"+label) || strings.Contains(marked, label+"┄") {
					t.Fatalf("no space around %s: %q", label, marked)
				}
			}
		}
	}
}

func TestTrafficChartGapMarksAppearOnlyOnTimeAxis(t *testing.T) {
	now := trafficChartTestNow()
	history := []trafficPoint{
		{At: now.Add(-24 * time.Second), Valid: true},
		{At: now.Add(-20 * time.Second), Valid: true},
	}
	rows := overviewTrafficChart(dashboardOptions{TrafficHistory: history, TrafficNow: now}, 100, 6)
	if len(rows) != 7 || strings.Contains(strings.Join(rows[:6], ""), "┄") || !strings.Contains(rows[6], "┄") {
		t.Fatalf("expected six plot rows and a marked time axis, got %q", rows)
	}
}
