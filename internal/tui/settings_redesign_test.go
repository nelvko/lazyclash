package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func settingsTestSnapshot() app.Snapshot {
	return app.Snapshot{
		Installed: true, ConfigReady: true, HasRun: true,
		Version: "v1.19.33", RunningVersion: "v1.19.32", Controller: "127.0.0.1:9097",
		Service:      service.Status{State: "inactive", Linger: true, LingerKnown: true},
		Mode:         app.ModeStatus{Desired: "rule", Actual: "global"},
		Capabilities: app.Capabilities{Scope: "system", Runtime: app.Capability{Supported: true}, TUN: app.Capability{Supported: true}},
	}
}

func settingsPaintedTarget(t *testing.T, body string, hits []hitRegion, id string) string {
	t.Helper()
	for _, hit := range hits {
		if hit.ID == id {
			return ansi.Strip(ansi.Cut(strings.Split(body, "\n")[hit.Y], hit.X, hit.X+hit.W))
		}
	}
	t.Fatalf("setting %q has no target", id)
	return ""
}

func TestSettingsFillWindowWidthAcrossSidebarAndColumnBreakpoints(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range []struct{ width, height, bodyWidth, columns int }{
			{240, 45, 215, 2}, {200, 40, 175, 2}, {160, 45, 135, 2}, {120, 32, 95, 2},
			{105, 32, 80, 2}, {104, 32, 79, 1}, {100, 28, 75, 1}, {99, 28, 95, 2},
			{84, 24, 80, 2}, {83, 24, 79, 1}, {80, 24, 76, 1}, {40, 16, 36, 1},
			{240, 19, 236, 1},
		} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, size.width, size.height), func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), Page: "settings", Width: size.width, Height: size.height, Snapshot: settingsTestSnapshot()}
				o.Header = (Model{page: "settings", locale: o.Locale}).pageHeader()
				g := dashboardGeometry(o)
				if g.width != size.bodyWidth {
					t.Fatalf("settings body width=%d, want available width %d", g.width, size.bodyWidth)
				}
				screen, hits, _ := renderDashboard(o)
				assertCanvas(t, screen, size.width, size.height)
				assertRegions(t, hits, size.width, size.height)
				top := ansi.Strip(strings.Split(screen, "\n")[g.y])
				if strings.Count(top, "┌") != size.columns || ansi.Cut(top, g.x+size.bodyWidth-1, g.x+size.bodyWidth) != "┐" {
					t.Fatalf("settings cards do not fill the available width with %d columns: %q", size.columns, top)
				}
			})
		}
	}
}

func TestSettingsGroupsAdaptToBodyWidthAndShortTerminals(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{160, 45}, {80, 24}, {79, 24}, {60, 20}, {120, 18}, {20, 7}, {3, 2}, {1, 1}} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, size[0], size[1]), func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), Height: size[1], Snapshot: settingsTestSnapshot()}
				body, hits := settingsContent(o, size[0])
				rows := strings.Split(body, "\n")
				assertCanvas(t, body, size[0], len(rows))
				assertRegions(t, hits, size[0], len(rows))
				if size[0] < 40 {
					return
				}
				position := func(label string) (int, int) {
					for y, row := range rows {
						plain := ansi.Strip(row)
						if x := strings.Index(plain, label); x >= 0 {
							return ansi.StringWidth(plain[:x]), y
						}
					}
					t.Fatalf("missing section %q", label)
					return 0, 0
				}
				ix, iy := position(o.Locale.T("settings.interface"))
				sx, sy := position(o.Locale.T("settings.serviceGroup"))
				nx, ny := position(o.Locale.T("settings.network"))
				if size[0] >= 80 && size[1] >= 20 {
					if sx <= ix || sy != iy || nx != ix || ny <= iy {
						t.Fatalf("wide settings are not grouped in two columns: interface=%d,%d service=%d,%d network=%d,%d", ix, iy, sx, sy, nx, ny)
					}
				} else if sx != ix || sy <= iy || nx != ix || ny <= sy {
					t.Fatalf("compact settings did not stack in reading order: interface=%d,%d service=%d,%d network=%d,%d", ix, iy, sx, sy, nx, ny)
				}
			})
		}
	}
}

func TestSettingsActionsKeepCompleteLabelsAndUniqueTargets(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{40, 60, 80, 120, 160} {
			for _, active := range []bool{false, true} {
				o := dashboardOptions{Locale: i18n.New(language), Height: 32, Snapshot: settingsTestSnapshot()}
				o.Snapshot.Service.Active = active
				o.Snapshot.Service.Enabled = active
				o.Snapshot.TUN = active
				tr := o.Locale.T
				labels := map[string]string{
					"language.en": "English", "language.zh-Hans": "简体中文", "key-hints.toggle": tr("dashboard.control.on"),
					"install": tr("settings.manage"), "import": tr("settings.import"), "network": tr("settings.edit"), "runtime-settings": tr("settings.edit"),
					"restart":   tr("dashboard.action.restart"),
					"mode.rule": tr("dashboard.mode.rule"), "mode.global": tr("dashboard.mode.global"), "mode.direct": tr("dashboard.mode.direct"),
				}
				if active {
					labels["stop"], labels["autostart-disable"], labels["tun.disable"] = tr("dashboard.settings.stop"), tr("dashboard.control.on"), tr("dashboard.control.on")
				} else {
					labels["start"], labels["autostart-enable"], labels["tun.enable"] = tr("dashboard.settings.start"), tr("dashboard.control.off"), tr("dashboard.control.off")
				}
				body, hits := settingsContent(o, width)
				seen := map[string]bool{}
				for _, hit := range hits {
					if seen[hit.ID] {
						t.Fatalf("%s/%d: duplicate target %s", language, width, hit.ID)
					}
					seen[hit.ID] = true
					label, ok := labels[hit.ID]
					if !ok {
						t.Fatalf("unexpected action %q", hit.ID)
					}
					painted := settingsPaintedTarget(t, body, hits, hit.ID)
					if !strings.Contains(painted, label) || ansi.StringWidth(painted) != hit.W {
						t.Fatalf("%s/%d: %s target misses complete label %q: %q", language, width, hit.ID, label, painted)
					}
				}
				if len(seen) != len(labels) {
					t.Fatalf("%s/%d: got actions %v; want %v", language, width, seen, labels)
				}
				o.Busy = true
				_, hits = settingsContent(o, width)
				if len(hits) != 1 || hits[0].ID != "key-hints.toggle" {
					t.Fatalf("busy settings expose write actions: %v", hits)
				}
			}
		}
	}
}

func TestSettingsUninstalledActionsAndHeaderOwnership(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English), Height: 32}
	body, hits := settingsContent(o, 120)
	for _, id := range []string{"install", "import", "network", "language.en", "language.zh-Hans", "key-hints.toggle"} {
		if !hasAction(hits, id) {
			t.Fatalf("uninstalled settings lost %s", id)
		}
	}
	if len(hits) != 6 || !strings.Contains(ansi.Strip(body), o.Locale.T("settings.afterInstall")) {
		t.Fatal("uninstalled settings expose installed-core actions or omit their availability")
	}
	o.Header = pageHeader{actions: []headerAction{{id: "network", enabled: true}}}
	_, hits = settingsContent(o, 120)
	if hasAction(hits, "network") || len(hits) != 5 {
		t.Fatal("download settings are duplicated in the header and body")
	}
}

func TestSettingsPreservesSavedStateScopeAndDiagnostics(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		o := dashboardOptions{Locale: i18n.New(language), Height: 32, Snapshot: settingsTestSnapshot(), PlainButtons: true}
		o.Snapshot.Service.Result, o.Snapshot.Service.ExitCode = "exit-code", 17
		o.Snapshot.ServiceError, o.Snapshot.Drift = "service fixture diagnostic", "saved state differs from service"
		body, hits := settingsContent(o, 120)
		plain := ansi.Strip(body)
		for _, want := range []string{
			o.Snapshot.ServiceError, o.Snapshot.Drift, "exit-code", "17", o.Snapshot.Controller,
			o.Locale.T("dashboard.settings.actualMode", dashboardModeLabel("global", o.Locale)),
			o.Locale.T("dashboard.settings.running", o.Snapshot.RunningVersion), o.Locale.T("tun.host_scope"),
		} {
			if !strings.Contains(plain, want) {
				t.Fatalf("%s: missing setting diagnostic %q", language, want)
			}
		}
		for _, mode := range []string{"rule", "global", "direct"} {
			painted := settingsPaintedTarget(t, body, hits, "mode."+mode)
			if strings.Contains(painted, "<") != (mode == "rule") {
				t.Fatalf("saved mode selection used the running mode: %q", painted)
			}
		}
		if hasAction(hits, "tun.enable") {
			t.Fatal("TUN remains editable while service diagnosis is unresolved")
		}
		o.Snapshot.ServiceError = ""
		o.Snapshot.Service.State = "activating"
		_, hits = settingsContent(o, 120)
		if hasAction(hits, "tun.enable") {
			t.Fatal("TUN remains editable during a service transition")
		}
		o.Snapshot.Service.State = "inactive"
		o.Snapshot.Capabilities.TUN.Supported = false
		o.Snapshot.Capabilities.Scope = "user"
		o.Snapshot.Service.Linger = false
		o.Snapshot.Service.LingerKnown = false
		body, hits = settingsContent(o, 120)
		serviceBody := ansi.Strip(strings.Join(settingsService(o, 200).lines, "\n"))
		if hasAction(hits, "tun.enable") || !strings.Contains(ansi.Strip(body), o.Locale.T("settings.unsupported")) || !strings.Contains(serviceBody, o.Locale.T("dashboard.settings.lingerUnknown")) {
			t.Fatal("user scope lost TUN unavailability or unknown linger status")
		}
	}
}

func TestSettingsNetworkReportsUnknownRunningStateAndErrors(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		o := dashboardOptions{Locale: i18n.New(language), Height: 32, Snapshot: settingsTestSnapshot()}
		o.Snapshot.Service.Active = true
		o.Snapshot.Mode.Error = "mode fixture diagnostic"
		o.Snapshot.TUNError = "TUN fixture diagnostic"
		body := ansi.Strip(strings.Join(settingsNetwork(o, 160).lines, "\n"))
		for _, want := range []string{o.Snapshot.Mode.Error, o.Snapshot.TUNError, o.Locale.T("dashboard.settings.actualMode", o.Locale.T("dashboard.status.unknown"))} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s: network settings omitted %q", language, want)
			}
		}
		o.Snapshot.Service.EnableState = "unknown"
		block := settingsService(o, 160)
		body = ansi.Strip(strings.Join(block.lines, "\n"))
		if !strings.Contains(body, o.Locale.T("dashboard.status.unknown")) || !strings.Contains(settingsPaintedTarget(t, body, block.targets, "autostart-enable"), o.Locale.T("dashboard.settings.enableAutostart")) {
			t.Fatalf("%s: unknown autostart was presented as an observed off state", language)
		}
	}
}

func TestSettingsRepeatedResizePreservesFocusAndTabRevealsEveryAction(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
		directionCleanup(t, &m)
		m.locale, m.page, m.focus = i18n.New(language), "settings", "runtime-settings"
		for _, size := range [][2]int{{240, 45}, {200, 40}, {160, 45}, {120, 32}, {105, 32}, {104, 32}, {100, 28}, {99, 28}, {84, 24}, {83, 24}, {80, 24}, {60, 20}, {40, 16}, {5, 2}, {120, 18}, {240, 45}} {
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
			if m.focus != "runtime-settings" || m.ports.open || m.busy {
				t.Fatalf("resize changed focused setting or activated it: focus=%s busy=%t", m.focus, m.busy)
			}
			if size[0] >= 40 && !hasAction(m.targets, m.focus) {
				t.Fatalf("%s/%v: resized focused setting is not revealed", language, size)
			}
		}
		m, _ = update(m, tea.WindowSizeMsg{Width: 60, Height: 20})
		wanted := map[string]bool{}
		for _, hit := range m.contentTargets() {
			wanted[hit.ID] = true
		}
		seen := map[string]bool{}
		var trace []string
		for range 2*len(m.focusTargets()) + 2 {
			m, _ = update(m, special(tea.KeyTab))
			trace = append(trace, m.focus)
			if wanted[m.focus] {
				seen[m.focus] = true
				if !hasAction(m.targets, m.focus) {
					t.Fatalf("%s: Tab focused offscreen setting %s without revealing it", language, m.focus)
				}
			}
		}
		if len(seen) != len(wanted) || m.busy || m.ports.open || m.wizard.open {
			t.Fatalf("Tab lost or activated a setting: seen=%v wanted=%v trace=%v", seen, wanted, trace)
		}
	}
}
