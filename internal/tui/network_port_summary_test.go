package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestOnlySettingsSummarizesSavedMixedPort(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := Model{locale: i18n.New(language), ports: newPortsState(), snapshot: app.Snapshot{
			Installed: true, MixedPort: 7897, RuntimeSettings: app.RuntimeSettings{MixedPort: 7897},
			Controller: "http://private-user:private-password@127.0.0.1:9097/private-path?token=private-token#private-fragment",
		}}
		m.ports.current = app.RuntimeSettings{MixedPort: 18080, ControllerPort: 19090}
		m.ports.mixed.SetValue("18888")
		for _, width := range []int{40, 100} {
			o := m.dashboardOptions()
			o.Snapshot = m.snapshot
			for name, render := range map[string]func(dashboardOptions, int) dashboardBlock{
				"overview": overviewNetworkCard, "settings": settingsMihomo,
			} {
				t.Run(fmt.Sprintf("%s/%s/%d", language, name, width), func(t *testing.T) {
					card := render(o, width)
					plain := ansi.Strip(strings.Join(card.lines, "\n"))
					label := "7897 ❯"
					if name == "settings" && strings.Count(plain, label) != 1 {
						t.Fatalf("saved mixed port must occupy one summary row: %s", plain)
					}
					for _, hidden := range []string{"18080", "18888", "private-user", "private-password", "private-path", "private-token", "private-fragment"} {
						if strings.Contains(plain, hidden) {
							t.Fatalf("summary exposed a draft or credential %q: %s", hidden, plain)
						}
					}
					if name == "overview" && (strings.Contains(plain, "7897") || hasAction(card.targets, "runtime-settings")) {
						t.Fatal("overview exposed a standalone port setting")
					}
					if name == "settings" && (!hasAction(card.targets, "runtime-settings") || !hasAction(card.targets, "settings.externalControl") || strings.Contains(plain, "9097")) {
						t.Fatal("settings lost its dedicated modal entries or exposed the control address")
					}
					assertCanvas(t, strings.Join(card.lines, "\n"), width, len(card.lines))
					assertRegions(t, card.targets, width, len(card.lines))
				})
			}
		}
	}
}

func TestProxyPortSummaryCountsEnabledListeners(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.Chinese), Snapshot: settingsTestSnapshot()}
	for _, tc := range []struct {
		mixed, socks, http int
		summary            string
	}{
		{0, 0, 0, "已停用 ❯"}, {-1, 0, 65536, "已停用 ❯"}, {7897, 0, 0, "7897 ❯"}, {0, 8080, 0, "8080 ❯"}, {7897, 8080, 8081, "3 项启用 ❯"},
	} {
		settings := &o.Snapshot.RuntimeSettings
		settings.MixedPort, settings.SOCKSPort, settings.HTTPPort = tc.mixed, tc.socks, tc.http
		card := settingsMihomo(o, 60)
		if !strings.Contains(settingsPaintedTarget(t, strings.Join(card.lines, "\n"), card.targets, "runtime-settings"), tc.summary) {
			t.Fatalf("missing %q", tc.summary)
		}
	}
}
