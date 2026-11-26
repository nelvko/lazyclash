package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func sidebarRowContaining(t *testing.T, screen, value string) int {
	t.Helper()
	for y, row := range strings.Split(screen, "\n") {
		if strings.Contains(ansi.Strip(ansi.Cut(row, 0, sidebarWidth)), value) {
			return y
		}
	}
	t.Fatalf("sidebar does not contain %q:\n%s", value, sidebarText(screen))
	return -1
}

func TestSidebarCoreUptimeCentersAboveStatus(t *testing.T) {
	now := time.Now()
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, plain := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/plain=%t", language, plain), func(t *testing.T) {
				m := headerModel("logs", 160, 45)
				m.locale, m.noColor = i18n.New(language), plain
				m.snapshot.Service.PID = 42
				m.snapshot.Service.StartedAt = now.Add(-101*time.Hour - 2*time.Minute - 3*time.Second)
				m.runtime.snapshot.Traffic = &mihomo.Traffic{Up: 2048, Down: 4096}
				m.layout()
				if head := headerText(m); strings.Contains(head, "●") || !strings.Contains(head, m.tr("header.paused")) {
					t.Fatalf("log header lost its own state or repeated global status: %q", head)
				}

				o := m.navigationOptions()
				o.Now = now
				status := "● " + m.tr("dashboard.status.running")
				baseline := -1
				for _, page := range navPages() {
					o.Page = page
					screen, targets, _ := renderDashboard(o)
					y := sidebarRowContaining(t, screen, status)
					if baseline == -1 {
						baseline = y
					}
					if y != baseline || y == 0 {
						t.Fatalf("%s: status moved from row %d to %d", page, baseline, y)
					}
					rows := strings.Split(screen, "\n")
					for index, want := range []string{"101:02:03", status} {
						line := ansi.Strip(ansi.Cut(rows[y-1+index], 0, sidebarWidth))
						left := len(line) - len(strings.TrimLeft(line, " "))
						right := len(line) - len(strings.TrimRight(line, " "))
						if strings.TrimSpace(line) != want || left-right > 1 || right-left > 1 {
							t.Fatalf("%s: expected centered %q, got %q", page, want, line)
						}
					}
					g := dashboardGeometry(o)
					if strings.Contains(ansi.Strip(rows[g.headerY]), "101:02:03") {
						t.Fatalf("%s: header repeats process uptime", page)
					}
					visible := visibleSidebarTargets(targets)
					if len(visible) != len(navPages()) {
						t.Fatalf("%s: tall sidebar lost a destination", page)
					}
					last := visible[len(visible)-1]
					for row := last.Y + last.H; row < dashboardContentBottom(o); row++ {
						if row == y-1 || row == y {
							continue
						}
						content := ansi.Strip(ansi.Cut(rows[row], 0, sidebarWidth))
						if strings.TrimSpace(content) != "" {
							t.Fatalf("%s: footer contains extra data: %q", page, content)
						}
					}
					for _, hit := range targets {
						if regionsOverlap(hit, hitRegion{X: 0, Y: y - 1, W: sidebarWidth, H: 2}) {
							t.Fatalf("read-only sidebar footer acquired an interaction target: %+v", hit)
						}
					}
				}
				o.Now = now.Add(time.Second)
				screen, _, _ := renderDashboard(o)
				if sidebarRowContaining(t, screen, "101:02:04") != baseline-1 {
					t.Fatal("process uptime did not advance on the existing clock row")
				}
			})
		}
	}
}

func TestSidebarCoreUptimeRequiresConfirmedRunningProcess(t *testing.T) {
	now := time.Now()
	clock := "01:02:03"
	for _, tc := range []struct {
		name, state, action                                             string
		inactive, noPID, missingStart, futureStart, stale, serviceError bool
	}{
		{name: "stopped", state: "inactive", inactive: true},
		{name: "unrecognized", state: "unknown"},
		{name: "failed", state: "failed", inactive: true},
		{name: "missing process", state: "active", noPID: true},
		{name: "missing start time", state: "active", missingStart: true},
		{name: "future start time", state: "active", futureStart: true},
		{name: "stale snapshot", state: "active", stale: true},
		{name: "service read failed", state: "active", serviceError: true},
		{name: "pending restart", state: "active", action: "restart"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := dashboardOptions{Width: 120, Height: 35, Page: "profiles", Now: now,
				Snapshot: app.Snapshot{Installed: true, Service: service.Status{State: "active", Active: true, PID: 42, StartedAt: now.Add(-time.Hour - 2*time.Minute - 3*time.Second)}}}
			screen, _, _ := renderDashboard(o)
			y := sidebarRowContaining(t, screen, "● Running")
			if sidebarRowContaining(t, screen, clock) != y-1 {
				t.Fatal("running process missing its uptime")
			}
			o.Snapshot.Service.State, o.Snapshot.Service.Active = tc.state, !tc.inactive
			o.StateUnavailable, o.CoreAction = tc.stale, tc.action
			if tc.noPID {
				o.Snapshot.Service.PID = 0
			}
			if tc.missingStart {
				o.Snapshot.Service.StartedAt = time.Time{}
			}
			if tc.futureStart {
				o.Snapshot.Service.StartedAt = now.Add(time.Minute)
			}
			if tc.serviceError {
				o.Snapshot.ServiceError = "fixture observation unavailable"
			}
			screen, _, _ = renderDashboard(o)
			status, _, _ := headerCoreState(o)
			if sidebarRowContaining(t, screen, status) != y || strings.Contains(sidebarText(screen), ":") {
				t.Fatalf("unconfirmed clock or shifted status in sidebar:\n%s", sidebarText(screen))
			}
		})
	}
}

func TestSidebarCoreStatusKeepsCompleteStateLabelsAtAFixedRow(t *testing.T) {
	for _, tc := range []struct {
		name, state, action, symbol, label string
		installed, active, unavailable     bool
		err                                string
	}{
		{name: "running", state: "active", installed: true, active: true, symbol: "●", label: "Running"},
		{name: "stopped", state: "inactive", installed: true, symbol: "○", label: "Stopped"},
		{name: "starting", state: "activating", installed: true, symbol: "◌", label: "Starting"},
		{name: "stopping", state: "active", installed: true, active: true, action: "stop", symbol: "◌", label: "Stopping"},
		{name: "failed", state: "failed", installed: true, symbol: "!", label: "Failed"},
		{name: "unknown", state: "unknown", installed: true, symbol: "?", label: "Unknown"},
		{name: "unavailable", state: "unknown", installed: true, err: "fixture unavailable", symbol: "!", label: "Unknown"},
		{name: "unsupported", state: "unsupported", installed: true, symbol: "!", label: "Unavailable"},
		{name: "not installed", symbol: "−", label: "Not installed"},
		{name: "initial observation", unavailable: true, symbol: "?", label: "Unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := dashboardOptions{Width: 120, Height: 32, Page: "profiles", Locale: i18n.New(i18n.English),
				Snapshot:   app.Snapshot{Installed: tc.installed, ServiceError: tc.err, Service: service.Status{State: tc.state, Active: tc.active}},
				CoreAction: tc.action, StateUnavailable: tc.unavailable}
			screen, targets, _ := renderDashboard(o)
			y := sidebarRowContaining(t, screen, tc.label)
			row := strings.TrimSpace(ansi.Strip(ansi.Cut(strings.Split(screen, "\n")[y], 0, sidebarWidth)))
			if row != tc.symbol+" "+tc.label || y != dashboardContentBottom(o)-2 {
				t.Fatalf("state is ambiguous or moved the footer: row=%d %q", y, row)
			}
			assertCanvas(t, screen, o.Width, o.Height)
			assertRegions(t, targets, o.Width, o.Height)
		})
	}
}

func TestSidebarCoreUptimePreservesShortNavigation(t *testing.T) {
	now := time.Now()
	for _, height := range []int{9, 10, 11, 24, 26, 45} {
		for _, hideHints := range []bool{false, true} {
			t.Run(fmt.Sprintf("height=%d/hideHints=%t", height, hideHints), func(t *testing.T) {
				o := dashboardOptions{Width: 120, Height: height, Page: "profiles", HideKeyHints: hideHints, Now: now,
					Snapshot: app.Snapshot{Installed: true, Service: service.Status{State: "active", Active: true, PID: 42}}}
				withoutTime, targets, _ := renderDashboard(o)
				before := visibleSidebarTargets(targets)
				baseline := sidebarRowContaining(t, withoutTime, "● Running")
				o.Snapshot.Service.StartedAt = now.Add(-time.Hour)
				screen, targets, _ := renderDashboard(o)
				y := sidebarRowContaining(t, screen, "● Running")
				if y != baseline {
					t.Fatalf("uptime moved the status from %d to %d", baseline, y)
				}
				visible := visibleSidebarTargets(targets)
				if !slices.Equal(before, visible) || len(visible) == 0 {
					t.Fatalf("uptime displaced navigation: before=%+v after=%+v", before, visible)
				}
				rows := strings.Split(screen, "\n")
				for _, hit := range visible {
					row := ansi.Strip(ansi.Cut(rows[hit.Y+hit.H/2], hit.X, hit.X+hit.W))
					if !strings.Contains(row, pageTitle(strings.TrimPrefix(hit.ID, "nav."))) {
						t.Fatalf("footer overwrote navigation: %q at %+v", row, hit)
					}
				}
				hasClock := strings.Contains(sidebarText(screen), "01:00:00")
				if !hideHints {
					switch height {
					case 9:
						if hasClock || !strings.Contains(sidebarText(screen), "↓") {
							t.Fatal("nine rows must preserve the menu scroll hint and core state")
						}
					case 10:
						if hasClock || len(visible) != len(navPages()) {
							t.Fatal("ten rows should preserve all destinations plus status")
						}
					case 11, 26:
						if !hasClock || len(visible) != len(navPages()) || visible[0].H != 1 {
							t.Fatal("compact navigation should fit the clock and all destinations")
						}
					}
				}
				if hasClock && visible[len(visible)-1].Y+visible[len(visible)-1].H > y-1 {
					t.Fatal("clock overlaps the final visible menu item")
				}
				assertCanvas(t, screen, o.Width, o.Height)
				assertRegions(t, targets, o.Width, o.Height)
			})
		}
	}
}

func TestPopupNavigationDoesNotDuplicateCompactHeaderCoreStatus(t *testing.T) {
	now := time.Now()
	for _, height := range []int{9, 24, 45} {
		o := dashboardOptions{Width: 80, Height: height, Page: "profiles", SidebarOpen: true, Now: now,
			Snapshot: app.Snapshot{Installed: true, Service: service.Status{State: "active", Active: true, PID: 42, StartedAt: now.Add(-time.Hour)}}}
		screen, targets, _ := renderDashboard(o)
		if count := strings.Count(ansi.Strip(screen), "● Running"); count != 1 {
			t.Fatalf("80x%d rendered %d compact header badges", height, count)
		}
		if strings.Contains(ansi.Strip(screen), "01:00:00") {
			t.Fatal("compact header or popup showed fixed-sidebar uptime")
		}
		assertCanvas(t, screen, o.Width, o.Height)
		assertRegions(t, targets, o.Width, o.Height)
	}
}

func TestCoreStatusMovesAcrossSidebarBreakpointWithoutResettingBody(t *testing.T) {
	m := headerModel("rules", 120, 24)
	m.rules.active = true
	m.rules.source = m.currentRulesSource()
	m.rules.snapshot.Rules = nil
	for index := range 60 {
		m.rules.snapshot.Rules = append(m.rules.snapshot.Rules, mihomo.Rule{Index: index, Type: "Domain", Payload: fmt.Sprintf("r%d.invalid", index), Proxy: "DIRECT", Size: -1})
	}
	m.rules.selectedIndex = 20
	m.rules.selected = ruleIdentity(m.rules.snapshot.Rules[20])
	m.focus, m.scroll = "rule.20", 15
	m.layout()
	before := m.scroll
	if before == 0 || !hasAction(m.targets, m.focus) {
		t.Fatal("fixture did not start in a scrolled body with a visible focus")
	}
	for _, width := range []int{114, 115, 160, 114, 120} {
		m, _ = update(m, tea.WindowSizeMsg{Width: width, Height: 24})
		if m.focus != "rule.20" || m.scroll != before || m.sidebarOpen {
			t.Fatalf("width %d moved body state: focus=%s scroll=%d open=%v", width, m.focus, m.scroll, m.sidebarOpen)
		}
		if sidebarVisible(width, m.height) {
			sidebarRowContaining(t, m.screen, "● Running")
			if strings.Contains(headerText(m), "● Running") {
				t.Fatal("expanding sidebar retained the title badge")
			}
		} else if !strings.Contains(headerText(m), "● Running") {
			t.Fatal("folding sidebar hid the global core state")
		}
		assertCanvas(t, m.screen, width, 24)
		assertRegions(t, m.targets, width, 24)
	}
}
