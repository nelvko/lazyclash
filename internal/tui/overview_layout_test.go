package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestOverviewFlowingLayoutKeepsCardGeometryAndActionOwnership(t *testing.T) {
	snapshot := app.Snapshot{
		Installed: true, ConfigReady: true, HasRun: true, Applied: "default",
		Version: "v1.19.33", RunningVersion: "v1.19.33",
		Service:      service.Status{Active: true, State: "active", PID: 456},
		Mode:         app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
		Groups:       []mihomo.Group{{Name: "PROXY", Type: "Selector", Now: "DIRECT"}},
		Capabilities: app.Capabilities{Scope: "system", Runtime: app.Capability{Supported: true}, TUN: app.Capability{Supported: true}},
	}
	owners := map[string]string{
		"core.stop": "dashboard.control.core", "core.restart": "dashboard.control.core", "card.install": "dashboard.control.core",
		"mode.rule": "dashboard.control.routingMode", "mode.global": "dashboard.control.routingMode", "mode.direct": "dashboard.control.routingMode", "card.proxies": "dashboard.control.routingMode",
		"card.profiles": "dashboard.profile.title", "tun.enable": "dashboard.system.title",
	}
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{16, 40, 72, 89, 90, 112, 180} {
			for _, height := range []int{19, 20, 45} {
				t.Run(fmt.Sprintf("%s/%dx%d", language, width, height), func(t *testing.T) {
					o := dashboardOptions{Locale: i18n.New(language), Snapshot: snapshot, Runtime: liveRuntime(), RuntimeReceived: true, Height: height}
					body := overviewLayout(o, width)
					assertCanvas(t, strings.Join(body.lines, "\n"), width, len(body.lines))
					assertRegions(t, body.targets, width, len(body.lines))
					assertRegions(t, body.cards, width, len(body.lines))
					cards := make(map[string]hitRegion)
					for index, card := range body.cards {
						if _, found := cards[card.ID]; found {
							t.Fatalf("duplicate card %s", card.ID)
						}
						cards[card.ID] = card
						for _, other := range body.cards[index+1:] {
							if regionsOverlap(card, other) {
								t.Fatalf("card frames overlap: %+v / %+v", card, other)
							}
						}
						if cell := ansi.Strip(ansi.Cut(body.lines[card.Y+card.H-1], card.X, card.X+card.W)); !strings.HasPrefix(cell, "└") || !strings.HasSuffix(cell, "┘") {
							t.Fatalf("semantic region does not match the natural card frame: %+v / %q", card, cell)
						}
					}
					if len(cards) != 5 {
						t.Fatalf("got %d cards, want five", len(cards))
					}
					core := cards["overview.card.dashboard.control.core"]
					route := cards["overview.card.dashboard.control.routingMode"]
					traffic := cards["overview.card.dashboard.traffic.title"]
					profile := cards["overview.card.dashboard.profile.title"]
					if width >= 90 && height >= 20 {
						if core.W <= traffic.W || core.Y != traffic.Y || route.Y != core.Y+core.H+1 || profile.Y != traffic.Y+traffic.H+1 {
							t.Fatalf("cards lost asymmetric widths or independent flow: core=%+v route=%+v traffic=%+v profile=%+v", core, route, traffic, profile)
						}
					} else if core.X != route.X || core.W != width || route.Y != core.Y+core.H+1 || traffic.Y != route.Y+route.H+1 || profile.Y != traffic.Y+traffic.H+1 {
						t.Fatal("compact layout did not preserve content order and natural spacing")
					}
					for id, owner := range owners {
						found := false
						for _, target := range body.targets {
							if target.ID != id {
								continue
							}
							found = true
							card := cards["overview.card."+owner]
							if clipped, ok := clipRegion(target, card.X, card.Y, card.W, card.H); !ok || clipped != target {
								t.Fatalf("action %s left its semantic card: %+v / %+v", id, target, card)
							}
							if strings.TrimSpace(ansi.Strip(ansi.Cut(body.lines[target.Y], target.X, target.X+target.W))) == "" {
								t.Fatalf("action %s has no rendered label", id)
							}
						}
						if !found {
							t.Fatalf("missing action %s", id)
						}
					}
				})
			}
		}
	}
}

func TestOverviewAlignedVersionsAndModesCollapseOnlyConfirmedState(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			locale := i18n.New(language)
			o := dashboardOptions{Locale: locale, Snapshot: app.Snapshot{
				Installed: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
				Service: service.Status{Active: true, State: "active", PID: 456},
				Desired: app.DesiredState{Known: true, Running: true},
				Mode:    app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
			}}
			core := func() string { return ansi.Strip(strings.Join(overviewCoreCard(o, 100).lines, "\n")) }
			if value := core(); strings.Count(value, "v1.19.33") != 1 || !strings.Contains(value, locale.T("dashboard.overview.versionAligned", "v1.19.33")) || !strings.Contains(value, locale.T("dashboard.kernel.pid", "456")) || !strings.Contains(value, locale.T("dashboard.kernel.desired", locale.T("dashboard.status.running"))) {
				t.Fatalf("merged card duplicated aligned versions or hid process/intent: %s", value)
			}
			o.Snapshot.Version = "v1.20.0"
			if value := core(); !strings.Contains(value, locale.T("dashboard.kernel.installed", "v1.20.0")) || !strings.Contains(value, locale.T("dashboard.kernel.running", "v1.19.33")) {
				t.Fatalf("version mismatch lost explicit installed/running distinction: %s", value)
			}
			mode := func() string { return ansi.Strip(strings.Join(overviewModeCard(o, 100).lines, "\n")) }
			rule := locale.T("dashboard.mode.rule")
			if value := mode(); !strings.Contains(value, locale.T("dashboard.overview.modeAligned", rule)) || strings.Contains(value, locale.T("dashboard.control.savedMode", rule)) || strings.Contains(value, locale.T("dashboard.control.runningMode", rule)) {
				t.Fatalf("confirmed aligned mode retained duplicate state lines: %s", value)
			}
			o.Snapshot.Mode.Actual = "global"
			if value := mode(); !strings.Contains(value, locale.T("dashboard.control.savedMode", rule)) || !strings.Contains(value, locale.T("dashboard.control.runningMode", locale.T("dashboard.mode.global"))) {
				t.Fatalf("mode mismatch lost explicit saved/running distinction: %s", value)
			}
			o.Snapshot.Mode.Actual = ""
			if value := mode(); !strings.Contains(value, locale.T("dashboard.control.runningMode", locale.T("dashboard.status.unknown"))) || strings.Contains(value, locale.T("dashboard.overview.modeAligned", rule)) {
				t.Fatalf("unobserved mode appeared aligned: %s", value)
			}
			for _, unknown := range []string{"", "unrecognized"} {
				o.Snapshot.Mode.Desired, o.Snapshot.Mode.Actual = unknown, unknown
				if value := mode(); !strings.Contains(value, locale.T("dashboard.control.runningMode", locale.T("dashboard.status.unknown"))) || strings.Contains(value, locale.T("dashboard.overview.modeAligned", locale.T("dashboard.status.unknown"))) {
					t.Fatalf("matching unknown modes appeared confirmed: %s", value)
				}
			}
		})
	}
}
