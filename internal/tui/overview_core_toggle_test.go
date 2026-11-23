package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

type coreToggleTestBackend struct {
	*fakeBackend
	reads   int
	observe func(int) (app.Snapshot, error)
}

func (b *coreToggleTestBackend) Snapshot(context.Context) (app.Snapshot, error) {
	b.reads++
	if b.observe != nil {
		return b.observe(b.reads)
	}
	return b.snapshot, b.snapshotErr
}
func coreToggleTestModel(t *testing.T, active bool) (Model, *coreToggleTestBackend) {
	t.Helper()
	state := "inactive"
	if active {
		state = "active"
	}
	b := &coreToggleTestBackend{fakeBackend: &fakeBackend{snapshot: app.Snapshot{
		Installed: true, ConfigReady: true, HasRun: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
		Service: service.Status{Active: active, State: state, PID: 42}, ServiceObservedAt: time.Now(),
		Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
		Desired:      app.DesiredState{Known: true, Running: active},
	}}}
	m := ready(b.fakeBackend)
	m.backend = b
	m.page, m.focus, m.width, m.height = "overview", "core.toggle", 160, 45
	m.layout()
	return m, b
}
func coreToggleStateOf(m Model) toggleState {
	return overviewCoreToggle(m.navigationOptions()).state
}

func TestOverviewCoreToggleStartsAndStopsOnlyAfterObservedCompletion(t *testing.T) {
	for _, active := range []bool{false, true} {
		for _, activation := range []tea.KeyPressMsg{special(tea.KeyEnter), {Code: ' ', Text: " "}} {
			t.Run(fmt.Sprintf("active=%t/key=%s", active, activation.String()), func(t *testing.T) {
				m, b := coreToggleTestModel(t, active)
				defer m.stopRuntime()
				before := m.snapshot.Service
				action := "start"
				want := toggleOn
				if active {
					action, want = "stop", toggleOff
				}
				calls := 0
				b.control = func(_ context.Context, got string) error {
					calls++
					if b.reads < 1 || got != action {
						t.Fatalf("operation did not preflight real state: reads=%d action=%s", b.reads, got)
					}
					b.snapshot.Service.Active = !active
					b.snapshot.Service.State = "active"
					if active {
						b.snapshot.Service.State = "inactive"
						b.snapshot.Service.PID = 0
					}
					b.snapshot.Desired.Running = !active
					b.snapshot.ServiceObservedAt = time.Now()
					return nil
				}
				m, cmd := update(m, activation)
				if cmd == nil || calls != 0 || b.reads != 0 || !m.busy || m.snapshot.Service != before || coreToggleStateOf(m) != togglePending {
					t.Fatal("activation ran inline or optimistically confirmed state")
				}
				for _, repeat := range []tea.KeyPressMsg{special(tea.KeyEnter), {Code: ' ', Text: " "}} {
					operationID := m.id
					next, _ := update(m, repeat)
					m = next
					if m.id != operationID || calls != 0 || b.reads != 0 || !m.busy {
						t.Fatal("pending control accepted duplicate submission")
					}
				}
				result := actionCmd(t, cmd)()
				if calls != 1 || b.reads != 2 || m.snapshot.Service != before || coreToggleStateOf(m) != togglePending {
					t.Fatalf("observation bypassed async delivery: calls=%d reads=%d", calls, b.reads)
				}
				m, _ = update(m, result)
				if m.busy || coreToggleStateOf(m) != want || m.snapshot.Service != b.snapshot.Service {
					t.Fatal("toggle did not adopt confirmed actual state")
				}
				// A held activation key must not reverse a just-completed operation.
				repeat := activation
				repeat.IsRepeat = true
				operationID := m.id
				m, _ = update(m, repeat)
				if m.id != operationID || m.busy || calls != 1 {
					t.Fatal("key repeat reversed the confirmed state")
				}
			})
		}
	}
}

func TestOverviewCoreToggleRechecksExternalChangesBeforeControl(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		active      bool
		want        toggleState
	}{
		{"already started externally", "active", true, toggleOn},
		{"external transition", "activating", false, togglePending},
		{"unknown observation", "unknown", false, toggleUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, b := coreToggleTestModel(t, false)
			defer m.stopRuntime()
			calls := 0
			b.control = func(context.Context, string) error { calls++; return nil }
			b.snapshot.Service = service.Status{State: tc.state, Active: tc.active, PID: 43}
			b.snapshot.ServiceObservedAt = time.Now()
			cmd := m.coreToggleAction()
			if cmd == nil {
				t.Fatal("initial known stopped observation did not create command")
			}
			m, _ = update(m, actionCmd(t, cmd)())
			if calls != 0 || m.busy || coreToggleStateOf(m) != tc.want {
				t.Fatalf("preflight inverted or blindly changed external state: calls=%d state=%d", calls, coreToggleStateOf(m))
			}
		})
	}
}

func TestOverviewCoreToggleFailureRereadsActualState(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(fmt.Sprintf("active=%t", active), func(t *testing.T) {
			m, b := coreToggleTestModel(t, active)
			defer m.stopRuntime()
			calls := 0
			b.control = func(context.Context, string) error {
				calls++
				if !active {
					b.snapshot.Service.State = "failed"
					b.snapshot.Service.Result = "exit-code"
					b.snapshot.Service.ExitCode = 1
					b.snapshot.Service.PID = 0
				}
				b.snapshot.ServiceObservedAt = time.Now()
				return errors.New("fixture lifecycle failure")
			}
			cmd := m.coreToggleAction()
			m, _ = update(m, actionCmd(t, cmd)())
			want := toggleOff
			if active {
				want = toggleOn
			}
			if calls != 1 || b.reads != 2 || m.busy || m.failure == "" || coreToggleStateOf(m) != want {
				t.Fatalf("failure did not report refreshed real state: calls=%d reads=%d state=%d failure=%q", calls, b.reads, coreToggleStateOf(m), m.failure)
			}
		})
	}
}

func TestOverviewCoreToggleFailedObservationNeverClaimsSuccess(t *testing.T) {
	m, b := coreToggleTestModel(t, false)
	defer m.stopRuntime()
	b.control = func(context.Context, string) error {
		b.snapshotErr = errors.New("status unavailable after start")
		return nil
	}
	cmd := m.coreToggleAction()
	m, _ = update(m, actionCmd(t, cmd)())
	if m.busy || m.failure == "" || coreToggleStateOf(m) == toggleOn || coreToggleStateOf(m) == toggleOff {
		t.Fatal("failed postread was presented as confirmed state")
	}
	if next := m.coreToggleAction(); next != nil {
		t.Fatal("unknown state allowed blind toggle")
	}
}

func TestOverviewCoreToggleReadFailureRejectsControl(t *testing.T) {
	m, b := coreToggleTestModel(t, false)
	defer m.stopRuntime()
	calls := 0
	b.control = func(context.Context, string) error { calls++; return nil }
	b.snapshotErr = errors.New("fixture preflight unavailable")
	cmd := m.coreToggleAction()
	m, _ = update(m, actionCmd(t, cmd)())
	if calls != 0 || m.failure == "" || m.busy {
		t.Fatal("failed preflight still changed the service")
	}
}

func TestOverviewCoreToggleExternalStatusAndLateSnapshotsKeepOrdering(t *testing.T) {
	m, _ := coreToggleTestModel(t, false)
	defer m.stopRuntime()
	base := time.Now().Add(-time.Minute)
	m.snapshot.ServiceObservedAt = base
	generation := m.statusGeneration
	cmd := m.coreToggleAction()
	if cmd == nil || coreToggleStateOf(m) != togglePending {
		t.Fatal("start did not become pending")
	}
	actionID := m.id
	stale := m.snapshot
	stale.Service = service.Status{Active: true, State: "active", PID: 100}
	stale.ServiceObservedAt = base.Add(time.Second)
	m, _ = update(m, resultMsg{id: actionID - 1, snapshot: stale, hasSnapshot: true})
	m, _ = update(m, coreStatusMsg{generation: generation, snapshot: stale})
	if !m.busy || m.snapshot.Service.Active || coreToggleStateOf(m) != togglePending {
		t.Fatal("old job/status result overwrote pending toggle")
	}
	observed := base.Add(20 * time.Second)
	current := service.Status{State: "active", Active: true, PID: 101, StartedAt: observed.Add(-10 * time.Second)}
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 1, ObservedAt: observed, Snapshot: app.RuntimeSnapshot{State: "ready", Service: current, RunningVersion: "v1.19.33"}})
	if coreToggleStateOf(m) != togglePending {
		t.Fatal("heartbeat bypassed pending operation confirmation")
	}
	old := m.snapshot
	old.Service = service.Status{State: "inactive"}
	old.ServiceObservedAt = base.Add(10 * time.Second)
	m, _ = update(m, resultMsg{id: actionID, snapshot: old, hasSnapshot: true, actionSucceeded: true})
	if m.busy || m.snapshot.Service != current || coreToggleStateOf(m) != toggleOn {
		t.Fatal("late postread overwrote newer external service observation")
	}
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 2, ObservedAt: observed.Add(time.Second), Snapshot: app.RuntimeSnapshot{State: "stopped", Service: service.Status{State: "inactive"}}})
	if coreToggleStateOf(m) != toggleOff {
		t.Fatal("external stop did not synchronize toggle")
	}
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 1, ObservedAt: observed.Add(2 * time.Second), Snapshot: app.RuntimeSnapshot{State: "ready", Service: current}})
	if coreToggleStateOf(m) != toggleOff {
		t.Fatal("old instance restarted current toggle")
	}
}

func TestOverviewCoreToggleCardHasStableCompactLayoutAcrossLifecycle(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{4, 10, 20, 40, 72, 100} {
			t.Run(fmt.Sprintf("%s/%d", language, width), func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), Now: time.Now(), Snapshot: app.Snapshot{Installed: true, Version: "v1.19.33", RunningVersion: "v1.19.33", Service: service.Status{State: "active", Active: true, PID: 10, StartedAt: time.Now().Add(-time.Hour)}}}
				height, y := -1, -1
				for _, tc := range []struct {
					state, action string
					active        bool
				}{
					{"active", "", true}, {"inactive", "", false}, {"activating", "", false}, {"deactivating", "", true}, {"reloading", "", true}, {"active", "restart", true},
				} {
					o.Snapshot.Service.State, o.Snapshot.Service.Active = tc.state, tc.active
					o.CoreAction = tc.action
					o.Busy = tc.action != ""
					card := overviewCoreCard(o, width)
					assertCanvas(t, strings.Join(card.lines, "\n"), width, len(card.lines))
					assertRegions(t, card.targets, width, len(card.lines))
					for _, removed := range []string{"core.start", "core.stop", "core.restart", "card.logs"} {
						if hasAction(card.targets, removed) {
							t.Fatal("old action survived", removed)
						}
					}
					if width >= 40 {
						if len(card.targets) != 1 || card.targets[0].ID != "core.toggle" {
							t.Fatalf("single focusable toggle lost: %+v", card.targets)
						}
						if height < 0 {
							height, y = len(card.lines), card.targets[0].Y
						}
						if len(card.lines) != height || card.targets[0].Y != y {
							t.Fatalf("lifecycle changed card geometry: %s/%s h=%d/y=%d want %d/%d", tc.state, tc.action, len(card.lines), card.targets[0].Y, height, y)
						}
					}
				}
			})
		}
	}
}

func TestOverviewCoreToggleUnknownUnavailableAndTransitionsStayInert(t *testing.T) {
	for _, tc := range []struct {
		name, state               string
		active, unsupported, busy bool
		problem                   string
	}{
		{name: "unknown", state: "unknown"},
		{name: "inconsistent active flag", state: "active"},
		{name: "unavailable", state: "active", active: true, problem: "fixture manager unreadable"},
		{name: "unsupported", state: "active", active: true, unsupported: true},
		{name: "starting", state: "activating"},
		{name: "stopping", state: "deactivating", active: true},
		{name: "restarting", state: "reloading", active: true},
		{name: "another action busy", state: "active", active: true, busy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, b := coreToggleTestModel(t, tc.active)
			defer m.stopRuntime()
			m.snapshot.Service.State, m.snapshot.Service.Active = tc.state, tc.active
			m.snapshot.ServiceError = tc.problem
			m.snapshot.Capabilities.Runtime.Supported = !tc.unsupported
			m.busy = tc.busy
			m.focus = "core.toggle"
			m.layout()
			if !hasAction(m.targets, "core.toggle") || m.focus != "core.toggle" {
				t.Fatal("nonactionable observation lost toggle focus")
			}
			calls := 0
			b.control = func(context.Context, string) error { calls++; return nil }
			id := m.id
			if cmd := m.activate("core.toggle"); cmd != nil {
				t.Fatal("inert mouse action created service operation")
			}
			for _, input := range []tea.KeyPressMsg{special(tea.KeyEnter), {Code: ' ', Text: " "}} {
				m, _ = update(m, input)
				if calls != 0 || b.reads != 0 || m.id != id {
					t.Fatal("inert state accepted service operation")
				}
			}
		})
	}
}

func TestOverviewCoreUptimeKeepsActionInPlace(t *testing.T) {
	now := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)
	for _, language := range []string{i18n.English, i18n.Chinese} {
		// Include narrow cards where translated headings and uptimes wrap.
		for _, width := range []int{5, 8, 10, 20, 30, 33, 34, 40} {
			t.Run(fmt.Sprintf("%s/%d", language, width), func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), Now: now, Snapshot: app.Snapshot{
					Installed: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
					Service: service.Status{State: "active", Active: true, PID: 10},
				}}
				height := -1
				var target hitRegion
				longest := time.Duration(1<<63 - 1)
				for _, sample := range []struct {
					elapsed time.Duration
					want    string
				}{
					{time.Second, o.Locale.T("dashboard.core.uptimeUnderMinute")},
					{3 * time.Minute, o.Locale.T("dashboard.core.uptimeMinutes", 3)},
					{7 * time.Hour, o.Locale.T("dashboard.core.uptimeHours", 7)},
					{23*time.Hour + 59*time.Minute, o.Locale.T("dashboard.core.uptimeHoursMinutes", 23, 59)},
					{365 * 24 * time.Hour, o.Locale.T("dashboard.core.uptimeDays", 365)},
					{longest, o.Locale.T("dashboard.core.uptimeDaysHours", int(longest/(24*time.Hour)), int(longest/time.Hour%24))},
				} {
					for _, phase := range []string{"on", "pending", "off"} {
						o.Snapshot.Service.StartedAt = now.Add(-sample.elapsed)
						o.Snapshot.Service.State, o.Snapshot.Service.Active = "active", true
						o.CoreAction, o.Busy = "", false
						switch phase {
						case "pending":
							o.CoreAction, o.Busy = "stop", true
						case "off":
							o.Snapshot.Service.State, o.Snapshot.Service.Active = "inactive", false
						}
						card := overviewCoreCard(o, width)
						assertCanvas(t, strings.Join(card.lines, "\n"), width, len(card.lines))
						assertRegions(t, card.targets, width, len(card.lines))
						if len(card.targets) != 1 || card.targets[0].ID != "core.toggle" {
							t.Fatalf("missing single lifecycle control: %+v", card.targets)
						}
						if height < 0 {
							height, target = len(card.lines), card.targets[0]
						}
						if (width >= 30 && len(card.lines) != height) || card.targets[0] != target {
							t.Fatalf("%s/%s moved core toggle: height=%d target=%+v, want height=%d target=%+v", sample.elapsed, phase, len(card.lines), card.targets[0], height, target)
						}
						if phase == "on" && width >= 30 && !strings.Contains(strings.Join(card.lines, "\n"), sample.want) {
							t.Fatalf("uptime was dropped or truncated: %s", sample.elapsed)
						}
					}
				}
			})
		}
	}
}
