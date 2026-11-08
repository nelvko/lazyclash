package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
				if header := m.pageHeader(); header.summary != "" || len(header.actions) != 0 {
					t.Fatalf("proxy header retained a count or action: %+v", header)
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
