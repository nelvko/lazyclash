package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestOverviewIPRefreshLifecycle(t *testing.T) {
	info := app.IPInfo{IP: "203.0.113.1"}
	var lookupErr error
	b := &fakeBackend{lookupIPInfo: func(ctx context.Context) (app.IPInfo, error) {
		if ctx.Err() != nil {
			return app.IPInfo{}, ctx.Err()
		}
		if lookupErr != nil {
			return app.IPInfo{}, lookupErr
		}
		return info, nil
	}}
	m := New(t.Context(), b, "en")
	m.busy, m.snapshotReady = false, true
	m.snapshot = app.Snapshot{
		Installed: true, MixedPort: 7890, Service: service.Status{Active: true, State: "active", PID: 12},
		Groups: []mihomo.Group{{Name: "PROXY", Now: "A"}},
	}

	first := m.syncIPInfo(false)
	if first == nil || m.busy || m.activate("ip.refresh") != nil {
		t.Fatal("the automatic lookup should run once without blocking the overview")
	}
	m.acceptIPInfo(first().(ipInfoResultMsg))
	m.navigate("settings")
	m.syncIPInfo(false)
	m.navigate("overview")
	if m.syncIPInfo(false) != nil || m.ipInfo.info != info {
		t.Fatal("a brief visit to another page should keep the current observation")
	}

	m.ipInfo.checkedAt = time.Now().Add(-ipRefreshInterval)
	pending := m.syncIPInfo(false)
	if pending == nil {
		t.Fatal("expired IP info should refresh on the next overview update")
	}
	s := m.snapshot
	s.Groups = []mihomo.Group{{Name: "PROXY", Now: "B"}}
	m.adoptSnapshot(s)
	current := m.syncIPInfo(false)
	if current == nil || m.ipInfo.info.IP != "" {
		t.Fatal("changing the selected node should replace the in-flight lookup")
	}
	stale := pending().(ipInfoResultMsg)
	if !errors.Is(stale.err, context.Canceled) {
		t.Fatal("the previous route's lookup was not canceled")
	}
	m.acceptIPInfo(stale)
	if !m.ipInfo.loading || m.ipInfo.failed {
		t.Fatal("a late result from the previous route replaced the current request")
	}
	lookupErr = errors.New("offline")
	m.acceptIPInfo(current().(ipInfoResultMsg))
	if !m.ipInfo.failed || m.syncIPInfo(false) != nil {
		t.Fatal("a failed lookup should wait before retrying automatically")
	}
	retry := m.activate("ip.refresh")
	if retry == nil {
		t.Fatal("the refresh action should allow an immediate retry")
	}
	m.navigate("settings")
	m.syncIPInfo(false)
	m.navigate("overview")
	if m.syncIPInfo(false) != nil || !m.ipInfo.failed {
		t.Fatal("leaving during a retry should retain the last known failure")
	}
	lookupErr = nil
	retry = m.activate("ip.refresh")
	m.acceptIPInfo(retry().(ipInfoResultMsg))
	if m.ipInfo.info != info || m.ipInfo.failed {
		t.Fatal("the successful retry should restore the data")
	}

	m.snapshot.Service.PID++
	pending = m.syncIPInfo(false)
	if pending == nil || m.ipInfo.info.IP != "" {
		t.Fatal("a restarted core inherited the previous IP")
	}
	m.navigate("settings")
	if m.syncIPInfo(false) != nil || !errors.Is(pending().(ipInfoResultMsg).err, context.Canceled) {
		t.Fatal("leaving the overview should cancel an in-flight lookup")
	}
	m.navigate("overview")
	m.snapshot.Service.Active = false
	if m.syncIPInfo(true) != nil || m.ipInfo.loading || m.ipInfo.info.IP != "" {
		t.Fatal("a stopped core should have no exit IP or active request")
	}
}
