package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestGeoMaintenanceUsesServiceStateAndSupportsStoppingTheWait(t *testing.T) {
	for _, running := range []bool{false, true} {
		b := &fakeBackend{snapshot: settingsTestSnapshot()}
		network := downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: "https://example.invalid/"}
		b.prefs.Network = network
		b.snapshot.Service.Active = running
		if running {
			b.snapshot.Service.State = "active"
		}
		stops, updates := 0, 0
		b.prepareGeo = func(_ context.Context, cfg downloadnet.Config, _ func(mihomo.Progress)) (app.GeoPreparation, error) {
			stops++
			if cfg != network.Normalized() {
				t.Fatalf("download method changed: %+v", cfg)
			}
			return app.GeoPreparation{Present: []string{"GeoSite.dat"}}, nil
		}
		b.updateGeo = func(ctx context.Context) error {
			updates++
			if err := ctx.Err(); err != nil {
				return diagnostic.New("diagnostic.geo.updateWaitStopped", "stopped waiting for the GEO update, Mihomo may continue updating: %w", err)
			}
			return nil
		}
		m := ready(b)
		m.page, m.settingsTab, m.focus, m.width, m.height = "settings", resourcesTab, geoMaintenanceID, 150, 46
		m.layout()
		m, run := click(m, findTarget(t, m, geoMaintenanceID))
		if !m.busy || run == nil {
			t.Fatal("resource page did not start an action")
		}
		if running {
			if !strings.Contains(m.screen, m.tr("geo.action.stopWaiting")) {
				t.Fatal("running check did not expose Stop waiting")
			}
			m, _ = update(m, key("enter"))
			if m.opCtx.Err() == nil {
				t.Fatal("keyboard action did not cancel the request context")
			}
		}
		m, _ = update(m, inlineOperationMsg(t, run))
		if len(m.notification.items) == 0 || m.notification.items[len(m.notification.items)-1].Silent {
			t.Fatal("GeoData completion was hidden from the resource page")
		}
		if running {
			if updates != 1 || stops != 0 || !strings.Contains(m.message, m.tr("geo.result.stoppedWaiting")) {
				t.Fatalf("incorrect active update result: updates=%d prepares=%d message=%q", updates, stops, m.message)
			}
		} else if stops != 1 || updates != 0 || !strings.Contains(m.message, "GeoSite.dat") {
			t.Fatalf("incorrect stopped result: updates=%d prepares=%d message=%q", updates, stops, m.message)
		}
	}
}
