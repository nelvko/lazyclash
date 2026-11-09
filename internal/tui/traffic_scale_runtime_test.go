package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func trafficScaleRuntimeModel(t *testing.T) (Model, time.Time) {
	t.Helper()
	m := runtimeModel(t, &fakeBackend{})
	base := time.Now().Add(-30 * time.Second)
	sample := liveRuntime()
	sample.ObservedAt, sample.Service.PID = base, 42
	sample.Traffic = &mihomo.Traffic{}
	m = acceptRuntimeFixture(m, sample)
	// The old peak has expired, but its range is still in the low-peak hold.
	m.runtime.chartScale = trafficScaleState{
		current: makeTrafficChartScale(100000), lastAt: base,
		lowSince: base.Add(-8 * time.Second),
	}
	return m, base
}

func TestTrafficScaleRuntimeOnlyTrafficFramesAdvanceTheRange(t *testing.T) {
	m, base := trafficScaleRuntimeModel(t)
	before, history := m.runtime.chartScale, slices.Clone(m.runtime.history.points)
	memory := uint64(100)
	for _, event := range []app.RuntimeEvent{
		{Source: app.RuntimeMemory, Memory: &memory},
		{Source: app.RuntimeConnections, Connections: &mihomo.ConnectionSnapshot{}},
		{Source: app.RuntimeStatus, Snapshot: app.RuntimeSnapshot{State: "ready", Service: m.snapshot.Service}},
		{Source: app.RuntimeTraffic, Connected: true},
	} {
		event.Instance, event.ObservedAt = 1, base.Add(time.Second)
		m.acceptRuntimeEvent(event)
		if m.runtime.chartScale != before || !slices.Equal(history, m.runtime.history.points) {
			t.Fatalf("source %s advanced traffic presentation state", event.Source)
		}
	}
	for _, size := range [][2]int{{160, 45}, {80, 24}, {40, 16}, {120, 32}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		_ = m.View()
		if m.runtime.chartScale != before || !slices.Equal(history, m.runtime.history.points) {
			t.Fatal("rendering or resizing advanced traffic presentation state")
		}
	}
	at := base.Add(2 * time.Second)
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeTraffic, Instance: 1, ObservedAt: at, Traffic: &mihomo.Traffic{}})
	if m.runtime.chartScale.current.maximum != trafficScaleMinimum || m.runtime.chartScale.lastAt != at || !m.runtime.chartScale.lowSince.IsZero() {
		t.Fatal("a real valid traffic frame did not complete the ten-second hold")
	}
}

func TestTrafficScaleRuntimeMissingSourcesInterruptTheLowPeakHold(t *testing.T) {
	for _, source := range []string{"traffic", "status", "session"} {
		t.Run(source, func(t *testing.T) {
			m, base := trafficScaleRuntimeModel(t)
			before := m.runtime.chartScale
			failure := errors.New("temporary observation failure")
			switch source {
			case "traffic":
				m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeTraffic, Instance: 1, ObservedAt: base.Add(time.Second), Err: failure})
				points := m.runtime.history.points
				if len(points) != 2 || points[1].Valid {
					t.Fatal("traffic failure was not preserved as missing history")
				}
			case "status":
				m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 1, ObservedAt: base.Add(time.Second), Err: failure})
			case "session":
				if m.acceptRuntimeEnded(runtimeEndedMsg{generation: m.runtime.generation, err: failure}) == nil {
					t.Fatal("unexpected session end did not schedule recovery")
				}
			}
			if m.runtime.chartScale.current != before.current || m.runtime.chartScale.lastAt != before.lastAt || !m.runtime.chartScale.lowSince.IsZero() {
				t.Fatal("missing observation changed range or retained the low-peak deadline")
			}
			if source == "session" {
				return // This path uses the actual session-end clock, checked above.
			}
			at := base.Add(2 * time.Second)
			m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 1, ObservedAt: at, Snapshot: app.RuntimeSnapshot{State: "ready", Service: m.snapshot.Service}})
			m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeTraffic, Instance: 1, ObservedAt: at, Traffic: &mihomo.Traffic{}})
			if m.runtime.chartScale.current != before.current || m.runtime.chartScale.lowSince != at {
				t.Fatal("brief missing interval counted toward contraction after recovery")
			}
		})
	}
}

func TestTrafficScaleRuntimeNewCoreNeverInheritsThePreviousRange(t *testing.T) {
	for _, invalidFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid first frame", true: "missing first frame"}[invalidFirst], func(t *testing.T) {
			m, base := trafficScaleRuntimeModel(t)
			m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 2, ObservedAt: base.Add(time.Second), Snapshot: app.RuntimeSnapshot{
				State: "ready", Service: service.Status{Active: true, State: "active", PID: 43},
			}})
			options := m.dashboardOptions()
			if len(options.TrafficHistory) != 0 || options.TrafficScale.maximum != 0 {
				t.Fatal("a new core's empty chart retained the previous scale")
			}
			chart := ansi.Strip(strings.Join(overviewTrafficChart(options, 80, 6), "\n"))
			if !strings.Contains(chart, "1 KiB/s") || strings.Contains(chart, "MiB/s") {
				t.Fatal("a new core's empty chart did not use the minimum range")
			}
			at := base.Add(2 * time.Second)
			if invalidFirst {
				m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeTraffic, Instance: 2, ObservedAt: at, Err: errors.New("first frame missing")})
				if points := m.runtime.history.points; len(points) != 1 || points[0].Valid {
					t.Fatal("new history's missing first frame was changed into zero")
				}
				at = at.Add(time.Second)
			}
			m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeTraffic, Instance: 2, ObservedAt: at, Traffic: &mihomo.Traffic{}})
			if m.runtime.chartScale.current.maximum != trafficScaleMinimum || m.runtime.chartScale.lastAt != at || !m.runtime.chartScale.lowSince.IsZero() {
				t.Fatal("new core's first valid observation inherited the old peak range")
			}
		})
	}
}

func TestTrafficScaleRuntimeStoppedChartHidesItsPreviousRange(t *testing.T) {
	m, base := trafficScaleRuntimeModel(t)
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 1, ObservedAt: base.Add(time.Second), Snapshot: app.RuntimeSnapshot{
		State: "stopped", Service: service.Status{State: "inactive"},
	}})
	options := m.dashboardOptions()
	if len(options.TrafficHistory) != 0 || options.TrafficScale.maximum != 0 || !m.runtime.chartScale.lowSince.IsZero() {
		t.Fatal("stopping retained a visible range, history, or shrink deadline")
	}
}
