package tui

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

// The alternative paths deliberately live in the test binary. The application's
// default remains its original Bresenham rasterizer; experiments never change
// samples, the history model, or the values presented to other pages.
type trafficExperimentCurve struct {
	x, y, slope []float64
}

func newTrafficExperimentCurve(x, y []float64) trafficExperimentCurve {
	curve := trafficExperimentCurve{x: append([]float64(nil), x...), y: append([]float64(nil), y...), slope: make([]float64, len(x))}
	if len(x) < 2 {
		return curve
	}
	h, delta := make([]float64, len(x)-1), make([]float64, len(x)-1)
	for i := range h {
		h[i] = x[i+1] - x[i]
		delta[i] = (y[i+1] - y[i]) / h[i]
	}
	if len(x) == 2 {
		curve.slope[0], curve.slope[1] = delta[0], delta[0]
		return curve
	}
	endpoint := func(hereH, nextH, hereD, nextD float64) float64 {
		m := ((2*hereH+nextH)*hereD - hereH*nextD) / (hereH + nextH)
		if m*hereD <= 0 {
			return 0
		}
		if hereD*nextD < 0 && math.Abs(m) > 3*math.Abs(hereD) {
			return 3 * hereD
		}
		return m
	}
	curve.slope[0] = endpoint(h[0], h[1], delta[0], delta[1])
	last := len(h) - 1
	curve.slope[len(x)-1] = endpoint(h[last], h[last-1], delta[last], delta[last-1])
	for i := 1; i < len(x)-1; i++ {
		if delta[i-1]*delta[i] <= 0 {
			curve.slope[i] = 0
			continue
		}
		w1, w2 := 2*h[i]+h[i-1], h[i]+2*h[i-1]
		curve.slope[i] = (w1 + w2) / (w1/delta[i-1] + w2/delta[i])
	}
	return curve
}

// evaluate uses the unmodified source rates and shape-preserving cubic Hermite
// interpolation. Clamping only protects the local range against floating point
// roundoff; the slope construction already guarantees the interval's bounds.
func (curve trafficExperimentCurve) evaluate(interval int, at float64) float64 {
	h := curve.x[interval+1] - curve.x[interval]
	t := max(0, min(1, (at-curve.x[interval])/h))
	t2, t3 := t*t, t*t*t
	value := (2*t3-3*t2+1)*curve.y[interval] + (t3-2*t2+t)*h*curve.slope[interval] +
		(-2*t3+3*t2)*curve.y[interval+1] + (t3-t2)*h*curve.slope[interval+1]
	low, high := min(curve.y[interval], curve.y[interval+1]), max(curve.y[interval], curve.y[interval+1])
	return max(low, min(high, value))
}

func trafficExperimentSegments(points []trafficPoint, start, now time.Time) [][]trafficPoint {
	var segments [][]trafficPoint
	var current []trafficPoint
	flush := func() {
		if len(current) > 0 {
			segments = append(segments, current)
			current = nil
		}
	}
	for _, point := range points {
		if !point.Valid || point.Up < 0 || point.Down < 0 || point.At.IsZero() || point.At.Before(start) || point.At.After(now) {
			flush()
			continue
		}
		if len(current) > 0 {
			delta := point.At.Sub(current[len(current)-1].At)
			if delta <= 0 || delta > trafficHistoryMaxGap {
				flush()
			}
		}
		current = append(current, point)
	}
	flush()
	return segments
}

func trafficExperimentThicken(cells []trafficChartCell, columns, rows int) []trafficChartCell {
	out := append([]trafficChartCell(nil), cells...)
	bits := [4][2]uint8{{1, 8}, {2, 16}, {4, 32}, {64, 128}}
	for y := 0; y < rows*4-1; y++ {
		for x := 0; x < columns*2; x++ {
			cell, bit := cells[(y/4)*columns+x/2], bits[y%4][x%2]
			if cell.up&bit != 0 {
				trafficChartDot(out, columns, rows, x, y+1, true)
			}
			if cell.down&bit != 0 {
				trafficChartDot(out, columns, rows, x, y+1, false)
			}
		}
	}
	return out
}

func trafficExperimentSmooth(points []trafficPoint, start, now time.Time, columns, rows int, position func(time.Time, float64) (int, int)) []trafficChartCell {
	cells := make([]trafficChartCell, columns*rows)
	for _, segment := range trafficExperimentSegments(points, start, now) {
		for _, upload := range []bool{true, false} {
			x, rates := make([]float64, len(segment)), make([]float64, len(segment))
			for i, point := range segment {
				x[i] = point.At.Sub(start).Seconds()
				rates[i] = float64(point.Down)
				if upload {
					rates[i] = float64(point.Up)
				}
			}
			if len(segment) == 1 {
				if rates[0] > 0 {
					x, y := position(segment[0].At, rates[0])
					trafficChartDot(cells, columns, rows, x, y, upload)
				}
				continue
			}
			curve := newTrafficExperimentCurve(x, rates)
			for i := 0; i < len(segment)-1; i++ {
				if rates[i] == 0 && rates[i+1] == 0 {
					continue
				}
				previousX, previousY := position(segment[i].At, rates[i])
				endX, _ := position(segment[i+1].At, rates[i+1])
				steps := max(1, endX-previousX)
				for step := 1; step <= steps; step++ {
					fraction := float64(step) / float64(steps)
					at := segment[i].At.Add(time.Duration(fraction * float64(segment[i+1].At.Sub(segment[i].At))))
					rate := curve.evaluate(i, at.Sub(start).Seconds())
					px, py := position(at, rate)
					trafficChartStroke(cells, columns, rows, previousX, previousY, px, py, upload)
					previousX, previousY = px, py
				}
			}
		}
	}
	return cells
}

func TestTrafficExperimentPCHIPPreservesSamplesAndLocalRanges(t *testing.T) {
	for _, test := range []struct {
		name string
		x, y []float64
	}{
		{"steady", []float64{0, 1, 2, 3}, []float64{4, 4, 4, 4}},
		{"gradual", []float64{0, .5, 2, 3}, []float64{0, 1, 2, 5}},
		{"spike", []float64{0, 1, 2, 3, 4}, []float64{1, 1, 100, 1, 1}},
		{"oscillation", []float64{0, 1, 2, 3, 4}, []float64{0, 5, 1, 9, 0}},
		{"two points", []float64{0, 3}, []float64{0, 1}},
		{"small", []float64{0, 1, 2}, []float64{0, 1e-15, 0}},
		{"large", []float64{0, 1, 2}, []float64{0, 1e18, 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			curve := newTrafficExperimentCurve(test.x, test.y)
			for i := 0; i < len(test.x)-1; i++ {
				for j := 0; j <= 256; j++ {
					at := test.x[i] + (test.x[i+1]-test.x[i])*float64(j)/256
					value := curve.evaluate(i, at)
					if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value < min(test.y[i], test.y[i+1]) || value > max(test.y[i], test.y[i+1]) {
						t.Fatalf("interval %d at %g overshot source range: %g", i, at, value)
					}
				}
				if curve.evaluate(i, test.x[i]) != test.y[i] || curve.evaluate(i, test.x[i+1]) != test.y[i+1] {
					t.Fatal("PCHIP did not pass through an original sample")
				}
			}
		})
	}
}

func TestTrafficExperimentAlternativesKeepGapsZerosAndOriginalSamples(t *testing.T) {
	start := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	points := []trafficPoint{
		{At: start, Up: 4, Valid: true},
		{At: start.Add(time.Second), Up: 8, Valid: true},
		{At: start.Add(2 * time.Second), Valid: false},
		{At: start.Add(3 * time.Second), Up: 12, Valid: true},
		{At: start.Add(7 * time.Second), Up: 5, Valid: true},
		{At: start.Add(8 * time.Second), Up: 5, Valid: true},
		{At: start.Add(9 * time.Second), Valid: true},
		{At: start.Add(10 * time.Second), Valid: true},
		{At: start.Add(11 * time.Second), Up: 7, Valid: true},
	}
	before := append([]trafficPoint(nil), points...)
	const columns, rows = 30, 6
	position := func(at time.Time, rate float64) (int, int) {
		return int(at.Sub(start).Seconds() * 2), 23 - int(math.Round(rate))
	}
	smooth := trafficExperimentSmooth(points, start, start.Add(60*time.Second), columns, rows, position)
	thick := trafficExperimentThicken(smooth, columns, rows)
	for _, cells := range [][]trafficChartCell{smooth, thick} {
		for _, x := range []int{4, 7, 8, 9, 10, 11, 12, 13, 19} {
			for y := range rows {
				cell := cells[y*columns+x/2]
				// Choose complete character columns in the missing intervals and
				// the middle of the sustained zero span.
				mask := uint8(0x47)
				if x%2 != 0 {
					mask = 0xb8
				}
				if cell.up&mask != 0 {
					t.Fatalf("alternative crossed missing or zero-only data at x=%d", x)
				}
			}
		}
		for _, point := range points {
			if !point.Valid || point.Up == 0 {
				continue
			}
			x, y := position(point.At, float64(point.Up))
			bits := [4][2]uint8{{1, 8}, {2, 16}, {4, 32}, {64, 128}}
			if cells[(y/4)*columns+x/2].up&bits[y%4][x%2] == 0 {
				t.Fatal("alternative path omitted an original valid sample")
			}
		}
		for _, cell := range cells {
			if cell.down != 0 {
				t.Fatal("an all-zero direction produced a curve")
			}
		}
	}
	for i := range points {
		if points[i] != before[i] {
			t.Fatal("experimental rendering changed source history")
		}
	}
}

func TestTrafficExperimentThickOnlyExpandsInsideTheSameSubpointColumn(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {2, 4}, {12, 6}} {
		columns, rows := size[0], size[1]
		original := make([]trafficChartCell, columns*rows)
		trafficChartDot(original, columns, rows, 0, 0, true)
		trafficChartDot(original, columns, rows, columns*2-1, rows*4-1, false)
		thick := trafficExperimentThicken(original, columns, rows)
		if len(thick) != len(original) {
			t.Fatal("thick path changed canvas bounds")
		}
		for index, cell := range original {
			if thick[index].up&cell.up != cell.up || thick[index].down&cell.down != cell.down {
				t.Fatal("thick path erased an original subpoint")
			}
		}
		for x := 1; x < columns-1; x++ {
			for y := range rows {
				if thick[y*columns+x] != (trafficChartCell{}) {
					t.Fatal("thick path expanded horizontally into an unobserved gap")
				}
			}
		}
	}
}

type trafficExperimentScenario struct {
	name, description string
	points            []trafficPoint
}

func trafficExperimentScenarios(now time.Time) []trafficExperimentScenario {
	cases := []trafficExperimentScenario{
		{name: "steady", description: "Stable rates / 稳定流量"},
		{name: "gradual", description: "Slow changes / 缓慢变化"},
		{name: "spike", description: "One-second bursts / 突然尖峰"},
		{name: "oscillation", description: "Fast oscillation / 高频波动"},
		{name: "crossing", description: "Crossing directions / 双曲线交叉"},
		{name: "zero-gap", description: "Zero transitions and real gaps / 零值与缺测"},
	}
	for index := range cases {
		for second := 0; second <= 60; second++ {
			point := trafficPoint{At: now.Add(time.Duration(second-60) * time.Second), Valid: true}
			switch cases[index].name {
			case "steady":
				point.Up, point.Down = 384<<10, 896<<10
			case "gradual":
				point.Up, point.Down = int64(64+second*8)<<10, int64(1024-second*10)<<10
			case "spike":
				point.Up, point.Down = 128<<10, 256<<10
				if second == 19 || second == 42 {
					point.Up = 1536 << 10
				}
				if second == 20 || second == 43 {
					point.Down = 1792 << 10
				}
			case "oscillation":
				pattern := [...]int64{128, 900, 256, 1400, 512, 1700, 128}
				point.Up, point.Down = pattern[second%len(pattern)]<<10, pattern[(second+3)%len(pattern)]<<10
			case "crossing":
				point.Up, point.Down = int64(second*24)<<10, int64((60-second)*24)<<10
			case "zero-gap":
				if second >= 8 && second <= 16 || second >= 27 && second <= 43 || second >= 52 {
					point.Up = int64(384+(second%5)*64) << 10
				}
				if second >= 3 && second <= 24 || second >= 36 && second <= 54 {
					point.Down = int64(768+(second%6)*80) << 10
				}
				if second == 32 || second == 33 {
					point.Valid = false
				}
				if second >= 45 && second <= 48 {
					continue // A genuine timestamp gap, rather than a false zero.
				}
			}
			cases[index].points = append(cases[index].points, point)
		}
	}
	return cases
}

func trafficExperimentOccupancy(cells []trafficChartCell) (visible, shared int) {
	for _, cell := range cells {
		if cell.up|cell.down != 0 {
			visible++
		}
		if cell.up != 0 && cell.down != 0 {
			shared++
		}
	}
	return
}

// TestTrafficChartRenderExperiments is a reproducible developer preview, not a
// new user preference. Every alternative shares source history, time, scale,
// geometry and the production ANSI/Braille presentation function.
func TestTrafficChartRenderExperiments(t *testing.T) {
	output := os.Getenv("LAZYCLASH_TRAFFIC_CHART_EXPERIMENTS")
	if output == "" {
		t.Skip("set LAZYCLASH_TRAFFIC_CHART_EXPERIMENTS to export ANSI comparisons")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	now := trafficChartTestNow()
	scale := trafficChartScale{maximum: 2 << 20, top: "2 MiB/s", mid: "1 MiB/s"}
	var manifest []map[string]any
	for _, scenario := range trafficExperimentScenarios(now) {
		input, err := json.Marshal(scenario.points)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, scenario.name+"-data.json"), append(input, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(input))
		for _, size := range [][2]int{{128, 6}, {80, 4}, {40, 4}} {
			width, height := size[0], size[1]
			g := newTrafficChartGeometry(width, height, scale, now)
			o := dashboardOptions{Locale: i18n.New("zh-Hans"), TrafficNow: now, TrafficHistory: scenario.points}
			original := trafficChartOriginalCells(scenario.points, g)
			schemes := []struct {
				name, description string
				cells             []trafficChartCell
			}{
				{"Original", "A  Original / Bresenham", original},
				{"Thick", "B  Thick / +1 vertical subpoint", trafficExperimentThicken(original, g.columns, g.height)},
				{"Smooth", "C  Smooth / shape-preserving PCHIP", trafficExperimentSmooth(scenario.points, g.start, g.now, g.columns, g.height, g.position)},
			}
			statistics := make(map[string]map[string]int)
			var lines []string
			line := func(value, foreground string) {
				value = cellCrop(value, width)
				lines = append(lines, themed(value+strings.Repeat(" ", max(0, width-ansi.StringWidth(value))), foreground, panel, false))
			}
			line(scenario.description, text)
			line(fmt.Sprintf("Same data / 60s / %dx%d / shared Y 0-2 MiB/s", width, height), muted)
			colors := trafficCurveColors()
			line(themed("↑ 上传", colors.upload, panel, false)+"   "+themed("↓ 下载", colors.download, panel, false)+"   "+themed("共享字符格", colors.overlap, panel, false), text)
			for _, scheme := range schemes {
				line(scheme.description, text)
				rows := trafficChartRows(o, g, scheme.cells)
				for _, row := range rows {
					if ansi.StringWidth(row) != width {
						t.Fatalf("%s/%s: row width %d, want %d", scenario.name, scheme.name, ansi.StringWidth(row), width)
					}
				}
				lines = append(lines, rows...)
				line("", muted)
				visible, shared := trafficExperimentOccupancy(scheme.cells)
				statistics[scheme.name] = map[string]int{"visible_cells": visible, "mixed_cells": shared}
			}
			for _, profile := range []struct {
				name  string
				value colorprofile.Profile
			}{{"truecolor", colorprofile.TrueColor}, {"256", colorprofile.ANSI256}, {"no-color", colorprofile.ASCII}} {
				name := fmt.Sprintf("%s-%dx%d-%s", scenario.name, width, height, profile.name)
				content := terminalTheme(strings.Join(lines, "\n"), profile.value)
				if err := os.WriteFile(filepath.Join(output, name+".ansi"), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(output, name+".txt"), []byte(ansi.Strip(content)), 0600); err != nil {
					t.Fatal(err)
				}
				manifest = append(manifest, map[string]any{
					"name": name, "scenario": scenario.name, "columns": width, "plot_rows": height,
					"rows": len(lines), "profile": profile.name, "data_sha256": digest,
					"window_seconds": 60, "y_maximum": 2 << 20, "schemes": statistics,
				})
			}
		}
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "manifest.json"), append(encoded, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
