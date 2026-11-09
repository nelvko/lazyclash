package tui

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

func trafficScaleTestTime() time.Time {
	return time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
}

func appendTrafficScaleObservation(state *trafficScaleState, history *[]trafficPoint, at time.Time, up, down int64, valid bool) {
	*history = append(*history, trafficPoint{At: at, Up: up, Down: down, Valid: valid})
	state.observe(*history, at)
}

func TestTrafficScaleWindowMaximumUsesBothDirectionsAndActualTime(t *testing.T) {
	now := trafficScaleTestTime()
	history := []trafficPoint{
		{At: now.Add(-61 * time.Second), Up: math.MaxInt64, Valid: true},
		{At: now.Add(-60 * time.Second), Up: 900, Down: 5, Valid: true},
		{At: now.Add(-30 * time.Second), Up: 10, Down: 1400, Valid: true},
		{At: now.Add(-time.Second), Up: math.MaxInt64},
		{At: now, Up: -1, Down: math.MaxInt64, Valid: true},
		{At: now, Up: math.MaxInt64, Down: -1, Valid: true},
		{At: now.Add(time.Nanosecond), Up: math.MaxInt64, Valid: true},
		{Up: math.MaxInt64, Valid: true},
		{At: now, Up: 0, Down: 0, Valid: true},
	}
	before := slices.Clone(history)
	maximum, found := trafficWindowMaximum(history, now)
	if maximum != 1400 || !found || !slices.Equal(history, before) {
		t.Fatalf("window maximum=%d found=%t; expected only real in-window values and unchanged history", maximum, found)
	}
	maximum, found = trafficWindowMaximum([]trafficPoint{{At: now, Valid: true}}, now)
	if maximum != 0 || !found {
		t.Fatal("a genuine zero observation was mistaken for missing data")
	}
	maximum, found = trafficWindowMaximum([]trafficPoint{{At: now, Up: 9000}}, now)
	if maximum != 0 || found {
		t.Fatal("an invalid observation contributed a peak")
	}
	maximum, found = trafficWindowMaximum(nil, now)
	if maximum != 0 || found {
		t.Fatal("an empty window invented a peak")
	}
}

func TestTrafficScaleHasFiniteHeadroomAndReasonableMinimum(t *testing.T) {
	for _, maximum := range []int64{math.MinInt64, -1, 0, 1, 20, 1023, 1024, 1300, 2 << 20, math.MaxInt64} {
		scale := makeTrafficChartScale(maximum)
		if math.IsInf(scale.maximum, 0) || math.IsNaN(scale.maximum) || scale.maximum < 1024 || scale.maximum <= 0 {
			t.Fatalf("peak %d produced an invalid range: %+v", maximum, scale)
		}
		if maximum > 0 && scale.maximum < float64(maximum)*1.15 {
			t.Fatalf("peak %d has no requested top margin: %+v", maximum, scale)
		}
		if !strings.Contains(scale.top, "/s") || !strings.Contains(scale.mid, "/s") || strings.Contains(scale.top, "Inf") || strings.Contains(scale.top, "NaN") {
			t.Fatalf("peak %d produced unreadable tick labels: %+v", maximum, scale)
		}
	}
	for peak := int64(900); peak < math.MaxInt64/100; peak = peak*13/10 + 1 {
		scale := makeTrafficChartScale(peak)
		if scale.maximum < float64(peak)*1.15 || scale.maximum > float64(peak)*1.54 {
			t.Fatalf("friendly ceiling leaves an excessive or insufficient range for %d: %g", peak, scale.maximum)
		}
	}
	for _, sample := range []struct {
		peak int64
		top  string
	}{
		{0, "1 KiB/s"},
		{1024, "1.25 KiB/s"},
		{2 << 20, "2.5 MiB/s"},
		{math.MaxInt64, "10 EiB/s"},
	} {
		if got := makeTrafficChartScale(sample.peak).top; got != sample.top {
			t.Fatalf("peak %d tick=%q, want %q", sample.peak, got, sample.top)
		}
	}
}

func TestTrafficScaleExpandsImmediatelyAndKeepsWindowPeakUntilItExpires(t *testing.T) {
	base := trafficScaleTestTime()
	var state trafficScaleState
	var history []trafficPoint
	appendTrafficScaleObservation(&state, &history, base, 1000, 0, true)
	initial := state.current.maximum
	appendTrafficScaleObservation(&state, &history, base.Add(time.Second), 0, 100000, true)
	expanded := state.current.maximum
	if expanded <= initial || expanded < 115000 || !state.lowSince.IsZero() {
		t.Fatal("a new download peak did not expand the shared range immediately")
	}
	for second := 2; second <= 61; second++ {
		appendTrafficScaleObservation(&state, &history, base.Add(time.Duration(second)*time.Second), 0, 0, true)
		if state.current.maximum != expanded || !state.lowSince.IsZero() {
			t.Fatalf("a real peak still inside the 60-second window was ignored at second %d", second)
		}
	}
	appendTrafficScaleObservation(&state, &history, base.Add(62*time.Second), 0, 0, true)
	if state.current.maximum != expanded || !state.lowSince.Equal(base.Add(62*time.Second)) {
		t.Fatal("an expired peak shrank immediately instead of starting the low-peak hold")
	}
	for second := 63; second < 72; second++ {
		appendTrafficScaleObservation(&state, &history, base.Add(time.Duration(second)*time.Second), 0, 0, true)
		if state.current.maximum != expanded {
			t.Fatalf("range shrank before ten seconds of low observations: %d", second)
		}
	}
	appendTrafficScaleObservation(&state, &history, base.Add(72*time.Second), 0, 0, true)
	if state.current.maximum != 1024 || !state.lowSince.IsZero() {
		t.Fatal("ten seconds of genuine zeros did not return to the minimum range")
	}
}

func TestTrafficScaleThresholdRecoveryRestartsTheShrinkDelay(t *testing.T) {
	base := trafficScaleTestTime()
	var state trafficScaleState
	var history []trafficPoint
	appendTrafficScaleObservation(&state, &history, base.Add(-61*time.Second), 100000, 0, true)
	high := state.current.maximum
	for second := 0; second < 5; second++ {
		appendTrafficScaleObservation(&state, &history, base.Add(time.Duration(second)*time.Second), 1000, 0, true)
	}
	if !state.lowSince.Equal(base) {
		t.Fatal("low range did not start its hold")
	}
	// This medium peak is below the old range, but above its shrink threshold.
	appendTrafficScaleObservation(&state, &history, base.Add(5*time.Second), 60000, 0, true)
	if state.current.maximum != high || !state.lowSince.IsZero() {
		t.Fatal("threshold recovery retained the old shrink deadline")
	}
	for second := 6; second <= 65; second++ {
		appendTrafficScaleObservation(&state, &history, base.Add(time.Duration(second)*time.Second), 1000, 0, true)
		if state.current.maximum != high || !state.lowSince.IsZero() {
			t.Fatal("medium peak still in the window allowed contraction")
		}
	}
	for second := 66; second < 76; second++ {
		appendTrafficScaleObservation(&state, &history, base.Add(time.Duration(second)*time.Second), 1000, 0, true)
		if state.current.maximum != high {
			t.Fatal("old low-peak deadline survived the threshold interruption")
		}
	}
	appendTrafficScaleObservation(&state, &history, base.Add(76*time.Second), 1000, 0, true)
	if state.current.maximum >= high || !state.lowSince.IsZero() {
		t.Fatal("a new continuous low interval failed to contract")
	}
}

func TestTrafficScaleMissingSamplesAndLongGapsDoNotCountAsLowTraffic(t *testing.T) {
	base := trafficScaleTestTime()
	for _, missing := range []bool{true, false} {
		name := "timestamp gap"
		if missing {
			name = "explicit missing frame"
		}
		t.Run(name, func(t *testing.T) {
			var state trafficScaleState
			var history []trafficPoint
			appendTrafficScaleObservation(&state, &history, base.Add(-61*time.Second), 100000, 0, true)
			high := state.current.maximum
			for second := 0; second < 5; second++ {
				appendTrafficScaleObservation(&state, &history, base.Add(time.Duration(second)*time.Second), 1000, 0, true)
			}
			recoveredAt := 10
			if missing {
				before := state
				appendTrafficScaleObservation(&state, &history, base.Add(5*time.Second), 0, 0, false)
				if state.current != before.current || state.lastAt != before.lastAt || !state.lowSince.IsZero() {
					t.Fatal("missing frame changed range or failed to interrupt the low interval")
				}
				recoveredAt = 6
			}
			for second := recoveredAt; second < recoveredAt+10; second++ {
				appendTrafficScaleObservation(&state, &history, base.Add(time.Duration(second)*time.Second), 1000, 0, true)
				if state.current.maximum != high || !state.lowSince.Equal(base.Add(time.Duration(recoveredAt)*time.Second)) {
					t.Fatal("missing time was counted toward the new shrink delay")
				}
			}
			appendTrafficScaleObservation(&state, &history, base.Add(time.Duration(recoveredAt+10)*time.Second), 1000, 0, true)
			if state.current.maximum >= high {
				t.Fatal("valid recovery observations could not start a new shrink interval")
			}
		})
	}
}

func TestTrafficScaleIgnoresDuplicateOldAndMalformedObservations(t *testing.T) {
	base := trafficScaleTestTime()
	var state trafficScaleState
	var history []trafficPoint
	appendTrafficScaleObservation(&state, &history, base.Add(-61*time.Second), 100000, 0, true)
	for second := 0; second <= 4; second++ {
		appendTrafficScaleObservation(&state, &history, base.Add(time.Duration(second)*time.Second), 1000, 0, true)
	}
	before := state
	for _, at := range []time.Time{base.Add(4 * time.Second), base.Add(3 * time.Second), {}} {
		copyHistory := append(slices.Clone(history), trafficPoint{At: at, Up: math.MaxInt64, Valid: true})
		state.observe(copyHistory, at)
		if state != before {
			t.Fatal("duplicate, old or zero timestamp changed the range or hold")
		}
	}
	for _, point := range []trafficPoint{
		{At: base.Add(4 * time.Second), Up: 1000, Valid: true},
		{At: base.Add(5 * time.Second), Up: -1, Down: 1000, Valid: true},
		{At: base.Add(5 * time.Second), Up: 1000, Down: -1, Valid: true},
	} {
		state = before
		copyHistory := append(slices.Clone(history), point)
		state.observe(copyHistory, base.Add(5*time.Second))
		if state.current != before.current || state.lastAt != before.lastAt || !state.lowSince.IsZero() {
			t.Fatal("malformed current sample advanced the range or kept the low-peak hold")
		}
	}
}

func TestTrafficScaleNewHistoryResetsItsPresentationRange(t *testing.T) {
	base := trafficScaleTestTime()
	var state trafficScaleState
	state.observe([]trafficPoint{{At: base, Up: 100000, Valid: true}}, base)
	newAt := base.Add(time.Second)
	state.observe([]trafficPoint{{At: newAt, Valid: true}}, newAt)
	if state.current.maximum != 1024 || !state.lowSince.IsZero() || state.lastAt != newAt {
		t.Fatal("a new core's first observation inherited the previous range")
	}
}

func TestTrafficScaleUnitBoundaryDoesNotOscillateWithSmallPeaks(t *testing.T) {
	base := trafficScaleTestTime()
	var state trafficScaleState
	var history []trafficPoint
	appendTrafficScaleObservation(&state, &history, base, 900<<10, 0, true)
	initial := state.current
	if !strings.Contains(initial.top, "MiB/s") {
		t.Fatal("fixture did not start beyond the binary unit boundary")
	}
	peaks := []int64{850 << 10, 875 << 10, 899 << 10, 860 << 10}
	for second := 1; second <= 120; second++ {
		appendTrafficScaleObservation(&state, &history, base.Add(time.Duration(second)*time.Second), peaks[second%len(peaks)], 0, true)
		if state.current != initial {
			t.Fatal("small fluctuations caused range or KiB/MiB label changes")
		}
	}
}

func TestTrafficScaleRepaintDoesNotAdvanceStateOrMutateHistory(t *testing.T) {
	base := trafficScaleTestTime()
	var state trafficScaleState
	var history []trafficPoint
	appendTrafficScaleObservation(&state, &history, base.Add(-61*time.Second), 100000, 0, true)
	for second := 0; second <= 4; second++ {
		appendTrafficScaleObservation(&state, &history, base.Add(time.Duration(second)*time.Second), 1000, 0, true)
	}
	beforeState, beforeHistory := state, slices.Clone(history)
	for _, width := range []int{1, 8, 20, 40, 80, 160} {
		for _, height := range []int{1, 4, 6, 9} {
			_ = overviewTrafficChart(dashboardOptions{TrafficNow: base.Add(time.Hour), TrafficHistory: history, TrafficScale: state.current}, width, height)
		}
	}
	if state != beforeState || !slices.Equal(history, beforeHistory) {
		t.Fatal("repaint or resize changed scale timing or source data")
	}
}
