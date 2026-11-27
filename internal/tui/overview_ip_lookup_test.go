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
	m.busy = false
	m.adoptSnapshot(app.Snapshot{
		Installed: true, MixedPort: 7890, Service: service.Status{Active: true, State: "active", PID: 12},
		Mode: app.ModeStatus{Actual: "rule"}, Groups: []mihomo.Group{{Name: "PROXY", Now: "A"}},
	})

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

	m.ipInfo.checkedAt = time.Now().Add(-24 * time.Hour)
	if m.syncIPInfo(false) != nil {
		t.Fatal("an unchanged route should not poll for IP info")
	}
	pending := m.activate("ip.refresh")
	if pending == nil {
		t.Fatal("manual refresh should start a new lookup")
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
	// A completed request may already be queued when cancellation reaches it.
	stale.err, stale.info = nil, app.IPInfo{IP: "203.0.113.99"}
	m.acceptIPInfo(stale)
	if !m.ipInfo.loading || m.ipInfo.failed || m.ipInfo.info.IP != "" {
		t.Fatal("a late result from the previous route replaced the current request")
	}
	lookupErr = errors.New("offline")
	m.acceptIPInfo(current().(ipInfoResultMsg))
	if !m.ipInfo.failed || m.syncIPInfo(false) != nil {
		t.Fatal("a failed lookup should wait for a new route or a manual retry")
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
	m.acceptIPInfo(stale)
	if m.ipInfo.info != info || m.ipInfo.failed {
		t.Fatal("a late response replaced the successful retry")
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

func TestOverviewIPQueriesLatestObservedRouteOnReturn(t *testing.T) {
	queries := 0
	m := New(t.Context(), &fakeBackend{lookupIPInfo: func(context.Context) (app.IPInfo, error) {
		queries++
		return app.IPInfo{IP: "203.0.113.7"}, nil
	}}, "en")
	m.busy = false
	s := app.Snapshot{
		Installed: true, MixedPort: 7890, Service: service.Status{Active: true, State: "active", PID: 12},
		Mode: app.ModeStatus{Actual: "rule"}, Applied: "work", AppliedRecipeHash: "revision-1",
		Groups: []mihomo.Group{{Name: "Office", Now: "A"}},
	}
	m.adoptSnapshot(s)
	query := func(want int) {
		t.Helper()
		cmd := m.syncIPInfo(false)
		if cmd == nil {
			t.Fatal("expected an automatic exit IP lookup")
		}
		m.acceptIPInfo(cmd().(ipInfoResultMsg))
		if queries != want || m.syncIPInfo(false) != nil {
			t.Fatalf("expected exactly %d queries, got %d", want, queries)
		}
	}
	query(1)

	s.Generation = "settings-only"
	m.adoptSnapshot(s)
	incomplete := s
	incomplete.APIError, incomplete.Groups = "temporarily unavailable", nil
	m.adoptSnapshot(incomplete)
	m.adoptSnapshot(s)
	if m.syncIPInfo(false) != nil || m.ipInfo.info.IP == "" {
		t.Fatal("an unrelated setting or temporary control API error invalidated the exit IP")
	}
	m.navigate("proxies")
	s.Groups = []mihomo.Group{{Name: "Office", Now: "B"}}
	m.adoptSnapshot(s)
	s.Mode.Actual = "global"
	m.adoptSnapshot(s)
	if m.syncIPInfo(false) != nil || m.ipInfo.info.IP != "" {
		t.Fatal("route changes away from the overview must discard the old result and defer the lookup")
	}
	m.navigate("overview")
	query(2)

	s.Applied = "home"
	incomplete = s
	incomplete.APIError, incomplete.Groups = "proxy groups unavailable", nil
	m.adoptSnapshot(incomplete)
	query(3)
	m.adoptSnapshot(s)
	if m.syncIPInfo(false) != nil {
		t.Fatal("learning the new subscription's group list should not repeat its lookup")
	}
	s.AppliedRecipeHash = "revision-2"
	m.adoptSnapshot(s)
	query(4)
}
