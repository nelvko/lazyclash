package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestCoreManagementCardShowsMaintenanceActions(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		o := dashboardOptions{Locale: i18n.New(language), Height: 45, Snapshot: settingsTestSnapshot(), PlainButtons: true}
		o.Snapshot.Capabilities.Scope = "user"
		o.Snapshot.Service.State, o.Snapshot.Service.Active = "active", true
		tr := o.Locale.T
		for _, width := range []int{36, 68} {
			card := settingsCore(o, width)
			var rows []string
			for _, line := range card.lines {
				if fields := strings.Fields(ansi.Strip(line)); len(fields) > 0 {
					rows = append(rows, strings.Join(fields, " "))
				}
			}
			want := []string{
				tr("settings.core"), tr("wizard.installed_version") + " " + o.Snapshot.Version + " ❯",
				tr("core_update.check") + " ❯", tr("settings.downloadSettings") + " " + tr("wizard.method.direct") + " ❯",
				tr("settings.autostart") + " ██░░░░",
				tr("settings.userService") + " [" + tr("dashboard.action.restart") + "]",
			}
			if !slices.Equal(rows, want) {
				t.Fatalf("%s/%d: unexpected maintenance rows: %q", language, width, rows)
			}
			for _, id := range []string{"install", "core.checkUpdate", "network"} {
				link := findTarget(t, Model{targets: card.targets}, id)
				if link.X != 0 || link.W != width || link.H != 1 {
					t.Fatalf("%s/%d: %s is not a full row: %+v", language, width, id, link)
				}
			}
			restart := findTarget(t, Model{targets: card.targets}, "restart")
			face := ansi.Strip(ansi.Cut(card.lines[restart.Y], restart.X, restart.X+restart.W))
			if restart.X+restart.W != width-2 || !strings.Contains(face, tr("dashboard.action.restart")) {
				t.Fatalf("%s/%d: restart target misses its trailing button: %q", language, width, face)
			}
		}

		o.Snapshot.Service.Linger = false
		o.Snapshot.GeoRestartPending = true
		for _, known := range []bool{true, false} {
			o.Snapshot.Service.LingerKnown = known
			card := settingsCore(o, 120)
			toggle := findTarget(t, Model{targets: card.targets}, autostartToggleTarget)
			hint := "dashboard.settings.lingerDisabled"
			if !known {
				hint = "dashboard.settings.lingerUnknown"
			}
			if !strings.Contains(ansi.Strip(card.lines[toggle.Y+1]), tr(hint)) {
				t.Fatalf("%s: missing %s under autostart", language, hint)
			}
		}
		o.Snapshot.Capabilities.Scope = "system"
		o.Snapshot.Desired.Known, o.Snapshot.Desired.Autostart = true, true
		card := settingsCore(o, 120)
		toggle := findTarget(t, Model{targets: card.targets}, autostartToggleTarget)
		if !strings.Contains(ansi.Strip(card.lines[toggle.Y+1]), tr("diagnostic.snapshot.autostartDrift")) {
			t.Fatalf("%s: missing drift guidance under autostart", language)
		}
		o.Snapshot.Service.EnableState = "unknown"
		if strings.Contains(settingsAutostart(o).note, tr("diagnostic.snapshot.autostartDrift")) {
			t.Fatalf("%s: reported autostart drift without an observed enable state", language)
		}
		restart := findTarget(t, Model{targets: card.targets}, "restart")
		if !strings.Contains(ansi.Strip(card.lines[restart.Y]), tr("settings.systemService")) ||
			!strings.Contains(ansi.Strip(card.lines[restart.Y+1]), tr("settings.geoRestart")) ||
			strings.Contains(ansi.Strip(strings.Join(card.lines, "\n")), tr("dashboard.settings.lingerUnknown")) {
			t.Fatalf("%s: unexpected system service controls or guidance", language)
		}
	}
}

func TestCoreManagementCardUnavailableActions(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English), Snapshot: settingsTestSnapshot(), Busy: true}
	if len(settingsCore(o, 68).targets) != 0 {
		t.Fatal("busy card exposes an actionable control")
	}
	o.Busy, o.Snapshot.Installed = false, false
	card := settingsCore(o, 36)
	if len(card.targets) != 2 || card.targets[0].ID != "install" || card.targets[1].ID != "network" {
		t.Fatal("first install should be followed by download settings")
	}
	o.StateUnavailable = true
	if targets := settingsCore(o, 36).targets; len(targets) != 1 || targets[0].ID != "network" {
		t.Fatal("an unavailable snapshot should only allow editing download settings")
	}
}
