package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestCoreManagementCardGroupsAndActions(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		o := dashboardOptions{Locale: i18n.New(language), Height: 45, Snapshot: settingsTestSnapshot(), PlainButtons: true}
		o.Snapshot.Capabilities.Scope = "user"
		o.Snapshot.Service.State, o.Snapshot.Service.Active = "active", true
		tr := o.Locale.T
		for _, width := range []int{36, 68} {
			card := settingsCore(o, width)
			for _, id := range []string{"install", "core.checkUpdate"} {
				link := findTarget(t, Model{targets: card.targets}, id)
				if link.X != 0 || link.W != width || link.H != 1 {
					t.Fatalf("%s/%d: %s is not a full row: %+v", language, width, id, link)
				}
			}
			hit := findTarget(t, Model{targets: card.targets}, "restart")
			line := ansi.Strip(card.lines[hit.Y])
			status, _, _ := headerCoreState(o)
			if !strings.Contains(line, tr("settings.serviceStatus")) ||
				!strings.HasSuffix(strings.TrimRight(ansi.Cut(line, 0, hit.X), " "), status) ||
				hit.X+hit.W != width-2 || !strings.Contains(ansi.Cut(line, hit.X, hit.X+hit.W), tr("dashboard.action.restart")) {
				t.Fatalf("%s/%d: service status and restart action misaligned: %q", language, width, line)
			}
			body := ansi.Strip(strings.Join(card.lines, "\n"))
			for _, label := range []string{"Mihomo", tr("settings.userService"), tr("dashboard.overview.linger")} {
				if !strings.Contains(body, label) {
					t.Fatalf("%s/%d: missing %q", language, width, label)
				}
			}
			if strings.Contains(body, "lazyclash uninstall") {
				t.Fatalf("%s/%d: uninstall instructions still occupy the card", language, width)
			}
		}

		o.Snapshot.Capabilities.Scope = "system"
		o.Snapshot.GeoRestartPending = true
		card := settingsCore(o, 68)
		restart := findTarget(t, Model{targets: card.targets}, "restart")
		body := ansi.Strip(strings.Join(card.lines, "\n"))
		if !strings.Contains(ansi.Strip(card.lines[restart.Y+1]), tr("settings.geoRestart")) ||
			!strings.Contains(body, tr("settings.systemService")) || strings.Contains(body, tr("dashboard.overview.linger")) {
			t.Fatalf("%s: unexpected system service section:\n%s", language, body)
		}
		o.Busy = true
		if len(settingsCore(o, 68).targets) != 0 {
			t.Fatal("busy card exposes an actionable control")
		}
		o.Busy, o.Snapshot.Installed = false, false
		card = settingsCore(o, 36)
		if len(card.targets) != 1 || card.targets[0].ID != "install" {
			t.Fatal("first install should be the only action before installation")
		}
		o.StateUnavailable = true
		if len(settingsCore(o, 36).targets) != 0 {
			t.Fatal("unavailable snapshot offered an installation")
		}
	}
}
