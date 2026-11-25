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

func TestLocalizedDashboardPreservesCanvasAndActions(t *testing.T) {
	longName := strings.Repeat("自定义代理组中文路径/", 12)
	snapshot := app.Snapshot{
		Installed: true, ConfigReady: true, HasRun: true,
		Version: "v1.19.33", RunningVersion: "v1.19.33", Applied: longName,
		Controller: "http://127.0.0.1:9097", Profiles: []string{"default", longName},
		Groups:  []mihomo.Group{{Name: longName, Type: "Selector", Now: longName}},
		Service: service.Status{Active: true, State: "active", Linger: true},
		Mode:    app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
	}
	for _, language := range []string{"en", "zh-Hans"} {
		locale := i18n.New(language)
		for _, size := range [][2]int{{160, 45}, {120, 32}, {100, 28}, {80, 24}, {60, 20}, {30, 12}, {20, 7}, {10, 4}, {1, 1}} {
			for _, page := range navPages() {
				t.Run(fmt.Sprintf("%s/%dx%d/%s", language, size[0], size[1], page), func(t *testing.T) {
					m := Model{locale: locale, page: page, snapshot: snapshot}
					o := dashboardOptions{Locale: locale, Width: size[0], Height: size[1], Page: page, Snapshot: snapshot, Header: m.pageHeader()}
					screen, hits, _ := renderDashboard(o)
					assertCanvas(t, screen, size[0], size[1])
					assertRegions(t, hits, size[0], size[1])
					if size[0] >= 100 && size[1] >= 20 {
						rows := strings.Split(screen, "\n")
						for _, hit := range hits {
							if strings.HasPrefix(hit.ID, "nav.") {
								painted := ansi.Strip(ansi.Cut(rows[hit.Y+hit.H/2], hit.X, hit.X+hit.W))
								if !strings.Contains(painted, pageTitle(strings.TrimPrefix(hit.ID, "nav."), locale)) {
									t.Fatalf("navigation action misses its translated label: %s=%q", hit.ID, painted)
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestLanguageDropdownUsesCurrentNativeNameAndDisplayCellHit(t *testing.T) {
	for language, label := range map[string]string{i18n.English: "English ▾", i18n.Chinese: "简体中文 ▾"} {
		for _, width := range []int{20, 30, 56, 80} {
			o := dashboardOptions{Locale: i18n.New(language), PlainButtons: true}
			block := settingsInterface(o, width)
			assertCanvas(t, strings.Join(block.lines, "\n"), width, len(block.lines))
			assertRegions(t, block.targets, width, len(block.lines))
			hit := findTarget(t, Model{targets: block.targets}, "settings.language")
			painted := ansi.Strip(ansi.Cut(block.lines[hit.Y], hit.X, hit.X+hit.W))
			if !strings.Contains(painted, label) || ansi.StringWidth(painted) != hit.W {
				t.Fatalf("%s/%d: language target misses label %q: %q", language, width, label, painted)
			}
			if !strings.Contains(ansi.Strip(block.lines[hit.Y+1]), "──") {
				t.Fatalf("%s/%d: a hint occupies the row below the language control", language, width)
			}
			o.Busy = true
			if hasAction(settingsInterface(o, width).targets, "settings.language") {
				t.Fatal("language switching remains clickable during an operation")
			}
		}
	}
}

func TestLocalizedHelpCloseHitMatchesPaintedButton(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 20}, {30, 12}, {26, 8}} {
			m := Model{locale: i18n.New(language), page: "settings", width: size[0], height: size[1], helpOpen: true}
			screen, hits := m.renderHelp("")
			assertCanvas(t, screen, size[0], size[1])
			assertRegions(t, hits, size[0], size[1])
			if len(hits) == 0 || hits[0].ID != "help.close" {
				t.Fatal("help close action missing")
			}
			hit := hits[0]
			painted := ansi.Strip(ansi.Cut(strings.Split(screen, "\n")[hit.Y], hit.X, hit.X+hit.W))
			label := strings.Trim(m.tr("help.close"), "[] ")
			if strings.Trim(painted, "› ") != label || hit.W != ansi.StringWidth(label)+4 || !strings.HasPrefix(painted, " › ") || !strings.HasSuffix(painted, " ") {
				t.Fatalf("%s: close hit includes adjacent hint or misses translated label: %q", language, painted)
			}
			m.targets = hits
			m.helpMouse(tea.MouseClickMsg{X: hit.X + hit.W - 1, Y: hit.Y, Button: tea.MouseLeft})
			if m.helpOpen {
				t.Fatal("clicking translated close button's final cell did not close help")
			}
		}
	}
}

func TestDashboardTransientMessagesDoNotDependOnTranslatedText(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		o := dashboardOptions{Locale: i18n.New(language), Width: 80, Height: 24, Message: "custom idle refresh", MessageTransient: true}
		screen, _, _ := renderDashboard(o)
		if strings.Contains(ansi.Strip(screen), o.Message) {
			t.Fatal("explicitly transient message remained visible")
		}
		o.Message, o.MessageTransient = "State refreshed", false
		o.Notifications = []notificationItem{{ID: 1, Kind: "done", Message: o.Message}}
		screen, _, _ = renderDashboard(o)
		if !strings.Contains(ansi.Strip(screen), o.Message) {
			t.Fatal("message was hidden because of English wording")
		}
	}
}

func TestLocalizedCoreControlsPreserveStateAndSelection(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New("zh-Hans"), Snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true, State: "active"}}}
	block := overviewCoreCard(o, 56)
	plain := ansi.Strip(strings.Join(block.lines, "\n"))
	if !hasAction(block.targets, "core.toggle") || strings.Count(plain, "运行中") != 1 || !strings.Contains(plain, "停止") || strings.Contains(plain, "启动") || strings.Contains(plain, "已开启") {
		t.Fatal("core toggle lost its localized running state or stop affordance")
	}
	o.Snapshot.Service.Active = false
	o.Snapshot.Service.State = "unknown"
	block = overviewCoreCard(o, 56)
	plain = ansi.Strip(strings.Join(block.lines, "\n"))
	if !hasAction(block.targets, "core.toggle") || !strings.Contains(plain, "? 未知") || strings.Contains(plain, "运行中") || strings.Contains(plain, "已开启") || strings.Contains(plain, "启动") || strings.Contains(plain, "停止") {
		t.Fatal("unknown core toggle invented a confirmed state or lost focusability")
	}
}
