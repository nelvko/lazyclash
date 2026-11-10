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
			for _, want := range []string{o.Locale.T("dashboard.overview.service"), "● " + o.Locale.T("dashboard.status.running"), "v1.19.33", o.Locale.T("dashboard.core.uptime", "02:18:34")} {
				if !strings.Contains(plain, want) {
					t.Fatalf("core status omitted %q:\n%s", want, plain)
				}
			}
			if hasAction(block.targets, "card.install") || strings.Contains(plain, "4242") || strings.Count(plain, "v1.19.33") != 1 {
				t.Fatalf("normal core status retained duplicate management/diagnostics:\n%s", plain)
			}
			if heading := ansi.Strip(block.lines[1]); !strings.Contains(heading, o.Locale.T("dashboard.overview.service")) || !strings.HasSuffix(strings.TrimSpace(heading), "v1.19.33") {
				t.Fatalf("version did not stay at the right of the card heading: %q", heading)
			}
			for _, line := range block.lines[2:] {
				if strings.Contains(ansi.Strip(line), "v1.19.33") {
					t.Fatal("normal version remained in the status/uptime body")
				}
			}
			for _, id := range []string{"core.stop", "core.restart"} {
				if !hasAction(block.targets, id) {
					t.Fatalf("normal core lost %s", id)
				}
			}
			o.Now = now.Add(time.Second)
			if plain, _ := render(); !strings.Contains(plain, "02:18:35") {
				t.Fatal("uptime did not advance locally")
			}
			// Opening another TUI uses the same process anchor, not its own start.
			reopened := overviewCoreCard(o, 72)
			if !strings.Contains(ansi.Strip(strings.Join(reopened.lines, "\n")), "02:18:35") {
				t.Fatal("reopening the TUI reset the process uptime")
			}
			o.Snapshot.Service = service.Status{Active: true, State: "active", PID: 4243, StartedAt: o.Now.Add(-time.Second)}
			if plain, _ := render(); !strings.Contains(plain, "00:00:01") || strings.Contains(plain, "02:18:") {
				t.Fatal("new process inherited the old uptime")
			}
		})
	}
}

func TestCoreStatusKeepsFailuresTransitionsAndMissingTimingExplicit(t *testing.T) {
	now := time.Now()
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, tc := range []struct {
			name, state, serviceError, apiError, action, want string
			requestedRunning                                  bool
			active, missingTime, futureTime, noPID            bool
			start, stop, restart, logs, uptime                bool
		}{
			{name: "stopped", state: "inactive", want: "dashboard.status.stopped", start: true, restart: true},
			{name: "failed", state: "failed", want: "dashboard.status.failed", start: true, restart: true, logs: true},
			{name: "unexpected exit", state: "inactive", want: "dashboard.core.unexpectedExit", requestedRunning: true, start: true, restart: true, logs: true},
			{name: "starting", state: "activating", want: "dashboard.status.starting"},
			{name: "stopping", state: "deactivating", want: "dashboard.status.stopping"},
			{name: "reloading", state: "reloading", want: "dashboard.status.reloading"},
			{name: "unknown", state: "unknown", want: "dashboard.status.unknown", stop: true},
			{name: "empty state", want: "dashboard.status.unknown", stop: true},
			{name: "unrecognized state", state: "new-state", want: "dashboard.status.unknown", stop: true},
			{name: "service unavailable", state: "unknown", serviceError: "fixture manager unavailable", want: "dashboard.status.unknown", stop: true},
			{name: "monitor unavailable", state: "active", active: true, apiError: "fixture controller unavailable", want: "dashboard.status.running", stop: true, restart: true, logs: true, uptime: true},
			{name: "missing time", state: "active", active: true, missingTime: true, want: "dashboard.status.running", stop: true, restart: true},
			{name: "future time", state: "active", active: true, futureTime: true, want: "dashboard.status.running", stop: true, restart: true},
			{name: "missing process", state: "active", active: true, noPID: true, want: "dashboard.status.running", stop: true, restart: true},
			{name: "local start", state: "inactive", action: "start", want: "dashboard.status.starting"},
			{name: "local stop", state: "active", active: true, action: "stop", want: "dashboard.status.stopping"},
			{name: "local restart", state: "active", active: true, action: "restart", want: "dashboard.core.restarting"},
		} {
			t.Run(language+"/"+tc.name, func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), Now: now, Busy: tc.action != "", CoreAction: tc.action,
					Snapshot: app.Snapshot{Installed: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
						Service:      service.Status{Active: tc.active, State: tc.state, PID: 4242, StartedAt: now.Add(-time.Hour)},
						ServiceError: tc.serviceError, APIError: tc.apiError}}
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
				block := overviewCoreCard(o, 72)
				plain := ansi.Strip(strings.Join(block.lines, "\n"))
				if !strings.Contains(plain, "● "+o.Locale.T(tc.want)) || strings.Contains(plain, "01:00:00") != tc.uptime {
					t.Fatalf("core invented state or elapsed time:\n%s", plain)
				}
				if tc.apiError != "" && !strings.Contains(plain, o.Locale.T("dashboard.core.monitorUnavailable")) {
					t.Fatal("controller failure replaced process state")
				}
				for id, enabled := range map[string]bool{"core.start": tc.start, "core.stop": tc.stop, "core.restart": tc.restart, "card.logs": tc.logs} {
					if hasAction(block.targets, id) != enabled {
						t.Fatalf("%s action state = %v, want %v:\n%s", id, hasAction(block.targets, id), enabled, plain)
					}
				}
			})
		}
	}
}

func TestOverviewTitlesKeepSpacingAndNavigationActionsStayInHeading(t *testing.T) {
	o := dashboardOptions{Height: 45, Snapshot: app.Snapshot{
		Installed: true, Version: "v1.19.33", Service: service.Status{Active: true, State: "active"},
		ConfigReady: true, Applied: "work", Profiles: []string{"default", "work"},
		ProfileDetails: []app.ProfileInfo{{ID: "work", Name: "Work", Origin: "example.invalid", Kind: "remote", Validated: true, HasOverrides: true}},
	}}
	body := overviewLayout(o, 135)
	core, profile := body.cards[0], body.cards[1]
	if core.W != profile.W || core.H != profile.H || core.Y != profile.Y {
		t.Fatalf("paired card edges differ: %+v / %+v", core, profile)
	}
	for _, card := range body.cards {
		if strings.TrimSpace(ansi.Strip(ansi.Cut(body.lines[card.Y+2], card.X, card.X+card.W))) != "" {
			t.Fatalf("card title is not separated from its body: %s", card.ID)
		}
	}
	for _, hit := range body.targets {
		if hit.ID == "core.stop" {
			if hit.Y != core.Y+core.H-2 {
				t.Fatalf("core action did not stay above the bottom padding: %+v / %+v", hit, core)
			}
		}
		if hit.ID == "card.profiles" && hit.Y != profile.Y+1 {
			t.Fatalf("profile navigation did not stay in its heading: %+v / %+v", hit, profile)
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
	if plain := ansi.Strip(strings.Join(card.lines, "\n")); !strings.Contains(plain, "● Running") || !strings.Contains(plain, "Monitoring unavailable") || !hasAction(card.targets, "card.logs") {
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
