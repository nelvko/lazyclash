package tui

import (
	"fmt"
	"strings"
	"testing"

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

func TestSidebarCoreStatusSitsAboveTrafficAndKeepsLogStatus(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := headerModel("logs", 160, 45)
			m.locale = i18n.New(language)
			m.runtime.snapshot.Traffic = &mihomo.Traffic{Up: 2048, Down: 4096}
			m.layout()
			status := "● " + m.tr("dashboard.sidebar.coreStatus", m.tr("dashboard.status.running"))
			y := sidebarRowContaining(t, m.screen, status)
			if up, down := sidebarRowContaining(t, m.screen, "2.0 KiB/s"), sidebarRowContaining(t, m.screen, "4.0 KiB/s"); up != y+1 || down != y+2 {
				t.Fatalf("core status and traffic do not share one footer: status=%d up=%d down=%d", y, up, down)
			}
			if head := headerText(m); strings.Contains(head, "●") || !strings.Contains(head, m.tr("header.paused")) {
				t.Fatalf("log header lost its own state or repeated global status: %q", head)
			}
			for _, targets := range [][]hitRegion{m.targets, m.focusTargets()} {
				for _, hit := range targets {
					if regionsOverlap(hit, hitRegion{X: 0, Y: y, W: sidebarWidth, H: 1}) {
						t.Fatalf("read-only core status acquired an interaction target: %+v", hit)
					}
				}
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
			if !strings.HasPrefix(row, tc.symbol+" ") || y != dashboardContentBottom(o)-3 {
				t.Fatalf("state is ambiguous or moved the footer: row=%d %q", y, row)
			}
			assertCanvas(t, screen, o.Width, o.Height)
			assertRegions(t, targets, o.Width, o.Height)
		})
	}
}

func TestSidebarCoreStatusSurvivesWhenTrafficDoesNotFit(t *testing.T) {
	for _, hideHints := range []bool{false, true} {
		for _, height := range []int{9, 10, 26, 45} {
			t.Run(fmt.Sprintf("height=%d/hideHints=%v", height, hideHints), func(t *testing.T) {
				o := dashboardOptions{Width: 120, Height: height, Page: "profiles", HideKeyHints: hideHints,
					Snapshot: app.Snapshot{Installed: true, Service: service.Status{State: "active", Active: true}},
					Runtime:  liveRuntime(), RuntimeReceived: true}
				screen, targets, _ := renderDashboard(o)
				y := sidebarRowContaining(t, screen, "Core: Running")
				if height <= 10 && strings.Contains(sidebarText(screen), "KiB/s") {
					t.Fatal("short sidebar kept rates instead of reserving the core state")
				}
				if height == 45 && !strings.Contains(sidebarText(screen), "KiB/s") {
					t.Fatal("tall sidebar lost the rates")
				}
				for _, hit := range visibleSidebarTargets(targets) {
					if hit.Y+hit.H > y {
						t.Fatalf("core state overwrote navigation: %+v, state row=%d", hit, y)
					}
				}
				assertCanvas(t, screen, o.Width, o.Height)
				assertRegions(t, targets, o.Width, o.Height)
			})
		}
	}
}

func TestPopupNavigationDoesNotDuplicateCompactHeaderCoreStatus(t *testing.T) {
	for _, height := range []int{9, 24, 45} {
		o := dashboardOptions{Width: 80, Height: height, Page: "profiles", SidebarOpen: true,
			Snapshot: app.Snapshot{Installed: true, Service: service.Status{State: "active", Active: true}}}
		screen, targets, _ := renderDashboard(o)
		if count := strings.Count(ansi.Strip(screen), "● Running"); count != 1 {
			t.Fatalf("80x%d rendered %d compact header badges", height, count)
		}
		if strings.Contains(ansi.Strip(screen), "Core: Running") {
			t.Fatal("popup navigation duplicated the fixed core status")
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
			sidebarRowContaining(t, m.screen, "Core: Running")
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
