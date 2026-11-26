package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/buildinfo"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func settingsTestSnapshot() app.Snapshot {
	return app.Snapshot{
		Installed: true, ConfigReady: true, HasRun: true,
		Version: "v1.19.33", RunningVersion: "v1.19.32", Controller: "127.0.0.1:9097",
		Service:         service.Status{State: "inactive", EnableState: "disabled", Linger: true, LingerKnown: true},
		Mode:            app.ModeStatus{Desired: "rule", Actual: "global"},
		CoreSettings:    mihomo.DefaultGeneralSettings(),
		RuntimeSettings: app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097},
		Capabilities:    app.Capabilities{Scope: "system", Runtime: app.Capability{Supported: true}, TUN: app.Capability{Supported: true}},
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
			{115, 32, 90, 2}, {114, 32, 110, 2}, {105, 32, 101, 2}, {104, 32, 100, 2}, {100, 28, 96, 2}, {99, 28, 95, 2},
			{84, 24, 80, 2}, {83, 24, 79, 1}, {80, 24, 76, 1}, {40, 16, 36, 1},
			{240, 19, 215, 2}, {120, 18, 95, 2}, {115, 9, 90, 2},
		} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, size.width, size.height), func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), Page: "settings", Width: size.width, Height: size.height, Snapshot: settingsTestSnapshot()}
				o.Header = (&Model{page: "settings", locale: o.Locale}).pageHeader()
				g := dashboardGeometry(o)
				if g.width != size.bodyWidth {
					t.Fatalf("settings body width=%d, want available width %d", g.width, size.bodyWidth)
				}
				screen, hits, _ := renderDashboard(o)
				assertCanvas(t, screen, size.width, size.height)
				assertRegions(t, hits, size.width, size.height)
				titles := ansi.Strip(strings.Split(screen, "\n")[g.y+1])
				if !strings.HasPrefix(ansi.Cut(titles, g.x, g.x+g.width), "  "+o.Locale.T("settings.interface")) || strings.Contains(titles, o.Locale.T("settings.mihomo")) != (size.columns == 2) {
					t.Fatalf("unexpected titles in %d-column settings: %q", size.columns, titles)
				}
			})
		}
	}
}

func TestSettingsCardsStackAtTheirNaturalHeights(t *testing.T) {
	installed := settingsTestSnapshot()
	running := installed
	running.Service.Active, running.Service.State = true, "active"
	running.Drift = strings.Repeat("Configuration differs from the service. ", 20)

	for _, tc := range []struct {
		name          string
		width, height int
		snapshot      app.Snapshot
	}{
		{"installed", 160, 45, installed},
		{"odd width with long diagnostic", 121, 18, running},
		{"two columns before installation", 80, 24, app.Snapshot{}},
		{"single column", 79, 24, installed},
		{"narrow", 40, 16, running},
		{"minimal", 3, 2, installed},
		{"one cell", 1, 1, installed},
	} {
		for _, language := range []string{i18n.English, i18n.Chinese} {
			t.Run(tc.name+"/"+language, func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), Height: tc.height, Snapshot: tc.snapshot}
				body, hits := settingsContent(o, tc.width)
				rows := strings.Split(ansi.Strip(body), "\n")
				assertCanvas(t, body, tc.width, len(rows))
				assertRegions(t, hits, tc.width, len(rows))

				// Each column must reproduce its standalone cards, including their
				// padding and local hit rectangles, with one blank row between.
				checkColumn := func(x, width int, cards ...func(dashboardOptions, int) dashboardBlock) int {
					t.Helper()
					var want []string
					for index, render := range cards {
						if index > 0 {
							want = append(want, "")
						}
						card := render(o, width)
						for _, target := range card.targets {
							target.X += x
							target.Y += len(want)
							if got := findTarget(t, Model{targets: hits}, target.ID); got != target {
								t.Fatalf("misplaced %s: got %+v, want %+v", target.ID, got, target)
							}
						}
						want = append(want, card.lines...)
					}
					for y, row := range rows {
						expected := ""
						if y < len(want) {
							expected = ansi.Strip(want[y])
						}
						got := ansi.Cut(row, x, x+width)
						if strings.TrimRight(got, " ") != strings.TrimRight(expected, " ") {
							t.Fatalf("column at %d, row %d: got %q, want %q", x, y, got, expected)
						}
					}
					return len(want)
				}
				var height int
				if tc.width < 80 {
					height = checkColumn(0, tc.width, settingsInterface, settingsMihomo, settingsCore, settingsDiagnostics)
				} else {
					left := (tc.width - 2) / 2
					leftHeight := checkColumn(0, left, settingsInterface, settingsDiagnostics)
					rightHeight := checkColumn(left+2, tc.width-left-2, settingsMihomo, settingsCore)
					height = max(leftHeight, rightHeight)
				}
				if len(rows) != height {
					t.Fatalf("body height = %d, want %d", len(rows), height)
				}
			})
		}
	}
}

func TestCoreSettingsTabsHaveEqualHeightAndExposeOnlyTheirControls(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range []struct{ cardWidth, terminalHeight int }{
			{23, 16}, {35, 24}, {40, 46}, {60, 24}, {60, 36}, {80, 46}, {120, 46},
		} {
			width := size.cardWidth
			o := dashboardOptions{Locale: i18n.New(language), Height: size.terminalHeight, Snapshot: settingsTestSnapshot()}
			want := [][]string{
				{"runtime-settings", "settings.allowLAN", "settings.bindAddress", "settings.accessRules", "settings.proxyAuth", "settings.externalControl"},
				{"settings.ipv6", "settings.tcpConcurrent", "settings.tcpKeepAlive", "settings.keepAliveIdle", "settings.keepAliveInterval", "settings.unifiedDelay", "settings.logLevel", "settings.findProcessMode"},
				{"settings.geoAutoUpdate", "settings.geodataLoader", "settings.etagSupport", "settings.storeFakeIP"},
			}
			height := 0
			for tab, id := range coreSettingsTabs {
				o.settingsTab = coreSettingsTab(tab)
				card := settingsMihomo(o, width)
				if tab > 0 && len(card.lines) != height {
					t.Fatalf("%s/card=%d,terminalHeight=%d: tab %s changed height from %d to %d", language, width, size.terminalHeight, id, height, len(card.lines))
				}
				height = len(card.lines)
				if len(card.targets) != 1+len(want[tab]) || card.targets[0].ID != id {
					t.Fatalf("%s: unexpected controls %+v", id, card.targets)
				}
				for i, target := range card.targets[1:] {
					if target.ID != want[tab][i] {
						t.Fatalf("%s: target %d = %s", id, i, target.ID)
					}
				}
				assertCanvas(t, strings.Join(card.lines, "\n"), width, height)
				assertRegions(t, card.targets, width, height)
				if tab == int(accessTab) {
					for _, action := range []string{"runtime-settings", "settings.bindAddress", "settings.accessRules", "settings.proxyAuth", "settings.externalControl"} {
						target := findTarget(t, Model{targets: card.targets}, action)
						if target.X != 0 || target.W != width {
							t.Fatalf("%s only clickable at %+v", action, target)
						}
					}
				}
			}
			o.settingsTab = networkTab
			o.Snapshot.CoreSettings.DisableKeepAlive = "on"
			if hits := settingsMihomo(o, width).targets; hasAction(hits, "settings.keepAliveInterval") || hasAction(hits, "settings.keepAliveIdle") {
				t.Fatal("keepalive disabled while its fields remain editable")
			}
			o.settingsTab = resourcesTab
			o.Snapshot.CoreSettings.GeoAutoUpdate = "on"
			if !hasAction(settingsMihomo(o, width).targets, "settings.geoUpdateInterval") {
				t.Fatal("enabled GEO schedule lacks editable interval")
			}
		}
	}
}

func TestSettingsProxyAuthSummary(t *testing.T) {
	for _, tc := range []struct {
		language string
		values   []string
	}{
		{i18n.Chinese, []string{"未配置 ›", "1 个用户 ›", "2 个用户 ›"}},
		{i18n.English, []string{"Not configured ›", "1 user ›", "2 users ›"}},
	} {
		o := dashboardOptions{Locale: i18n.New(tc.language), Snapshot: settingsTestSnapshot()}
		for count, value := range tc.values {
			o.Snapshot.RuntimeSettings.ProxyUserCount = count
			body, hits := settingsContent(o, 160)
			row := settingsPaintedTarget(t, body, hits, "settings.proxyAuth")
			want := o.Locale.T("settings.proxyAuth") + " " + value
			if got := strings.Join(strings.Fields(row), " "); got != want {
				t.Errorf("%s, %d users: got %q, want %q", tc.language, count, got, want)
			}
		}
	}
}

func TestSettingsEditorRowShowsSavedCommandAndOpensFullValue(t *testing.T) {
	const width = 40
	for _, tc := range []struct {
		command, suffix string
	}{
		{"", "›"},
		{"nvim -f", "nvim -f ›"},
		{"nvim --cmd 'echo " + strings.Repeat("编辑", 15) + "'", "… ›"},
	} {
		m := ready(&fakeBackend{prefs: app.Preferences{Editor: tc.command}})
		m.page, m.focus, m.width, m.height = "settings", "settings.language", width, 24
		m.locale = i18n.New(i18n.Chinese)
		m.layout()
		row := findTarget(t, m, "editor.configure")
		if row.W != dashboardBodyWidth(m.navigationOptions()) || row.H != 1 {
			t.Fatalf("command %q should have one full-width row: %+v", tc.command, row)
		}
		line := strings.TrimSpace(settingsPaintedTarget(t, m.screen, m.targets, row.ID))
		label := m.tr("settings.editor")
		if !strings.HasPrefix(line, label) || !strings.HasSuffix(line, tc.suffix) {
			t.Fatalf("command %q rendered as %q", tc.command, line)
		}
		if tc.command == "" && strings.TrimSpace(strings.TrimPrefix(line, label)) != "›" {
			t.Fatalf("automatic editor should show just an arrow: %q", line)
		}
		m, _ = click(m, row)
		if m.form != "editor-preference" || m.input.Value() != tc.command {
			t.Fatalf("editor row did not open complete command %q", tc.command)
		}
	}
}

func TestCoreSettingsTabKeyboardAndMouseNavigation(t *testing.T) {
	m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
	directionCleanup(t, &m)
	m.page, m.focus, m.width, m.height = "settings", coreSettingsTabs[0], 160, 45
	m.layout()
	m, _ = update(m, special(tea.KeyRight))
	if m.settingsTab != networkTab || m.focus != coreSettingsTabs[networkTab] {
		t.Fatal("right arrow did not select the next tab")
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "settings.ipv6" {
		t.Fatalf("Tab selected %s outside the current pane", m.focus)
	}
	target := findTarget(t, m, coreSettingsTabs[resourcesTab])
	m, _ = update(m, tea.MouseClickMsg{X: target.X + target.W - 1, Y: target.Y, Button: tea.MouseLeft})
	if m.settingsTab != resourcesTab || m.focus != target.ID || hasAction(m.targets, "settings.ipv6") {
		t.Fatal("clicking the tab padding should select and focus the resources pane")
	}
	m, _ = update(m, special(tea.KeyRight))
	if m.settingsTab != accessTab || m.hover != "" {
		t.Fatal("keyboard navigation should switch tabs and clear mouse hover")
	}
}

func TestSettingsSwitchesAndDropdownsAlignAtCardEdges(t *testing.T) {
	const inset = 2
	controls := []struct {
		id, label string
		card      func(dashboardOptions, int) dashboardBlock
	}{
		{"settings.language", "dashboard.language.title", settingsInterface},
		{"settings.theme", "settings.theme", settingsInterface},
		{"key-hints.toggle", "dashboard.interface.keyHints", settingsInterface},
		{autostartToggleTarget, "settings.autostart", settingsCore},
		{"settings.ipv6", "settings.ipv6", settingsMihomo},
		{"settings.unifiedDelay", "settings.unifiedDelay", settingsMihomo},
		{"settings.logLevel", "settings.logLevel", settingsMihomo},
		{"settings.tcpConcurrent", "settings.tcpConcurrent", settingsMihomo},
		{"settings.tcpKeepAlive", "settings.tcpKeepAlive", settingsMihomo},
		{"settings.findProcessMode", "settings.findProcessMode", settingsMihomo},
		{"settings.etagSupport", "settings.etagSupport", settingsMihomo},
	}
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, c := range controls {
			o := dashboardOptions{Locale: i18n.New(language), Snapshot: settingsTestSnapshot(), Focus: c.id, PlainButtons: true, Monochrome: true}
			o.settingsTab = networkTab
			if c.id == "settings.etagSupport" {
				o.settingsTab = resourcesTab
			}
			label := o.Locale.T(c.label)
			if c.id == "key-hints.toggle" || c.id == "settings.unifiedDelay" || c.id == "settings.tcpConcurrent" {
				label += " ⓘ"
			}
			render := func(width int) (dashboardBlock, hitRegion) {
				block := c.card(o, width)
				return block, findTarget(t, Model{targets: block.targets}, c.id)
			}
			_, wide := render(120)
			exactFit := 2*inset + ansi.StringWidth(label) + 2 + wide.W
			for _, width := range []int{exactFit - 1, exactFit, 120} {
				block, hit := render(width)
				labelY, wantX := hit.Y, width-inset-wide.W
				if width < exactFit {
					labelY = hit.Y - 1
					if c.id == "settings.language" || c.id == "settings.theme" || c.id == "key-hints.toggle" || c.id == autostartToggleTarget {
						wantX = inset
					}
				}
				if labelY < 0 || !strings.HasPrefix(ansi.Strip(block.lines[labelY]), "› "+label) || hit.X != wantX || hit.W != wide.W {
					t.Fatalf("%s/%s at %d: label and control misplaced, hit %+v, want X=%d\n%s", language, c.id, width, hit, wantX, ansi.Strip(strings.Join(block.lines, "\n")))
				}
				face := ansi.Strip(ansi.Cut(block.lines[hit.Y], hit.X, hit.X+hit.W))
				focusedSwitch := face == "  ░░░░██" || face == "  ██░░░░"
				focusedDropdown := strings.HasPrefix(face, "  [") && strings.HasSuffix(face, "]")
				if !focusedSwitch && !focusedDropdown {
					t.Fatalf("%s/%s at %d: hit misses focused control: %q", language, c.id, width, face)
				}
			}
		}
	}
}

func TestSettingsMihomoSwitchWaitsForSavedValueAndAllowsRetry(t *testing.T) {
	b := &fakeBackend{snapshot: settingsTestSnapshot()}
	b.snapshot.CoreSettings.IPv6, b.snapshot.CoreSettings.UnifiedDelay, b.snapshot.CoreSettings.LogLevel = "off", "on", "warning"
	b.snapshot.TUN = true
	calls := 0
	b.setCoreSetting = func(_ context.Context, key, value string) error {
		calls++
		if key != "ipv6" || value != "on" {
			t.Fatalf("toggle sent key=%q value=%q", key, value)
		}
		if calls == 1 {
			return errors.New("reload failed")
		}
		b.snapshot.CoreSettings.IPv6 = value
		return nil
	}
	m := ready(b)
	directionCleanup(t, &m)
	m.settingsTab = networkTab
	m.page, m.focus, m.width, m.height = "settings", "settings.ipv6", 160, 45
	m.layout()
	if !strings.Contains(m.settingsContextHelp(), m.tr("settings.tunIPv6")) {
		t.Fatal("saved IPv6 preference lacks the host TUN override explanation")
	}
	for attempt := 1; attempt <= 2; attempt++ {
		var cmd tea.Cmd
		if attempt == 1 {
			m, cmd = update(m, special(tea.KeyEnter))
		} else {
			hit := findTarget(t, m, "settings.ipv6")
			m, cmd = update(m, tea.MouseClickMsg{X: hit.X + hit.W - 1, Y: hit.Y, Button: tea.MouseLeft})
		}
		if m.menu.open || !m.busy || m.snapshot.CoreSettings.IPv6 != "off" {
			t.Fatal("activating the switch should save immediately and display the last confirmed setting")
		}
		m = finishProfileJob(t, m, cmd)
		if attempt == 1 && (m.snapshot.CoreSettings.IPv6 != "off" || !strings.Contains(m.failure, "reload failed")) {
			t.Fatal("failed switch changed the current value or hid the error")
		}
	}
	if calls != 2 || m.snapshot.CoreSettings.IPv6 != "on" || m.snapshot.CoreSettings.UnifiedDelay != "on" || m.snapshot.CoreSettings.LogLevel != "warning" {
		t.Fatalf("unexpected saved Mihomo settings after toggle: %+v", m.snapshot.CoreSettings)
	}
	if !strings.HasSuffix(settingsPaintedTarget(t, m.screen, m.targets, m.focus), "░░░░██") {
		t.Fatal("the IPv6 switch did not display the confirmed value")
	}
}

func TestSettingsMihomoLogMenuOffersOnlyFixedLevels(t *testing.T) {
	b := &fakeBackend{snapshot: settingsTestSnapshot()}
	b.setCoreSetting = func(_ context.Context, key, value string) error {
		if key != "log-level" || value != "silent" {
			t.Fatalf("log menu sent key=%q value=%q", key, value)
		}
		b.snapshot.CoreSettings.LogLevel = value
		return nil
	}
	m := ready(b)
	directionCleanup(t, &m)
	m.settingsTab = networkTab
	m.page, m.focus, m.width, m.height = "settings", "settings.logLevel", 160, 45
	m.layout()
	m, _ = update(m, special(tea.KeyEnter))
	want := []string{"debug", "info", "warning", "error", "silent"}
	if !m.menu.open || len(m.menu.items) != len(want) || m.menu.selected != 1 {
		t.Fatalf("unexpected log-level menu: %+v", m.menu)
	}
	for i, level := range want {
		if item := m.menu.items[i]; item.ID != level || item.Label != level || item.Checked != (level == "info") {
			t.Fatalf("unexpected log level at %d: %+v", i, item)
		}
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.menu.open || m.busy {
		t.Fatal("selecting the current log level started a save")
	}
	m, _ = click(m, findTarget(t, m, m.focus))
	bounds := m.menu.bounds
	last := tea.Mouse{X: bounds.X + 2, Y: bounds.Y + bounds.H - 2, Button: tea.MouseLeft}
	before := strings.Split(m.screen, "\n")[last.Y]
	m, _ = update(m, tea.MouseMotionMsg{X: last.X, Y: last.Y})
	if m.menu.selected != 1 || strings.Split(m.screen, "\n")[last.Y] != before {
		t.Fatal("moving over silent changed the dropdown highlight or keyboard choice")
	}
	var cmd tea.Cmd
	m, cmd = update(m, tea.MouseClickMsg(last))
	m = finishProfileJob(t, m, cmd)
	if !strings.Contains(strings.Join(strings.Fields(settingsPaintedTarget(t, m.screen, m.targets, m.focus)), " "), "silent ▾") {
		t.Fatal("the dropdown did not display the saved log level")
	}
}

func TestSettingsCoreSwitchesSaveMihomoValues(t *testing.T) {
	for _, tc := range []struct {
		id, key, first, second string
		startsOn               bool
	}{
		{"settings.tcpConcurrent", "tcp-concurrent", "on", "off", false},
		{"settings.tcpKeepAlive", "disable-keep-alive", "on", "off", true},
		{"settings.etagSupport", "etag-support", "off", "on", true},
		{"settings.geoAutoUpdate", "geo-auto-update", "on", "off", false},
		{"settings.storeFakeIP", "profile.store-fake-ip", "on", "off", false},
	} {
		t.Run(tc.id, func(t *testing.T) {
			b := &fakeBackend{snapshot: settingsTestSnapshot()}
			var written string
			b.setCoreSetting = func(_ context.Context, key, value string) error {
				if key != tc.key {
					t.Fatalf("sent key %q, want %q", key, tc.key)
				}
				saved, err := b.snapshot.CoreSettings.With(key, value)
				if err == nil {
					b.snapshot.CoreSettings = saved
					written = value
				}
				return err
			}
			m := ready(b)
			directionCleanup(t, &m)
			m.settingsTab = networkTab
			if tc.id == "settings.etagSupport" || tc.id == "settings.geoAutoUpdate" || tc.id == "settings.storeFakeIP" {
				m.settingsTab = resourcesTab
			}
			m.page, m.focus, m.width, m.height = "settings", tc.id, 160, 45
			m.layout()
			on := tc.startsOn
			for _, want := range []string{tc.first, tc.second} {
				var cmd tea.Cmd
				m, cmd = update(m, special(tea.KeyEnter))
				m = finishProfileJob(t, m, cmd)
				if written != want {
					t.Fatalf("saved %s=%s, want %s", tc.key, written, want)
				}
				on = !on
				track := "██░░░░"
				if on {
					track = "░░░░██"
				}
				if got := settingsPaintedTarget(t, m.screen, m.targets, tc.id); !strings.HasSuffix(got, track) {
					t.Fatalf("after saving %s=%s, switch = %q, want %q", tc.key, want, got, track)
				}
			}
		})
	}
}

func TestSettingsProcessMenuUsesLocalizedLabelsAndNativeValues(t *testing.T) {
	for _, lang := range []string{i18n.English, i18n.Chinese} {
		t.Run(lang, func(t *testing.T) {
			b := &fakeBackend{snapshot: settingsTestSnapshot()}
			b.setCoreSetting = func(_ context.Context, key, value string) error {
				if key != "find-process-mode" || value != "always" {
					t.Fatalf("selection saved %s=%s", key, value)
				}
				b.snapshot.CoreSettings.FindProcessMode = value
				return nil
			}
			m := ready(b)
			directionCleanup(t, &m)
			m.locale = i18n.New(lang)
			m.settingsTab = networkTab
			m.page, m.focus, m.width, m.height = "settings", "settings.findProcessMode", 160, 45
			m.layout()
			m, _ = click(m, findTarget(t, m, m.focus))
			want := []menuItem{
				{ID: "strict", Label: m.tr("settings.processAsNeeded"), Checked: true},
				{ID: "always", Label: m.tr("settings.processAlways")},
				{ID: "off", Label: m.tr("settings.processOff")},
			}
			if !m.menu.open || len(m.menu.items) != len(want) || m.menu.selected != 0 {
				t.Fatalf("unexpected process menu: %+v", m.menu)
			}
			for i, item := range want {
				if m.menu.items[i] != item {
					t.Fatalf("process menu item %d = %+v, want %+v", i, m.menu.items[i], item)
				}
			}
			m, _ = update(m, special(tea.KeyDown))
			var cmd tea.Cmd
			m, cmd = update(m, special(tea.KeyEnter))
			m = finishProfileJob(t, m, cmd)
			face := settingsPaintedTarget(t, m.screen, m.targets, m.focus)
			if !strings.Contains(face, m.tr("settings.processAlways")) {
				t.Fatalf("saved process mode missing from trigger %q", face)
			}
		})
	}
}

func TestSettingsUninstalledActionsAndHeaderOwnership(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English), Width: 160, Height: 45}
	body, hits := settingsContent(o, 120)
	actions := []string{"install", "card.logs", "settings.language", "settings.startPage", "key-hints.toggle", "settings.theme", "editor.configure", coreSettingsTabs[accessTab]}
	for _, id := range actions {
		if !hasAction(hits, id) {
			t.Fatalf("uninstalled settings lost %s", id)
		}
	}
	if len(hits) != len(actions) || !strings.Contains(ansi.Strip(body), o.Locale.T("settings.afterInstall")) {
		t.Fatal("uninstalled settings expose installed-core actions or omit their availability")
	}
	if hasAction(hits, "network") {
		t.Fatal("download settings gained a fallback action in the body")
	}
	o.Page = "settings"
	o.Header = (&Model{page: "settings", locale: o.Locale}).pageHeader()
	_, hits = settingsContent(o, 120)
	if hasAction(hits, "network") || len(hits) != len(actions) {
		t.Fatal("download settings are duplicated in the header and body")
	}
	_, hits, _ = renderDashboard(o)
	networks := 0
	for _, hit := range hits {
		if hit.ID == "network" {
			networks++
		}
	}
	if networks != 1 {
		t.Fatalf("expected one download-network header entry, got %d", networks)
	}
}

func TestSettingsThemeMenuTracksTriggerAndKeepsChoicePending(t *testing.T) {
	m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
	directionCleanup(t, &m)
	m.page, m.focus = "settings", "settings.theme"
	m, _ = update(m, tea.WindowSizeMsg{Width: 160, Height: 45})
	m, _ = update(m, special(tea.KeyEnter))
	options := []menuItem{
		{ID: app.ThemeLight, Label: "Light"},
		{ID: app.ThemeDark, Label: "Dark", Checked: true},
		{ID: app.ThemeAuto, Label: "Auto"},
	}
	if !m.menu.open || len(m.menu.items) != len(options) {
		t.Fatalf("expected three theme options, got %+v", m.menu)
	}
	for i, want := range options {
		item := m.menu.items[i]
		if item != want {
			t.Fatalf("unexpected theme option at %d: %+v", i, item)
		}
	}
	m, _ = update(m, special(tea.KeyUp))
	m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	trigger := findTarget(t, m, "settings.theme")
	if !m.menu.open || m.menu.anchor != trigger || m.menu.selected != 0 || !m.menu.items[1].Checked {
		t.Fatal("resizing lost the trigger, pending light choice, or saved dark check")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.menu.open || m.busy || m.theme != app.ThemeDark || m.focus != "settings.theme" {
		t.Fatalf("cancelling changed the theme or focus: %s, %s", m.theme, m.focus)
	}
}

func TestSettingsThemeExplainsLightPlaceholderAndAutoChoice(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		o := dashboardOptions{Locale: i18n.New(language)}
		for _, tc := range []struct {
			theme, effective string
			placeholder      bool
		}{
			{app.ThemeDark, app.ThemeDark, false}, {app.ThemeLight, app.ThemeLight, true},
			{app.ThemeAuto, app.ThemeDark, false}, {app.ThemeAuto, app.ThemeLight, true},
		} {
			o.Theme, o.EffectiveTheme = tc.theme, tc.effective
			row := settingsThemeRow(o)
			if o.settingsDropdown(row.actions[0].id).Value != tc.theme ||
				strings.Contains(row.note, o.Locale.T("settings.themeLightPlaceholder")) != tc.placeholder ||
				strings.Contains(row.note, o.Locale.T("settings.themeAuto", (dropdown{Items: themeMenuItems, Value: tc.effective}).label())) != (tc.theme == app.ThemeAuto) {
				t.Fatalf("%s/%s/%s: unexpected theme setting %+v", language, tc.theme, tc.effective, row)
			}
		}
	}
}

func TestSettingsDiagnosticsContainOnlyRelevantObservedIssues(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		o := dashboardOptions{Locale: i18n.New(language), Height: 32, Snapshot: settingsTestSnapshot(), PlainButtons: true}
		healthy := ansi.Strip(strings.Join(settingsDiagnostics(o, 200).lines, "\n"))
		for _, want := range []string{buildinfo.Version, "lazyclash doctor", o.Locale.T("settings.openLogs")} {
			if !strings.Contains(healthy, want) {
				t.Fatalf("%s: diagnostics omitted its independent entry or client version %q", language, want)
			}
		}
		o.Snapshot.Service.Result, o.Snapshot.Service.ExitCode = "exit-code", 17
		o.Snapshot.ServiceError, o.Snapshot.Drift = "service fixture diagnostic", "saved state differs from service"
		block := settingsDiagnostics(o, 200)
		plain := ansi.Strip(strings.Join(block.lines, "\n"))
		for _, want := range []string{
			o.Snapshot.ServiceError, o.Snapshot.Drift,
			o.Locale.T("dashboard.settings.serviceResult", o.Snapshot.Service.Result, o.Snapshot.Service.ExitCode),
		} {
			if !strings.Contains(plain, want) {
				t.Fatalf("%s: missing setting diagnostic %q", language, want)
			}
			if strings.Contains(healthy, want) {
				t.Fatalf("%s: healthy diagnostics displayed an unrelated issue %q", language, want)
			}
		}
		if !hasAction(block.targets, "card.logs") {
			t.Fatal("service diagnosis disabled the independent Logs entry")
		}
		for name, render := range map[string]func(dashboardOptions, int) dashboardBlock{"interface": settingsInterface, "settings": settingsMihomo, "core": settingsCore} {
			plain := ansi.Strip(strings.Join(render(o, 200).lines, "\n"))
			for _, issue := range []string{o.Snapshot.ServiceError, o.Snapshot.Drift, "exit-code"} {
				if strings.Contains(plain, issue) {
					t.Fatalf("%s: %s repeated the diagnostic %q", language, name, issue)
				}
			}
		}
		o.Snapshot.ServiceError, o.Snapshot.Drift = "", ""
		o.Snapshot.Service.Result, o.Snapshot.Service.ExitCode = "success", 0
		plain = ansi.Strip(strings.Join(settingsDiagnostics(o, 200).lines, "\n"))
		if strings.Contains(plain, o.Locale.T("dashboard.settings.serviceResult", "success", 0)) {
			t.Fatal("successful service result was displayed as a diagnostic")
		}
	}
}

func TestSettingsAutostartAndCoreRestartGuards(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		o := dashboardOptions{Locale: i18n.New(language), Snapshot: settingsTestSnapshot()}
		block := settingsMihomo(o, 200)
		core := settingsCore(o, 200)
		if !hasAction(core.targets, "restart") || hasAction(block.targets, "restart") || !hasAction(block.targets, "runtime-settings") || hasAction(core.targets, "runtime-settings") {
			t.Fatal("core restart or service port editor lost its owning group")
		}
		o.Snapshot.Service.EnableState = "unknown"
		block = settingsCore(o, 200)
		body := ansi.Strip(strings.Join(block.lines, "\n"))
		if !strings.Contains(body, o.Locale.T("dashboard.status.unknown")) || !strings.Contains(settingsPaintedTarget(t, body, block.targets, autostartToggleTarget), o.Locale.T("dashboard.settings.enableAutostart")) {
			t.Fatalf("%s: unknown autostart was presented as an observed off state", language)
		}
		enable := findTarget(t, Model{targets: block.targets}, autostartToggleTarget)
		left := ansi.Strip(ansi.Cut(block.lines[enable.Y], 0, 60))
		if enable.X+enable.W != 200-2 || !strings.HasPrefix(left, "  "+o.Locale.T("settings.autostart")) || !strings.Contains(left, o.Locale.T("dashboard.status.unknown")) {
			t.Fatalf("%s: unknown autostart should keep its status left and action right: %q", language, ansi.Strip(block.lines[enable.Y]))
		}
		for _, state := range []string{"activating", "deactivating", "reloading"} {
			o.Snapshot.Service.State = state
			if hasAction(settingsCore(o, 200).targets, "restart") {
				t.Fatalf("restart remains available during %s", state)
			}
		}
		o.Snapshot.Service.State, o.Snapshot.ServiceError = "inactive", "service fixture diagnostic"
		if hasAction(settingsCore(o, 200).targets, "restart") {
			t.Fatal("restart remains available with an unresolved service diagnostic")
		}
		o.Snapshot.ServiceError = ""
		o.Snapshot.Capabilities.Scope = "user"
		o.Snapshot.Service.Linger, o.Snapshot.Service.LingerKnown = false, false
		body = ansi.Strip(strings.Join(settingsCore(o, 200).lines, "\n"))
		if !strings.Contains(body, o.Locale.T("dashboard.settings.lingerUnknown")) {
			t.Fatal("user service lost its unknown linger explanation")
		}
	}
}

func TestSettingsDoesNotRepeatRuntimeAndProfileActions(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		o := dashboardOptions{Locale: i18n.New(language), Snapshot: settingsTestSnapshot()}
		o.Snapshot.Mode.Error, o.Snapshot.TUNError = "mode fixture diagnostic", "TUN fixture diagnostic"
		body, hits := settingsContent(o, 160)
		for _, hit := range hits {
			if strings.HasPrefix(hit.ID, "mode.") || strings.HasPrefix(hit.ID, "tun.") || hit.ID == "start" || hit.ID == "stop" || hit.ID == "import" || hit.ID == "network" {
				t.Fatalf("settings repeated the page-owned action %s", hit.ID)
			}
		}
		plain := ansi.Strip(body)
		for _, removed := range []string{o.Snapshot.Mode.Error, o.Snapshot.TUNError, o.Locale.T("dashboard.control.routingMode"), o.Locale.T("tun.host_scope"), o.Snapshot.RunningVersion} {
			if strings.Contains(plain, removed) {
				t.Fatalf("settings retained the duplicated runtime display %q", removed)
			}
		}
		core := ansi.Strip(strings.Join(settingsCore(o, 200).lines, "\n"))
		if !strings.Contains(core, o.Snapshot.Version) || strings.Contains(core, o.Snapshot.RunningVersion) {
			t.Fatal("core management does not show the installed version independently of the running version")
		}
	}
}

func TestSettingsLogsEntryNavigatesWithoutChangingRuntime(t *testing.T) {
	m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
	directionCleanup(t, &m)
	m.width, m.height, m.page, m.focus = 80, 24, "settings", "card.logs"
	m.revealContentFocus()
	m.layout()
	id := m.id
	m, _ = click(m, findTarget(t, m, "card.logs"))
	if m.page != "logs" || m.busy || m.id != id || m.ports.open || m.wizard.open {
		t.Fatal("Logs entry failed to navigate independently or changed runtime settings")
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

func TestSettingsPointerMotionPreservesAppearanceAndFocus(t *testing.T) {
	m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
	directionCleanup(t, &m)
	m.width, m.height, m.page, m.focus = 160, 45, "settings", "settings.language"
	m.layout()
	before := strings.Split(m.screen, "\n")
	checkMotion := func(name string, x, y int) {
		t.Helper()
		m, _ = update(m, tea.MouseMotionMsg{X: x, Y: y})
		if strings.Split(m.screen, "\n")[y] != before[y] || m.focus != "settings.language" || m.settingsTab != accessTab {
			t.Errorf("pointer over %s changed the row appearance, keyboard focus or active tab", name)
		}
	}
	for _, id := range []string{
		coreSettingsTabs[accessTab], coreSettingsTabs[resourcesTab], "settings.allowLAN",
		"runtime-settings", "settings.language", "settings.theme", "editor.configure", "install", "card.logs", "network",
	} {
		hit := findTarget(t, m, id)
		checkMotion(id, hit.X+hit.W/2, hit.Y)
	}
	switchHit := findTarget(t, m, "settings.allowLAN")
	checkMotion("space beside switch", switchHit.X-2, switchHit.Y)
	m, _ = update(m, tea.MouseClickMsg{X: switchHit.X - 2, Y: switchHit.Y, Button: tea.MouseLeft})
	if m.focus != "settings.language" || m.busy || m.ports.open {
		t.Fatal("clicking beside the switch activated it or moved keyboard focus")
	}
	m, _ = update(m, special(tea.KeyTab))
	focused := findTarget(t, m, m.focus)
	if m.focus == "settings.language" || strings.Split(m.screen, "\n")[focused.Y] == before[focused.Y] {
		t.Fatalf("Tab should visibly focus the next control, got %s", m.focus)
	}
}

func TestAccessSettingsKeepLongIPv6AddressVisibleWithAndWithoutAccounts(t *testing.T) {
	const host = "[2001:db8:1234:5678:90ab:cdef:1234:5678]"
	for _, width := range []int{26, 40, 80} {
		for _, accounts := range []int{0, 2} {
			o := dashboardOptions{Locale: i18n.New(i18n.Chinese), Height: 24, Snapshot: settingsTestSnapshot()}
			runtime := &o.Snapshot.RuntimeSettings
			runtime.AllowLAN, runtime.BindAddress, runtime.ProxyUserCount = true, host, accounts
			b := settingsMihomo(o, width)
			var joined string
			for _, line := range b.lines {
				joined += strings.TrimSpace(ansi.Strip(line))
			}
			// Both the editable binding and the derived connection URI should
			// survive wrapping in the single-column settings card.
			userInfo := ""
			if accounts > 0 {
				userInfo = o.Locale.T("settings.proxyUserinfo")
			}
			if strings.Count(joined, host) != 2 || !strings.Contains(joined, runtime.ProxyAddress(userInfo)) {
				t.Fatalf("address truncated at %d columns with %d accounts: %s", width, accounts, joined)
			}
			if hasAction(b.targets, "settings.copyProxyAddress") != (accounts > 0) {
				t.Fatal("copy control did not match saved accounts")
			}
		}
	}
}
