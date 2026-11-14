package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type proxyPageBackend struct {
	*fakeBackend
	snapshots, controls int
}

func (b *proxyPageBackend) Snapshot(ctx context.Context) (app.Snapshot, error) {
	b.snapshots++
	return b.fakeBackend.Snapshot(ctx)
}

func (b *proxyPageBackend) Control(context.Context, string) error {
	b.controls++
	return nil
}

func (b *proxyPageBackend) Apply(_ context.Context, id string) error {
	b.snapshot.Applied, b.snapshot.Generation = id, "next-generation"
	b.snapshot.Groups = []mihomo.Group{{Name: "NEW", Type: "Selector", Now: "new-node", All: []string{"new-node"}}}
	b.snapshot.Proxies = map[string]mihomo.Proxy{"new-node": {Name: "new-node", Type: "Shadowsocks"}}
	return nil
}

func TestProxiesHeaderOmitsGroupTotalAndManualRefresh(t *testing.T) {
	for _, lang := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{20, 7}, {40, 16}, {80, 24}, {120, 32}, {216, 55}} {
			t.Run(fmt.Sprintf("%s/%dx%d", lang, size[0], size[1]), func(t *testing.T) {
				m := headerModel("proxies", size[0], size[1])
				m.locale = i18n.New(lang)
				m.layout()
				if header := m.pageHeader(); header.summary != "" || header.owns("page.refresh") {
					t.Fatalf("proxy header retained a count or manual refresh action: %+v", header)
				}
				if hasAction(m.targets, "page.refresh") || hasAction(m.focusTargets(), "page.refresh") {
					t.Fatal("manual refresh retained a mouse target or Tab stop")
				}
				if strings.Contains(strings.Join(m.helpLines(200), "\n"), m.tr("help.core.refresh")) || strings.Contains(m.contextHelp(), "r Refresh") {
					t.Fatal("proxy help retained manual refresh instructions")
				}
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
			})
		}
	}
}

func TestProxiesRetiredRefreshKeysCannotReadSnapshotOrRestart(t *testing.T) {
	for _, focus := range []string{"group.0", "content", "nav.proxies", "menu"} {
		t.Run(focus, func(t *testing.T) {
			m := headerModel("proxies", 120, 32)
			b := &proxyPageBackend{fakeBackend: &fakeBackend{snapshot: m.snapshot}}
			m.backend, m.focus = b, focus
			id := m.id
			for _, key := range []tea.KeyPressMsg{{Code: 'r', Text: "r"}, {Code: 'g', Text: "g"}, {Code: 'r', Mod: tea.ModCtrl}} {
				m, _ = update(m, key)
				if m.busy || m.id != id || m.job != "" {
					t.Fatalf("%s started a removed refresh or a restart", key.String())
				}
			}
			if cmd := m.activate("page.refresh"); cmd != nil || m.busy || b.snapshots != 0 || b.controls != 0 {
				t.Fatal("stale refresh action remained callable on the proxy page")
			}
			m.stopRuntime()
		})
	}
}

func TestApplyingProfileAutomaticallyUpdatesProxyGroups(t *testing.T) {
	m, original := profileClickModel(t, true, true, false, false)
	b := &proxyPageBackend{fakeBackend: original.fakeBackend}
	m.backend = b
	cmd := m.activate("profiles.apply.1")
	if !m.busy || cmd == nil {
		t.Fatal("profile apply did not start")
	}
	m = finishProfileJob(t, m, cmd)
	m.navigate("proxies")
	defer m.stopRuntime()
	if b.snapshots != 1 || m.snapshot.Applied != "p-stable-profile-id" || len(m.snapshot.Groups) != 1 || m.snapshot.Groups[0].Name != "NEW" {
		t.Fatalf("apply did not automatically read the new proxy groups: reads=%d, snapshot=%+v", b.snapshots, m.snapshot)
	}
	if b.controls != 0 || !m.snapshot.Service.Active {
		t.Fatal("reading the new proxy groups changed service lifecycle")
	}
	if _, ok := m.snapshot.Proxies["new-node"]; !ok {
		t.Fatal("new proxy metadata was not adopted with the groups")
	}
}

func TestProxiesSearchStartsCollapsedAndOpensOnDemand(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	if hasAction(m.targets, "proxies.search") || m.proxies.editing {
		t.Fatal("default proxy directory should not reserve a search input")
	}
	m, _ = click(m, findTarget(t, m, "proxies.search-open"))
	if _, id, active := m.activeInputField(); !active || id != "proxies.search" || !hasAction(m.targets, id) || m.busy {
		t.Fatal("header search action did not open an editable input")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if hasAction(m.targets, "proxies.search") || m.proxies.editing {
		t.Fatal("empty search should close after Escape")
	}
	m.stopRuntime()
}

func TestProxiesSearchActionsFitShortSingleRowHeader(t *testing.T) {
	for _, width := range []int{100, 101, 110, 120} {
		m := headerModel("proxies", width, 20)
		g := dashboardGeometry(m.dashboardOptions())
		for _, id := range []string{"proxies.search-open", "proxies.providers"} {
			hit := findTarget(t, m, id)
			if hit.Y >= g.headerHeight || hit.X+hit.W > g.headerX+g.headerWidth {
				t.Fatalf("%d columns: header action escaped its allocated row: %+v header=%+v", width, hit, g)
			}
			row := strings.Split(m.screen, "\n")[hit.Y]
			if strings.TrimSpace(ansi.Strip(ansi.Cut(row, hit.X, hit.X+hit.W))) == "" {
				t.Fatalf("%d columns: header action was overwritten by body or divider: %+v", width, hit)
			}
		}
	}
}

func TestProxyProvidersHeaderKeepsModeOrderAcrossSizes(t *testing.T) {
	for _, lang := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{20, 7}, {40, 16}, {50, 18}, {60, 20}, {80, 24}, {100, 20}, {120, 32}, {216, 55}} {
			t.Run(fmt.Sprintf("%s/%dx%d", lang, size[0], size[1]), func(t *testing.T) {
				m := headerModel("proxies", size[0], size[1])
				m.locale = i18n.New(lang)
				m.snapshot.Mode = app.ModeStatus{Desired: "rule", Actual: "rule"}
				m.layout()
				provider := findTarget(t, m, "proxies.providers")
				g := dashboardGeometry(m.dashboardOptions())
				if provider.Y >= g.headerHeight {
					t.Fatal("provider button escaped the fixed header")
				}
				if size[1] < 13 {
					for _, mode := range []string{"rule", "global", "direct"} {
						if !hasAction(m.contentTargets(), "mode."+mode) {
							t.Fatal("short terminal lost an offscreen mode control")
						}
					}
					assertCanvas(t, m.screen, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
					return
				}
				previous := hitRegion{}
				for _, mode := range []string{"rule", "global", "direct"} {
					target := findTarget(t, m, "mode."+mode)
					if previous.ID != "" && (target.Y < previous.Y || target.Y == previous.Y && target.X < previous.X+previous.W) {
						t.Fatalf("mode choices overlap or changed order: %+v %+v", previous, target)
					}
					if size[0] >= 50 && (target.Y != provider.Y || target.X+target.W > provider.X) {
						t.Fatalf("mode should precede providers in the same header row: mode=%+v providers=%+v", target, provider)
					}
					previous = target
				}
				if size[0] >= 50 {
					if provider.X+provider.W != g.headerX+g.headerWidth {
						t.Fatal("provider action is not aligned to the header's right edge")
					}
					search := findTarget(t, m, "proxies.search-open")
					if search.Y != provider.Y || search.X+search.W > provider.X {
						t.Fatal("search action should precede providers without overlap")
					}
					if gap := search.X - previous.X - previous.W; gap != 4 {
						t.Fatalf("mode choices should sit beside search with a four-cell gap, got %d", gap)
					}
				}
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
			})
		}
	}
}

func TestProxiesContentFocusDoesNotPaintOrphanMarker(t *testing.T) {
	for _, mode := range []string{"rule", "global", "direct"} {
		for _, size := range [][2]int{{40, 16}, {80, 24}, {120, 32}, {216, 55}} {
			t.Run(fmt.Sprintf("%s/%dx%d", mode, size[0], size[1]), func(t *testing.T) {
				m := headerModel("proxies", size[0], size[1])
				m.snapshot.Mode = app.ModeStatus{Desired: mode, Actual: mode}
				m.focus = "content"
				m.layout()
				g := dashboardGeometry(m.dashboardOptions())
				line := strings.Split(m.screen, "\n")[g.y]
				if ansi.Strip(ansi.Cut(line, max(0, g.x-1), g.x)) == "›" {
					t.Fatal("proxy content focus painted a standalone marker above the group list")
				}
				m.focus = "group.0"
				m.layout()
				if mode == "rule" && !strings.Contains(ansi.Strip(m.screen), "› PROXY") {
					t.Fatal("removing the standalone marker also removed the group's keyboard focus")
				}
			})
		}
	}
}
