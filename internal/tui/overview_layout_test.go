package tui

import (
	"fmt"
	"runtime"
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

func TestOverviewPairedLayoutAlignsBothEdgesAndKeepsActionOwnership(t *testing.T) {
	snapshot := app.Snapshot{
		Installed: true, ConfigReady: true, HasRun: true, Applied: "default",
		Version: "v1.19.33", RunningVersion: "v1.19.33",
		Service:      service.Status{Active: true, State: "active", PID: 456},
		Mode:         app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
		Groups:       []mihomo.Group{{Name: "PROXY", Type: "Selector", Now: "DIRECT"}},
		Capabilities: app.Capabilities{Scope: "system", Runtime: app.Capability{Supported: true}, TUN: app.Capability{Supported: true}},
	}
	owners := map[string]string{
		"core.toggle": "dashboard.control.core",
		"mode.rule":   "dashboard.control.routingMode", "mode.global": "dashboard.control.routingMode", "mode.direct": "dashboard.control.routingMode", "card.proxies": "dashboard.control.routingMode",
		"card.profiles": "dashboard.profile.title", "tun.enable": "dashboard.overview.network", "card.network": "dashboard.overview.network",
	}
	renders := map[string]func(dashboardOptions, int) dashboardBlock{
		"dashboard.control.core": overviewCoreCard, "dashboard.traffic.title": overviewTrafficCard,
		"dashboard.control.routingMode": overviewModeCard, "dashboard.profile.title": overviewProfileCard,
		"dashboard.overview.network": overviewNetworkCard, "dashboard.system.title": overviewSystemCard,
	}
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{16, 40, 72, 89, 90, 91, 112, 180} {
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
						render := renders[strings.TrimPrefix(card.ID, "overview.card.")]
						if render == nil || card.H < len(render(o, card.W).lines) {
							t.Fatalf("card %s clipped its required contents", card.ID)
						}
						for _, other := range body.cards[index+1:] {
							if regionsOverlap(card, other) {
								t.Fatalf("card frames overlap: %+v / %+v", card, other)
							}
						}
						for row := 0; row < card.H; row++ {
							cells := ansi.Strip(ansi.Cut(body.lines[card.Y+row], card.X, card.X+card.W))
							if !strings.HasPrefix(cells, " ") || !strings.HasSuffix(cells, " ") || (row == 0 || row == card.H-1) && strings.TrimSpace(cells) != "" {
								t.Fatalf("normal card perimeter is not blank: %+v row %d / %q", card, row, cells)
							}
						}
					}
					if len(cards) != 6 {
						t.Fatalf("got %d cards, want six", len(cards))
					}
					if width >= 90 {
						y := 0
						for first := 0; first < 4; first += 2 {
							left, right := body.cards[first], body.cards[first+1]
							wantHeight := max(len(renders[strings.TrimPrefix(left.ID, "overview.card.")](o, left.W).lines),
								len(renders[strings.TrimPrefix(right.ID, "overview.card.")](o, right.W).lines))
							if left.X != 0 || right.X != left.W+2 || width-left.W-right.W-2 != width%2 || left.W != right.W || left.H != right.H || left.H != wantHeight || left.Y != y || right.Y != y {
								t.Fatalf("paired cards lost equal dimensions or aligned edges: %+v / %+v at y=%d", left, right, y)
							}
							y += max(left.H, right.H) + 1
						}
						traffic, environment := body.cards[4], body.cards[5]
						if traffic.X != 0 || traffic.W != width || traffic.Y != y {
							t.Fatalf("full-width traffic card lost its row: %+v at y=%d", traffic, y)
						}
						if environment.X != body.cards[0].X || environment.W != body.cards[0].W || environment.Y != y+traffic.H+1 {
							t.Fatalf("environment card must occupy one left column after traffic: %+v", environment)
						}
					} else {
						y := 0
						for _, card := range body.cards {
							if card.X != 0 || card.W != width || card.Y != y {
								t.Fatalf("compact layout lost content order or natural spacing: %+v at y=%d", card, y)
							}
							y += card.H + 1
						}
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

func TestOverviewEnvironmentReportsObservedServiceSettings(t *testing.T) {
	for _, tc := range []struct {
		name, scope, serviceError, autostart, linger string
		supported, lingerKnown, lingerEnabled        bool
	}{
		{name: "user enabled", scope: "user", autostart: "disabled", linger: "enabled", supported: true, lingerKnown: true, lingerEnabled: true},
		{name: "user disabled", scope: "user", autostart: "disabled", linger: "disabled", supported: true, lingerKnown: true},
		{name: "user unknown", scope: "user", autostart: "disabled", linger: "unknown", supported: true},
		{name: "service error", scope: "user", serviceError: "service observation failed", autostart: "unknown", linger: "unknown", supported: true, lingerKnown: true, lingerEnabled: true},
		{name: "system", scope: "system", autostart: "disabled", supported: true},
		{name: "unsupported", scope: "user", autostart: "unknown"},
	} {
		for _, language := range []string{i18n.English, i18n.Chinese} {
			t.Run(tc.name+"/"+language, func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), Snapshot: app.Snapshot{
					Capabilities: app.Capabilities{Scope: tc.scope, Runtime: app.Capability{Supported: tc.supported}},
					Service:      service.Status{EnableState: "disabled", LingerKnown: tc.lingerKnown, Linger: tc.lingerEnabled},
					ServiceError: tc.serviceError,
				}}
				card := overviewSystemCard(o, 38)
				if len(card.targets) != 0 {
					t.Fatal("environment card must be informational")
				}
				tr := o.Locale.T
				rows := make([]string, len(card.lines))
				for i, line := range card.lines {
					rows[i] = strings.TrimSpace(ansi.Strip(line))
				}
				check := func(label, value string) {
					t.Helper()
					i := slices.Index(rows, label)
					if i < 0 || i+1 >= len(rows) || rows[i+1] != value {
						t.Fatalf("expected %q followed by %q, got %q", label, value, rows)
					}
				}
				check(tr("dashboard.field.platform"), runtime.GOOS+" / "+runtime.GOARCH)
				manager := tr("dashboard.system.manager")
				if tc.scope == "system" {
					manager = tr("dashboard.system.systemManager")
				} else if !tc.supported {
					manager = tr("dashboard.system.user") + " · " + tr("dashboard.status.unavailable")
				}
				check(tr("dashboard.overview.serviceLabel"), manager)
				check(tr("settings.autostart"), tr("dashboard.status."+tc.autostart))
				label := tr("dashboard.overview.linger")
				if tc.linger != "" {
					check(label, tr("dashboard.status."+tc.linger))
				} else if slices.Contains(rows, label) {
					t.Fatal("showed user linger where it does not apply")
				}
			})
		}
	}
}

func TestOverviewOutlineFollowsCardFocusWithoutMovingContents(t *testing.T) {
	for _, size := range []struct{ width, height, scroll int }{{160, 45, 0}, {80, 24, 0}, {40, 16, 0}, {80, 12, 1}} {
		t.Run(fmt.Sprintf("%dx%d/scroll%d", size.width, size.height, size.scroll), func(t *testing.T) {
			m := themeSample("overview-running")
			m.width, m.height, m.scroll = size.width, size.height, size.scroll
			m.focus = "nav.overview"
			m.layout()
			o := m.navigationOptions()
			baseline := overviewLayout(o, dashboardBodyWidth(o))
			assertOverviewCardPerimeters(t, m, "")
			if size.scroll > 0 && m.scroll != size.scroll {
				t.Fatal("fixture did not leave the core card partially scrolled")
			}
			assertGeometry := func() {
				t.Helper()
				o := m.navigationOptions()
				current := overviewLayout(o, dashboardBodyWidth(o))
				if !slices.Equal(baseline.cards, current.cards) || !slices.Equal(baseline.targets, current.targets) {
					t.Fatal("card focus changed semantic card or action geometry")
				}
			}
			m.focus = "overview.card.dashboard.control.core"
			m.layout()
			assertOverviewCardPerimeters(t, m, m.focus)
			assertGeometry()
			m, _ = update(m, special(tea.KeyEnter))
			if m.focus != "core.toggle" || m.busy {
				t.Fatal("entering the core controls did not safely focus Stop")
			}
			assertOverviewCardPerimeters(t, m, "")
			assertGeometry()
			m.returnToNavigation()
			m.layout()
			assertOverviewCardPerimeters(t, m, "")
			assertGeometry()
		})
	}
}

func assertOverviewCardPerimeters(t *testing.T, m Model, focused string) {
	t.Helper()
	g := dashboardGeometry(m.navigationOptions())
	rows := strings.Split(m.screen, "\n")
	checked := 0
	for _, card := range overviewCardRegionsFromModel(m) {
		for row := 0; row < card.H; row++ {
			y := g.y + card.Y + row - m.scroll
			if y < g.y || y >= g.y+g.height {
				continue
			}
			x := g.x + card.X
			check := func(x, width int, expected string) {
				t.Helper()
				actual := ansi.Strip(ansi.Cut(rows[y], x, x+width))
				if actual != expected {
					t.Fatalf("card %s perimeter (%d,%d): got %q, want %q", card.ID, x, y, actual, expected)
				}
				checked++
			}
			if row == 0 || row == card.H-1 {
				border := strings.Repeat(" ", card.W)
				if card.ID == focused {
					left, right := "┌", "┐"
					if row == card.H-1 {
						left, right = "└", "┘"
					}
					border = left + strings.Repeat("─", card.W-2) + right
				}
				check(x, card.W, border)
			} else {
				border := " "
				if card.ID == focused {
					border = "│"
				}
				check(x, 1, border)
				check(x+card.W-1, 1, border)
			}
		}
	}
	if checked == 0 {
		t.Fatal("fixture did not expose any card perimeter")
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
			if value := core(); strings.Count(value, "v1.19.33") != 1 || strings.Contains(value, "456") || strings.Contains(value, locale.T("dashboard.kernel.desired", locale.T("dashboard.status.running"))) {
				t.Fatalf("normal core card duplicated versions or retained diagnostic fields: %s", value)
			}
			o.Snapshot.Version = "v1.20.0"
			if value := core(); !strings.Contains(value, locale.T("dashboard.kernel.installed", "v1.20.0")) || strings.Count(value, "v1.19.33") != 1 {
				t.Fatalf("version mismatch lost explicit installed/running distinction: %s", value)
			}
			mode := func() string { return ansi.Strip(strings.Join(overviewModeCard(o, 100).lines, "\n")) }
			rule := locale.T("dashboard.mode.rule")
			if value := mode(); !strings.Contains(value, locale.T("dashboard.overview.modeRule")) || strings.Contains(value, locale.T("dashboard.control.savedMode", rule)) || strings.Contains(value, locale.T("dashboard.control.runningMode", rule)) {
				t.Fatalf("confirmed aligned mode retained duplicate state lines: %s", value)
			}
			o.Snapshot.Mode.Actual = "global"
			if value := mode(); !strings.Contains(value, locale.T("dashboard.control.savedMode", rule)) || !strings.Contains(value, locale.T("dashboard.control.runningMode", locale.T("dashboard.mode.global"))) {
				t.Fatalf("mode mismatch lost explicit saved/running distinction: %s", value)
			}
			o.Snapshot.Mode.Actual = ""
			if value := mode(); !strings.Contains(value, locale.T("dashboard.control.runningMode", locale.T("dashboard.status.unknown"))) || strings.Contains(value, locale.T("dashboard.overview.modeRule")) {
				t.Fatalf("unobserved mode appeared aligned: %s", value)
			}
			for _, unknown := range []string{"", "unrecognized"} {
				o.Snapshot.Mode.Desired, o.Snapshot.Mode.Actual = unknown, unknown
				if value := mode(); !strings.Contains(value, locale.T("dashboard.control.runningMode", locale.T("dashboard.status.unknown"))) || strings.Contains(value, locale.T("dashboard.overview.modeRule")) {
					t.Fatalf("matching unknown modes appeared confirmed: %s", value)
				}
			}
		})
	}
}

func TestOverviewProfileKeepsSafeMetadataAndUnappliedChangesExplicit(t *testing.T) {
	updated := time.Now().AddDate(0, 0, -40)
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			locale := i18n.New(language)
			o := dashboardOptions{Locale: locale, Snapshot: app.Snapshot{
				Installed: true, ConfigReady: true, Applied: "work", Profiles: []string{"default", "work"},
				ProfileDetails: []app.ProfileInfo{{ID: "work", Name: "Work", Kind: "remote", Origin: "subscription.example.invalid", UpdatedAt: updated, Validated: true, HasOverrides: true, Description: "hidden-profile-description"}},
			}}
			card := func() string { return ansi.Strip(strings.Join(overviewProfileCard(o, 100).lines, "\n")) }
			if value := card(); !strings.Contains(value, "Work") || !strings.Contains(value, "subscription.example.invalid") || !strings.Contains(value, updated.Format("2006-01-02")) || strings.Contains(value, "hidden-profile-description") {
				t.Fatalf("overview lost useful safe profile metadata or exposed its description: %s", value)
			}
			for _, obsolete := range []string{"Includes overrides", "包含覆写配置", "1 saved profiles", "已保存 1 份配置"} {
				if strings.Contains(card(), obsolete) {
					t.Fatalf("overview retained detail-only profile metadata %q", obsolete)
				}
			}
			for _, fromSnapshot := range []bool{true, false} {
				o.Snapshot.ProfilePending = fromSnapshot
				o.Snapshot.ProfileDetails[0].Pending = !fromSnapshot
				if value := card(); !strings.Contains(value, locale.T("profiles.changes_pending")) || strings.Contains(value, locale.T("dashboard.profile.ready")) {
					t.Fatalf("unapplied profile changes appeared ready: %s", value)
				}
			}
			o.Snapshot.ProfilePending, o.Snapshot.ProfileDetails[0].Pending = false, false
			o.Snapshot.ProfileDetails[0].Validated = false
			if value := card(); !strings.Contains(value, locale.T("profiles.validation_pending")) || strings.Contains(value, locale.T("dashboard.profile.ready")) {
				t.Fatalf("unvalidated profile appeared ready: %s", value)
			}
			for kind, origin := range map[string]string{"local": locale.T("profiles.create_local"), "imported": locale.T("profiles.imported_origin"), "builtin": locale.T("profiles.builtin_origin")} {
				o.Snapshot.ProfileDetails[0].Kind = kind
				o.Snapshot.ProfileDetails[0].Origin = "/private/profile-source.yaml"
				if value := card(); !strings.Contains(value, origin) || strings.Contains(value, "/private/profile-source.yaml") {
					t.Fatalf("profile kind %s lost its safe source label: %s", kind, value)
				}
			}
		})
	}
}

func TestOverviewNetworkMovesAddressesToDetailsAndKeepsControllerMetadataSafe(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			locale := i18n.New(language)
			o := dashboardOptions{Locale: locale, Snapshot: app.Snapshot{
				Installed: true, MixedPort: 17890, Controller: "http://private-user:private-password@127.0.0.1:19090/private-path?token=private-token#private-fragment",
			}}
			card := func() string { return ansi.Strip(strings.Join(overviewNetworkCard(o, 100).lines, "\n")) }
			if value := card(); strings.Contains(value, "127.0.0.1:") || !hasAction(overviewNetworkCard(o, 100).targets, "card.network") {
				t.Fatalf("network overview retained detail addresses or lost its settings entry: %s", value)
			}
			for _, secret := range []string{"private-user", "private-password", "private-path", "private-token", "private-fragment"} {
				if strings.Contains(card(), secret) {
					t.Fatalf("controller metadata leaked %s", secret)
				}
			}
			for _, invalid := range []int{0, -1, 65536} {
				o.Snapshot.MixedPort = invalid
				if value := card(); strings.Contains(value, "127.0.0.1:") {
					t.Fatalf("network summary exposed a default, stale or detail-only address: %s", value)
				}
			}
			o.Snapshot.Installed, o.Snapshot.MixedPort = false, 17890
			if value := card(); strings.Contains(value, "127.0.0.1:") || len(overviewNetworkCard(o, 100).targets) != 0 {
				t.Fatal("uninstalled network card exposed stale addresses or actions")
			}
		})
	}
	for _, sample := range []struct{ input, want string }{
		{"127.0.0.1:19090", "127.0.0.1:19090"},
		{"http://user:password@[::1]:19090/path?token=secret", "[::1]:19090"},
		{"127.0.0.1:not-a-port", "--"}, {"127.0.0.1:0", "--"}, {"127.0.0.1:65536", "--"}, {"", "--"},
	} {
		if got := overviewControllerAddress(sample.input); got != sample.want {
			t.Fatalf("controller display %q: got %q, want %q", sample.input, got, sample.want)
		}
	}
}

func TestOverviewNetworkSeparatesSavedAndObservedHostTUN(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			locale := i18n.New(language)
			actual := false
			o := dashboardOptions{Locale: locale, Snapshot: app.Snapshot{
				Installed: true, TUN: true, TUNActual: &actual, TUNError: "Host TUN differs from its saved policy",
				Service:      service.Status{Active: true, State: "active"},
				Capabilities: app.Capabilities{Scope: "system", Runtime: app.Capability{Supported: true}, TUN: app.Capability{Supported: true}},
			}}
			card := func() string { return ansi.Strip(strings.Join(overviewNetworkCard(o, 100).lines, "\n")) }
			if value := card(); !strings.Contains(value, locale.T("tun.saved_state", locale.T("dashboard.control.on"))) || !strings.Contains(value, locale.T("dashboard.overview.tunActual", locale.T("dashboard.control.off"))) || !strings.Contains(value, locale.T("tun.host_scope")) || !strings.Contains(value, o.Snapshot.TUNError) || !hasAction(overviewNetworkCard(o, 100).targets, "tun.disable") {
				t.Fatalf("host TUN mismatch lost saved/observed distinction, scope or recovery action: %s", value)
			}
			for _, missing := range []string{"no observation", "service error", "API error"} {
				o.Snapshot.TUNActual, o.Snapshot.ServiceError, o.Snapshot.APIError = &actual, "", ""
				switch missing {
				case "no observation":
					o.Snapshot.TUNActual = nil
				case "service error":
					o.Snapshot.ServiceError = "Service unavailable"
				case "API error":
					o.Snapshot.APIError = "Controller unavailable"
				}
				if value := card(); !strings.Contains(value, locale.T("dashboard.overview.tunActual", locale.T("dashboard.status.unknown"))) || strings.Contains(value, locale.T("dashboard.overview.tunActual", locale.T("dashboard.control.on"))) || strings.Contains(value, locale.T("dashboard.overview.tunActual", locale.T("dashboard.control.off"))) {
					t.Fatalf("%s presented saved or stale TUN as observed: %s", missing, value)
				}
			}
			o.Busy = true
			if len(overviewNetworkCard(o, 100).targets) != 0 {
				t.Fatal("busy network card retained an actionable control")
			}
		})
	}
}

func TestOverviewModeUsesActualModeAndDoesNotExposeStaleNodeSelection(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, state := range []struct {
			name, desired, actual, serviceError, apiError, modeError, want string
			missingGlobal, unsupported                                     bool
		}{
			{name: "rule", desired: "rule", actual: "rule", want: "proxy-node"},
			{name: "global ignores stale group selection", desired: "global", actual: "global", want: "global-node"},
			{name: "saved direct does not hide actual rule", desired: "direct", actual: "rule", want: "proxy-node"},
			{name: "actual direct hides old node", desired: "rule", actual: "direct"},
			{name: "missing global does not use rule proxy", desired: "global", actual: "global", missingGlobal: true},
			{name: "unknown mode", desired: "rule"},
			{name: "service unavailable", desired: "rule", actual: "rule", serviceError: "fixture service unavailable"},
			{name: "API unavailable", desired: "rule", actual: "rule", apiError: "fixture API unavailable"},
			{name: "mode unavailable", desired: "rule", actual: "rule", modeError: "fixture mode unavailable"},
			{name: "readable mode with unavailable runtime controls", desired: "rule", actual: "rule", unsupported: true, want: "proxy-node"},
		} {
			t.Run(language+"/"+state.name, func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), SelectedGroup: "PROXY", Snapshot: app.Snapshot{
					Installed: true, Service: service.Status{Active: true, State: "active"},
					Mode:         app.ModeStatus{Desired: state.desired, Actual: state.actual, Running: true, Error: state.modeError},
					ServiceError: state.serviceError, APIError: state.apiError,
					Groups:       []mihomo.Group{{Name: "PROXY", Type: "Selector", Now: "proxy-node"}, {Name: "GLOBAL", Type: "Selector", Now: "global-node"}},
					Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: !state.unsupported}},
				}}
				if state.missingGlobal {
					o.Snapshot.Groups = o.Snapshot.Groups[:1]
				}
				block := overviewModeCard(o, 72)
				plain := ansi.Strip(strings.Join(block.lines, "\n"))
				for _, node := range []string{"proxy-node", "global-node"} {
					if strings.Contains(plain, node) != (node == state.want) {
						t.Fatalf("mode card exposed or hid an unrelated node %q:\n%s", node, plain)
					}
				}
				if state.actual == "direct" && !strings.Contains(plain, o.Locale.T("dashboard.overview.modeDirect")) {
					t.Fatal("confirmed Direct lost its mode explanation")
				}
				if state.unsupported && (!strings.Contains(plain, o.Locale.T("dashboard.overview.modeRule")) || hasAction(block.targets, "mode.global")) {
					t.Fatal("control availability was confused with a valid observed mode")
				}
			})
		}
	}
}
