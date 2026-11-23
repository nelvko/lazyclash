package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestNetworkSummariesShowSavedMixedPortWithoutDraftsOrCredentials(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := Model{locale: i18n.New(language), ports: newPortsState(), snapshot: app.Snapshot{
			Installed: true, MixedPort: 7897,
			Controller: "http://private-user:private-password@127.0.0.1:9097/private-path?token=private-token#private-fragment",
		}}
		m.ports.current = app.RuntimeSettings{MixedPort: 18080, ControllerPort: 19090}
		m.ports.mixed.SetValue("18888")
		for _, width := range []int{40, 100} {
			o := m.dashboardOptions()
			o.Snapshot = m.snapshot
			for name, render := range map[string]func(dashboardOptions, int) dashboardBlock{
				"overview": overviewNetworkCard, "settings": settingsService,
			} {
				t.Run(fmt.Sprintf("%s/%s/%d", language, name, width), func(t *testing.T) {
					card := render(o, width)
					plain := ansi.Strip(strings.Join(card.lines, "\n"))
					label := m.tr("dashboard.network.mixedPort", "7897")
					if strings.Count(plain, label) != 1 {
						t.Fatalf("saved mixed port must occupy one summary row: %s", plain)
					}
					for _, hidden := range []string{"18080", "18888", "private-user", "private-password", "private-path", "private-token", "private-fragment"} {
						if strings.Contains(plain, hidden) {
							t.Fatalf("summary exposed a draft or credential %q: %s", hidden, plain)
						}
					}
					if name == "overview" {
						if hasAction(card.targets, "runtime-settings") || hasAction(card.targets, "card.network") {
							t.Fatal("read-only port summary added an editing action")
						}
						if !strings.HasSuffix(strings.TrimSpace(plain), label) {
							t.Fatal("overview port summary is not the last content row")
						}
					}
					if name == "settings" && (!hasAction(card.targets, "runtime-settings") || !strings.Contains(plain, "127.0.0.1:9097")) {
						t.Fatal("settings lost its existing edit action or sanitized control address")
					}
					assertCanvas(t, strings.Join(card.lines, "\n"), width, len(card.lines))
					assertRegions(t, card.targets, width, len(card.lines))
				})
			}
		}
	}
}

func TestSavedMixedPortSummaryRejectsMissingAndInvalidValues(t *testing.T) {
	for _, test := range []struct {
		installed bool
		port      int
		want      string
	}{{true, 1, "1"}, {true, 65535, "65535"}, {true, 0, "--"}, {true, -1, "--"}, {true, 65536, "--"}, {false, 7897, "--"}} {
		o := dashboardOptions{Locale: i18n.New(i18n.English), Snapshot: app.Snapshot{Installed: test.installed, MixedPort: test.port}}
		if got := savedMixedPortLabel(o); got != o.Locale.T("dashboard.network.mixedPort", test.want) {
			t.Fatalf("installed=%v port=%d: %q", test.installed, test.port, got)
		}
	}
}
