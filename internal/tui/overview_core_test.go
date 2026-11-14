package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestCoreStatusShowsOnlyCurrentStateVersionAndRealProcessUptime(t *testing.T) {
	now := time.Now()
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			o := dashboardOptions{Locale: i18n.New(language), Now: now, Snapshot: app.Snapshot{
				Installed: true, HasRun: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
				Service: service.Status{Active: true, State: "active", PID: 4242, StartedAt: now.Add(-2*time.Hour - 18*time.Minute - 34*time.Second)},
				Desired: app.DesiredState{Known: true, Running: true},
			}}
			render := func() (string, dashboardBlock) {
				block := overviewCoreCard(o, 72)
				return ansi.Strip(strings.Join(block.lines, "\n")), block
			}
			plain, block := render()
			status := "● " + o.Locale.T("dashboard.core.toggle.on")
			for _, want := range []string{o.Locale.T("dashboard.overview.service"), "Mihomo", status, "v1.19.33", o.Locale.T("dashboard.core.uptime", "02:18:34")} {
				if !strings.Contains(plain, want) {
					t.Fatalf("core status omitted %q:\n%s", want, plain)
				}
			}
			if strings.Count(plain, status) != 1 || strings.Count(plain, "v1.19.33") != 1 || strings.Contains(plain, "4242") {
				t.Fatalf("core retained repeated state or process diagnostics:\n%s", plain)
			}
			if heading := ansi.Strip(block.lines[1]); !strings.Contains(heading, o.Locale.T("dashboard.overview.service")) || !strings.HasSuffix(strings.TrimSpace(heading), "v1.19.33") {
				t.Fatalf("version not at heading right: %q", heading)
			}
			for _, line := range block.lines[2:] {
				if strings.Contains(ansi.Strip(line), "v1.19.33") {
					t.Fatal("version repeated in body")
				}
			}
			if len(block.targets) != 1 || block.targets[0].ID != "core.toggle" {
				t.Fatalf("core must expose exactly one toggle, got %+v", block.targets)
			}
			row := ansi.Strip(block.lines[block.targets[0].Y])
			if !strings.Contains(row, "02:18:34") || !strings.HasSuffix(strings.TrimSpace(row), status) {
				t.Fatalf("uptime and right-aligned toggle did not share row: %q", row)
			}
			o.Now = now.Add(time.Second)
			if plain, _ := render(); !strings.Contains(plain, "02:18:35") {
				t.Fatal("uptime did not advance locally")
			}
			if !strings.Contains(ansi.Strip(strings.Join(overviewCoreCard(o, 72).lines, "\n")), "02:18:35") {
				t.Fatal("reopening reset uptime")
			}
			o.Snapshot.Service = service.Status{Active: true, State: "active", PID: 4243, StartedAt: o.Now.Add(-time.Second)}
			if plain, _ := render(); !strings.Contains(plain, "00:00:01") || strings.Contains(plain, "02:18:") {
				t.Fatal("new process inherited old uptime")
			}
		})
	}
}

func TestCoreStatusKeepsFailuresTransitionsAndMissingTimingExplicit(t *testing.T) {
	now := time.Now()
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, tc := range []struct {
			name, state, serviceError, apiError, action, want                string
			requestedRunning, active, missingTime, futureTime, noPID, uptime bool
			toggle                                                           toggleState
		}{
			{name: "stopped", state: "inactive", want: "off", toggle: toggleOff},
			{name: "failed", state: "failed", want: "off", toggle: toggleOff},
			{name: "unexpected exit", state: "inactive", want: "off", requestedRunning: true, toggle: toggleOff},
			{name: "starting", state: "activating", want: "starting", toggle: togglePending},
			{name: "stopping", state: "deactivating", want: "stopping", toggle: togglePending},
			{name: "reloading", state: "reloading", want: "restarting", toggle: togglePending},
			{name: "unknown", state: "unknown", want: "unknown", toggle: toggleUnknown},
			{name: "empty state", want: "unknown", toggle: toggleUnknown},
			{name: "unrecognized state", state: "new-state", want: "unknown", toggle: toggleUnknown},
			{name: "service unavailable", state: "unknown", serviceError: "fixture manager unavailable", want: "unavailable", toggle: toggleUnavailable},
			{name: "monitor unavailable", state: "active", active: true, apiError: "fixture controller unavailable", want: "on", toggle: toggleOn, uptime: true},
			{name: "missing time", state: "active", active: true, missingTime: true, want: "on", toggle: toggleOn},
			{name: "future time", state: "active", active: true, futureTime: true, want: "on", toggle: toggleOn},
			{name: "missing process", state: "active", active: true, noPID: true, want: "on", toggle: toggleOn},
			{name: "local start", state: "inactive", action: "start", want: "starting", toggle: togglePending},
			{name: "local stop", state: "active", active: true, action: "stop", want: "stopping", toggle: togglePending},
			{name: "local restart", state: "active", active: true, action: "restart", want: "restarting", toggle: togglePending},
		} {
			t.Run(language+"/"+tc.name, func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), Now: now, Busy: tc.action != "", CoreAction: tc.action,
					Snapshot: app.Snapshot{Installed: true, Version: "v1.19.33", RunningVersion: "v1.19.33", Service: service.Status{Active: tc.active, State: tc.state, PID: 4242, StartedAt: now.Add(-time.Hour)}, ServiceError: tc.serviceError, APIError: tc.apiError}}
				if tc.missingTime {
					o.Snapshot.Service.StartedAt = time.Time{}
				}
				if tc.futureTime {
					o.Snapshot.Service.StartedAt = now.Add(time.Second)
				}
				if tc.noPID {
					o.Snapshot.Service.PID = 0
				}
				if tc.state == "failed" {
					o.Snapshot.Service.Result, o.Snapshot.Service.ExitCode = "start-limit-hit", 1
				}
				if tc.requestedRunning {
					o.Snapshot.Desired = app.DesiredState{Known: true, Running: true}
				}
				widget, _ := overviewCoreToggle(o)
				if widget.state != tc.toggle {
					t.Fatalf("toggle state=%v want %v", widget.state, tc.toggle)
				}
				block := overviewCoreCard(o, 72)
				plain := ansi.Strip(strings.Join(block.lines, "\n"))
				prefix := ""
				switch tc.toggle {
				case toggleOn:
					prefix = "● "
				case toggleOff:
					prefix = "○ "
				case togglePending:
					prefix = "◌ "
				}
				face := prefix + o.Locale.T("dashboard.core.toggle."+tc.want)
				if !strings.Contains(plain, face) || strings.Contains(plain, "01:00:00") != tc.uptime {
					t.Fatalf("core invented state/time (want %q):\n%s", face, plain)
				}
				if strings.Count(plain, face) != 1 {
					t.Fatalf("state repeated:\n%s", plain)
				}
				if tc.apiError != "" && !strings.Contains(plain, o.Locale.T("dashboard.core.monitorUnavailable")) {
					t.Fatal("controller failure replaced process state")
				}
				if len(block.targets) != 1 || block.targets[0].ID != "core.toggle" {
					t.Fatalf("core exposes extra controls: %+v", block.targets)
				}
			})
		}
	}
}

func TestOverviewTitlesKeepSpacingAndNavigationActionsStayInHeading(t *testing.T) {
	o := dashboardOptions{Height: 45, Snapshot: app.Snapshot{
		Installed: true, Version: "v1.19.33", Service: service.Status{Active: true, State: "active"}, ConfigReady: true, Applied: "work", Profiles: []string{"default", "work"},
		ProfileDetails: []app.ProfileInfo{{ID: "work", Name: "Work", Origin: "example.invalid", Kind: "remote", Validated: true, HasOverrides: true}},
	}}
	body := overviewLayout(o, 135)
	core, profile := body.cards[0], body.cards[1]
	if core.W != profile.W || core.H != profile.H || core.Y != profile.Y {
		t.Fatalf("paired card edges differ: %+v / %+v", core, profile)
	}
	for _, card := range body.cards {
		if strings.TrimSpace(ansi.Strip(ansi.Cut(body.lines[card.Y+2], card.X, card.X+card.W))) != "" {
			t.Fatalf("card title lost spacing: %s", card.ID)
		}
	}
	for _, hit := range body.targets {
		if hit.ID == "core.toggle" {
			row := ansi.Strip(ansi.Cut(body.lines[hit.Y], core.X, core.X+core.W))
			if !strings.HasSuffix(strings.TrimSpace(row), "● Running") {
				t.Fatalf("core toggle not right-aligned: %q", row)
			}
		}
		if hit.ID == "card.profiles" && hit.Y != profile.Y+1 {
			t.Fatalf("profile navigation not in heading: %+v", hit)
		}
	}
}

func TestCoreStatusMonitoringRecoversWhenLiveDataSupersedesAnOlderSnapshot(t *testing.T) {
	o := dashboardOptions{Snapshot: app.Snapshot{
		Installed: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
		Service:  service.Status{Active: true, State: "active"},
		APIError: "an older controller check failed",
	}, Runtime: liveRuntime(), RuntimeReceived: true}
	card := overviewCoreCard(o, 72)
	if plain := ansi.Strip(strings.Join(card.lines, "\n")); strings.Contains(plain, "Monitoring unavailable") || hasAction(card.targets, "card.logs") {
		t.Fatalf("healthy live metrics retained a stale controller diagnostic:\n%s", plain)
	}
	o.Runtime.TrafficError, o.Runtime.Traffic = "traffic stream interrupted", nil
	card = overviewCoreCard(o, 72)
	if plain := ansi.Strip(strings.Join(card.lines, "\n")); !strings.Contains(plain, "● Running") || !strings.Contains(plain, "Monitoring unavailable") || hasAction(card.targets, "card.logs") {
		t.Fatalf("independent source failure hid its warning or replaced process state:\n%s", plain)
	}
}

func TestCoreStatusHeartbeatReanchorsUptimeWithoutRequestingAFullSnapshot(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	m.syncRuntime()
	defer m.stopRuntime()
	now := time.Now()
	status := service.Status{Active: true, State: "active", PID: 101, StartTimestampMonotonic: 100,
		StartedAt: now.Add(-time.Hour)}
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 1, ObservedAt: now.Add(-time.Second),
		Snapshot: app.RuntimeSnapshot{State: "ready", Service: status}})
	m.runtime.history.points = []trafficPoint{{At: now.Add(-time.Second), Up: 10, Valid: true}}
	status.StartedAt = status.StartedAt.Add(time.Microsecond)
	if m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 1, ObservedAt: now.Add(-time.Millisecond),
		Snapshot: app.RuntimeSnapshot{State: "ready", Service: status}}) {
		t.Fatal("a clock recalibration requested another full settings/profile snapshot")
	}
	if m.snapshot.Service.StartedAt != status.StartedAt || len(m.runtime.history.points) != 1 {
		t.Fatal("heartbeat lost the latest anchor or cleared same-process history")
	}
	status.StartTimestampMonotonic = 200
	status.StartedAt = now.Add(-time.Second)
	if !m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 2, ObservedAt: now,
		Snapshot: app.RuntimeSnapshot{State: "ready", Service: status}}) {
		t.Fatal("reused PID with a different process start was not a state change")
	}
	if len(m.runtime.history.points) != 0 {
		t.Fatal("reused PID inherited the previous process's traffic history")
	}
	// A stream interruption changes monitoring health without changing the
	// current process label or hiding its valid elapsed time.
	m.runtime.snapshot.TrafficError = "stream unavailable"
	m.snapshot.Service = status
	m.snapshot.Version, m.snapshot.RunningVersion = "v1.19.33", "v1.19.33"
	o := m.navigationOptions()
	plain := ansi.Strip(strings.Join(overviewCoreCard(o, 72).lines, "\n"))
	if !strings.Contains(plain, "● Running") || !strings.Contains(plain, "Monitoring unavailable") {
		t.Fatalf("source failure replaced the process state:\n%s", plain)
	}
}
