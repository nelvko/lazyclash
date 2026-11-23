package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestCoreCardShowsElapsedUptimeWithoutSeconds(t *testing.T) {
	now := time.Now()
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			locale := i18n.New(language)
			o := dashboardOptions{Locale: locale, Now: now, Snapshot: app.Snapshot{
				Installed: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
				Service: service.Status{Active: true, State: "active", PID: 4242, StartedAt: now},
			}}
			render := func() string {
				return ansi.Strip(strings.Join(overviewCoreCard(o, 72).lines, "\n"))
			}
			for _, tc := range []struct {
				elapsed time.Duration
				want    string
			}{
				{0, locale.T("dashboard.core.uptimeUnderMinute")},
				{time.Minute - time.Nanosecond, locale.T("dashboard.core.uptimeUnderMinute")},
				{time.Minute, locale.T("dashboard.core.uptimeMinutes", 1)},
				{time.Hour - time.Nanosecond, locale.T("dashboard.core.uptimeMinutes", 59)},
				{time.Hour, locale.T("dashboard.core.uptimeHours", 1)},
				{time.Hour + time.Minute - time.Nanosecond, locale.T("dashboard.core.uptimeHours", 1)},
				{time.Hour + time.Minute, locale.T("dashboard.core.uptimeHoursMinutes", 1, 1)},
				{24*time.Hour - time.Nanosecond, locale.T("dashboard.core.uptimeHoursMinutes", 23, 59)},
				{24 * time.Hour, locale.T("dashboard.core.uptimeDays", 1)},
				{25*time.Hour - time.Nanosecond, locale.T("dashboard.core.uptimeDays", 1)},
				{25 * time.Hour, locale.T("dashboard.core.uptimeDaysHours", 1, 1)},
				{3*24*time.Hour + time.Minute, locale.T("dashboard.core.uptimeDays", 3)},
				{3*24*time.Hour + 21*time.Hour + 59*time.Minute, locale.T("dashboard.core.uptimeDaysHours", 3, 21)},
			} {
				if got := coreUptimeLabel(tc.elapsed, locale); got != tc.want {
					t.Fatalf("%v: uptime = %q, want %q", tc.elapsed, got, tc.want)
				}
			}
			o.Now = now.Add(time.Hour + 2*time.Minute + 11*time.Second)
			plain := render()
			title, uptime := "Core status", "Up for 1h 2m"
			if language == i18n.Chinese {
				title, uptime = "内核状态", "已运行 1 小时 2 分钟"
			}
			status := "● " + locale.T("dashboard.status.running")
			if !strings.Contains(plain, title) || strings.Count(plain, "Mihomo") != 1 || strings.Count(plain, status) != 1 || !strings.Contains(plain, uptime) {
				t.Fatalf("core identity, state or uptime incorrect:\n%s", plain)
			}
			if strings.Contains(plain, "v1.19.33") || strings.Contains(plain, "01:02:11") || strings.Contains(plain, "4242") {
				t.Fatalf("healthy card displayed detailed diagnostics:\n%s", plain)
			}
			o.Now = o.Now.Add(time.Second)
			if next := render(); next != plain {
				t.Fatalf("core card changed within the same minute:\n%s", next)
			}
			o.Snapshot.Service = service.Status{Active: true, State: "active", PID: 4243, StartedAt: o.Now.Add(-time.Second)}
			if next := render(); !strings.Contains(next, locale.T("dashboard.core.uptimeUnderMinute")) || strings.Contains(next, uptime) {
				t.Fatalf("new process inherited the previous process's uptime:\n%s", next)
			}
		})
	}
}

func TestCoreCardShowsUptimeOnlyWhenSidebarClockIsHidden(t *testing.T) {
	now := time.Now()
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, tc := range []struct {
			width, height           int
			hideHints, clockVisible bool
		}{
			{160, 45, false, true},
			{sidebarMinWidth, 24, false, true},
			{sidebarMinWidth - 1, 24, false, false},
			{80, 24, false, false},
			{120, 11, false, true},
			{120, 10, false, false},
			{120, 10, true, true},
		} {
			t.Run(fmt.Sprintf("%s/%dx%d/hideHints=%t", language, tc.width, tc.height, tc.hideHints), func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), Width: tc.width, Height: tc.height, Page: "overview", HideKeyHints: tc.hideHints, Now: now,
					Snapshot: app.Snapshot{Installed: true, Service: service.Status{State: "active", Active: true, PID: 42, StartedAt: now.Add(-time.Hour - 2*time.Minute - 11*time.Second)}}}
				screen, _, _ := renderDashboard(o)
				plain := ansi.Strip(screen)
				if strings.Contains(plain, "01:02:11") != tc.clockVisible {
					t.Fatalf("unexpected sidebar clock visibility:\n%s", plain)
				}
				minutes := o.Locale.T("dashboard.core.uptimeHoursMinutes", 1, 2)
				if strings.Contains(plain, minutes) == tc.clockVisible {
					t.Fatalf("overview must show %q exactly when the sidebar clock is absent:\n%s", minutes, plain)
				}
			})
		}
	}
}

func TestCoreStatusKeepsFailuresTransitionsAndMissingTimingExplicit(t *testing.T) {
	now := time.Now()
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, tc := range []struct {
			name, state, serviceError, apiError, action                      string
			requestedRunning, active, missingTime, futureTime, noPID, uptime bool
			unsupported, stateUnavailable                                    bool
			toggle                                                           toggleState
		}{
			{name: "stopped", state: "inactive", toggle: toggleOff},
			{name: "failed", state: "failed", toggle: toggleOff},
			{name: "unexpected exit", state: "inactive", requestedRunning: true, toggle: toggleOff},
			{name: "starting", state: "activating", toggle: togglePending},
			{name: "stopping", state: "deactivating", toggle: togglePending},
			{name: "reloading", state: "reloading", toggle: togglePending},
			{name: "unknown", state: "unknown", toggle: toggleUnknown},
			{name: "empty state", toggle: toggleUnknown},
			{name: "unrecognized state", state: "new-state", toggle: toggleUnknown},
			{name: "service unavailable", state: "unknown", serviceError: "fixture manager unavailable", toggle: toggleUnavailable},
			{name: "monitor unavailable", state: "active", active: true, apiError: "fixture controller unavailable", toggle: toggleOn, uptime: true},
			{name: "read-only running process", state: "active", active: true, unsupported: true, toggle: toggleUnavailable, uptime: true},
			{name: "stale running snapshot", state: "active", active: true, stateUnavailable: true, toggle: toggleUnavailable},
			{name: "missing time", state: "active", active: true, missingTime: true, toggle: toggleOn},
			{name: "future time", state: "active", active: true, futureTime: true, toggle: toggleOn},
			{name: "missing process", state: "active", active: true, noPID: true, toggle: toggleOn},
			{name: "local start", state: "inactive", action: "start", toggle: togglePending},
			{name: "local stop", state: "active", active: true, action: "stop", toggle: togglePending},
			{name: "local restart", state: "active", active: true, action: "restart", toggle: togglePending},
		} {
			t.Run(language+"/"+tc.name, func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), Now: now, Busy: tc.action != "", CoreAction: tc.action, StateUnavailable: tc.stateUnavailable,
					Snapshot: app.Snapshot{Installed: true, Version: "v1.19.33", RunningVersion: "v1.19.33", Service: service.Status{Active: tc.active, State: tc.state, PID: 4242, StartedAt: now.Add(-time.Hour)}, ServiceError: tc.serviceError, APIError: tc.apiError}}
				if tc.unsupported {
					o.Snapshot.Capabilities = app.Capabilities{Scope: "user", Runtime: app.Capability{Reason: "fixture read only"}}
				}
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
				widget := overviewCoreToggle(o)
				if widget.state != tc.toggle {
					t.Fatalf("toggle state=%v want %v", widget.state, tc.toggle)
				}
				block := overviewCoreCard(o, 72)
				plain := ansi.Strip(strings.Join(block.lines, "\n"))
				face, _, _ := headerCoreState(o)
				action, _ := coreCardAction(t, block)
				if identity, state := strings.TrimSpace(ansi.Strip(block.lines[action.Y+2])), strings.TrimSpace(ansi.Strip(block.lines[action.Y+3])); identity != "Mihomo" || state != face {
					t.Fatalf("%s: expected identity followed by the current state, got %q and %q", tc.name, identity, state)
				}
				prefix := "Up for "
				if language == i18n.Chinese {
					prefix = "已运行"
				}
				if strings.Contains(plain, prefix) != tc.uptime || (tc.uptime && !strings.Contains(plain, o.Locale.T("dashboard.core.uptimeHours", 1))) {
					t.Fatalf("core reported incorrect uptime for %s:\n%s", tc.name, plain)
				}
				if strings.Contains(plain, o.Snapshot.Version) {
					t.Fatalf("matching installed and running versions repeated in overview:\n%s", plain)
				}
				if strings.Count(plain, face) != 1 {
					t.Fatalf("expected one %q state label:\n%s", face, plain)
				}
				if tc.state == "reloading" && face != "◌ "+o.Locale.T("dashboard.status.reloading") {
					t.Fatalf("reload labeled as another operation: %q", face)
				}
				if tc.state == "failed" && !strings.Contains(plain, o.Locale.T("dashboard.core.failureReason", "start-limit-hit", 1)) {
					t.Fatal("failed service lost its exit reason")
				}
				if tc.requestedRunning && !strings.Contains(plain, o.Locale.T("dashboard.core.exitHint")) {
					t.Fatal("unexpected exit lost its recovery hint")
				}
				if tc.apiError != "" && !strings.Contains(plain, o.Locale.T("dashboard.core.monitorUnavailable")) {
					t.Fatal("controller failure replaced process state")
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
			if hit.Y != core.Y+1 || hit.X+hit.W != core.X+core.W-2 {
				t.Fatalf("core action not at heading's right edge: %+v / %+v", hit, core)
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
