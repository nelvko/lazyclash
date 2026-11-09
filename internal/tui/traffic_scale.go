package tui

import (
	"fmt"
	"math"
	"time"
)

const (
	trafficScaleMinimum     = 1024.0
	trafficScaleHeadroom    = 1.15
	trafficScaleShrinkRatio = 0.6
	trafficScaleShrinkDelay = 10 * time.Second
)

type trafficChartScale struct {
	maximum  float64
	top, mid string
}

// This presentation state advances only on real, valid traffic observations.
// Repainting or resizing a terminal must not advance the shrink timer.
type trafficScaleState struct {
	current          trafficChartScale
	lowSince, lastAt time.Time
}

func trafficWindowMaximum(history []trafficPoint, now time.Time) (int64, bool) {
	var maximum int64
	found := false
	start := now.Add(-trafficHistoryWindow)
	for _, point := range history {
		if point.Valid && point.Up >= 0 && point.Down >= 0 && !point.At.IsZero() && !point.At.Before(start) && !point.At.After(now) {
			maximum = max(maximum, point.Up, point.Down)
			found = true
		}
	}
	return maximum, found
}

func (s *trafficScaleState) observe(history []trafficPoint, at time.Time) {
	if len(history) == 0 || at.IsZero() || !s.lastAt.IsZero() && !at.After(s.lastAt) {
		return
	}
	latest := history[len(history)-1]
	// A new collection must not inherit the old core's range, even when its
	// first observation is missing. Only a later valid sample initializes it.
	if len(history) == 1 && latest.At.Equal(at) {
		s.current, s.lowSince = trafficChartScale{}, time.Time{}
	}
	if !latest.At.Equal(at) || !latest.Valid || latest.Up < 0 || latest.Down < 0 {
		s.lowSince = time.Time{}
		return
	}
	if !s.lastAt.IsZero() && at.Sub(s.lastAt) > trafficHistoryMaxGap {
		s.lowSince = time.Time{}
	}
	maximum, found := trafficWindowMaximum(history, at)
	if !found {
		return
	}
	target := makeTrafficChartScale(maximum)
	fresh := s.current.maximum <= 0
	s.lastAt = at
	if fresh || target.maximum > s.current.maximum {
		s.current, s.lowSince = target, time.Time{}
		return
	}
	if target.maximum > s.current.maximum*trafficScaleShrinkRatio {
		s.lowSince = time.Time{}
		return
	}
	if s.lowSince.IsZero() {
		s.lowSince = at
		return
	}
	if at.Sub(s.lowSince) >= trafficScaleShrinkDelay {
		s.current, s.lowSince = target, time.Time{}
	}
}

func makeTrafficChartScale(maximum int64) trafficChartScale {
	value := max(trafficScaleMinimum, float64(maximum)*trafficScaleHeadroom)
	units := [...]string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	divisor, unit := 1.0, 0
	for value/divisor >= 1024 && unit < len(units)-1 {
		divisor *= 1024
		unit++
	}
	ceiling := func(value float64) float64 {
		power := math.Pow(10, math.Floor(math.Log10(value)))
		for _, step := range [...]float64{1, 1.25, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10} {
			if bound := step * power; bound >= value {
				return bound
			}
		}
		return 10 * power
	}
	top := ceiling(value / divisor)
	if top >= 1024 && unit < len(units)-1 {
		divisor *= 1024
		unit++
		top = ceiling(value / divisor)
	}
	return trafficChartScale{maximum: top * divisor, top: fmt.Sprintf("%g %s/s", top, units[unit]), mid: fmt.Sprintf("%g %s/s", top/2, units[unit])}
}
