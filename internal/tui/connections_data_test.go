package tui

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func connectionRateSnapshot(at time.Time, rows ...mihomo.Connection) app.RuntimeSnapshot {
	return app.RuntimeSnapshot{
		State: "ready", ObservedAt: at, RunningVersion: "v1.19.33", Service: service.Status{PID: 123},
		Connections: &mihomo.ConnectionSnapshot{UploadTotal: 10000, DownloadTotal: 20000, Connections: rows},
	}
}

func TestConnectionRatesUseConsecutiveSourceObservations(t *testing.T) {
	at := time.Unix(1000, 0)
	c := mihomo.Connection{ID: "one", Upload: 100, Download: 200, Start: at.Add(-time.Minute)}
	var rates connectionRatesState
	rates.observe(connectionRateSnapshot(at, c), at.Add(time.Hour))
	if got := rates.get(c.ID); got.Valid || got.Upload != -1 || got.Download != -1 {
		t.Fatalf("first observation fabricated rates: %+v", got)
	}
	c.Upload, c.Download = 400, 1100
	rates.observe(connectionRateSnapshot(at.Add(1500*time.Millisecond), c), at.Add(2*time.Hour))
	if got := rates.get(c.ID); !got.Valid || got.Upload != 200 || got.Download != 600 {
		t.Fatalf("rate did not use source observation interval: %+v", got)
	}
	// Saving a value copy freezes the display even as later observations arrive.
	frozen := rates
	rates.observe(connectionRateSnapshot(at.Add(2500*time.Millisecond), c), at)
	if got := rates.get(c.ID); !got.Valid || got.Upload != 0 || got.Download != 0 {
		t.Fatalf("idle connection did not retain known zero rates: %+v", got)
	}
	if got := frozen.get(c.ID); got.Upload != 200 || got.Download != 600 {
		t.Fatalf("new observation mutated frozen rate maps: %+v", got)
	}
	if got := rates.get("missing"); got.Valid || got.Upload != -1 || got.Download != -1 {
		t.Fatalf("unknown connection inherited zero rate: %+v", got)
	}
}

func TestConnectionRatesResetDiscontinuousObservations(t *testing.T) {
	at := time.Unix(1000, 0)
	base := mihomo.Connection{ID: "one", Upload: 100, Download: 200, Start: at.Add(-time.Minute)}
	tests := []struct {
		name   string
		change func(*app.RuntimeSnapshot)
	}{
		{"stopped", func(s *app.RuntimeSnapshot) { s.State = "stopped" }},
		{"unavailable", func(s *app.RuntimeSnapshot) { s.State = "unavailable" }},
		{"missing connections", func(s *app.RuntimeSnapshot) { s.Connections = nil }},
		{"removed", func(s *app.RuntimeSnapshot) { s.Connections.Connections = nil }},
		{"same timestamp", func(s *app.RuntimeSnapshot) { s.ObservedAt = at }},
		{"backward timestamp", func(s *app.RuntimeSnapshot) { s.ObservedAt = at.Add(-time.Second) }},
		{"new process", func(s *app.RuntimeSnapshot) { s.Service.PID++ }},
		{"new version", func(s *app.RuntimeSnapshot) { s.RunningVersion = "v1.20.0" }},
		{"core upload reset", func(s *app.RuntimeSnapshot) { s.Connections.UploadTotal = 0 }},
		{"core download reset", func(s *app.RuntimeSnapshot) { s.Connections.DownloadTotal = 0 }},
		{"upload reset", func(s *app.RuntimeSnapshot) { s.Connections.Connections[0].Upload = 50 }},
		{"download reset", func(s *app.RuntimeSnapshot) { s.Connections.Connections[0].Download = 50 }},
		{"negative upload", func(s *app.RuntimeSnapshot) { s.Connections.Connections[0].Upload = -1 }},
		{"negative download", func(s *app.RuntimeSnapshot) { s.Connections.Connections[0].Download = -1 }},
		{"reused identifier", func(s *app.RuntimeSnapshot) { s.Connections.Connections[0].Start = at }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var rates connectionRatesState
			rates.observe(connectionRateSnapshot(at, base), at)
			c := base
			c.Upload, c.Download = 200, 400
			next := connectionRateSnapshot(at.Add(time.Second), c)
			test.change(&next)
			rates.observe(next, at.Add(time.Second))
			if got := rates.get(c.ID); got.Valid || got.Upload != -1 || got.Download != -1 {
				t.Fatalf("discontinuity exposed stale or invalid rate: %+v", got)
			}
			if next.Connections == nil || next.State != "ready" {
				if len(rates.samples) != 0 || len(rates.values) != 0 || !rates.at.IsZero() {
					t.Fatal("unavailable snapshot retained rate baseline")
				}
			}
		})
	}
}

func TestConnectionRatesRecoverAfterGapAndPruneRemovedRows(t *testing.T) {
	at := time.Unix(1000, 0)
	one := mihomo.Connection{ID: "one", Upload: 100, Download: 200, Start: at}
	two := mihomo.Connection{ID: "two", Upload: 100, Download: 200, Start: at}
	var rates connectionRatesState
	rates.observe(connectionRateSnapshot(at, one, two), at)
	rates.observe(app.RuntimeSnapshot{State: "unavailable"}, at.Add(time.Second))
	one.Upload, one.Download = 1000, 2000
	rates.observe(connectionRateSnapshot(at.Add(5*time.Second), one), at)
	if rates.get(one.ID).Valid {
		t.Fatal("recovery computed rate across unavailable gap")
	}
	one.Upload, one.Download = 1300, 2600
	rates.observe(connectionRateSnapshot(at.Add(8*time.Second), one), at)
	if got := rates.get(one.ID); !got.Valid || got.Upload != 100 || got.Download != 200 {
		t.Fatalf("consecutive observations after recovery stayed unavailable: %+v", got)
	}
	if _, found := rates.samples[two.ID]; found || len(rates.samples) != 1 {
		t.Fatal("closed connection retained rate history")
	}
	// Fixtures without source timestamps use receipt times instead.
	one.Upload, one.Download = 1500, 3000
	rates.observe(connectionRateSnapshot(time.Time{}, one), at.Add(10*time.Second))
	if got := rates.get(one.ID); !got.Valid || got.Upload != 100 || got.Download != 200 {
		t.Fatalf("zero source timestamp did not use receipt time: %+v", got)
	}
}

func TestConnectionSortModesHaveStableIdentityTies(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	rows := []mihomo.Connection{
		{
			ID: "c", Start: time.Unix(30, 0), Upload: 30, Download: 10,
			Metadata: mihomo.ConnectionMetadata{Host: "Zulu", Network: "TCP", Process: " zeta "},
			Chains:   []string{"alpha", "Zulu"}, Rule: "RuleSet", RulePayload: "beta",
		},
		{
			ID: "b", Start: time.Unix(20, 0), Upload: 40, Download: 30,
			Metadata: mihomo.ConnectionMetadata{Host: "alpha", Network: "udp", Process: "Zeta"},
			Chains:   []string{"zulu", "alpha"}, Rule: "RuleSet", RulePayload: "zulu",
		},
		{
			ID: "a", Start: time.Unix(20, 0), Upload: 40, Download: 20,
			Metadata: mihomo.ConnectionMetadata{DestinationIP: "Alpha", Network: "tcp", Process: "alpha"},
			Chains:   []string{"DIRECT", "BETA"}, Rule: "RuleSet", RulePayload: "alpha",
		},
		{ID: "d", Download: -1, Upload: -1},
	}
	m.runtime.snapshot = connectionRateSnapshot(time.Unix(100, 0), rows...)
	m.connections.rates = connectionRatesState{values: map[string]connectionRate{
		"a": {Upload: 20, Download: 20, Valid: true},
		"b": {Upload: 10, Download: 20, Valid: true},
		"c": {Upload: 30, Download: 0, Valid: true},
	}}
	tests := []struct {
		order           connectionSort
		first, reversed string
	}{
		{connectionSortNewest, "cabd", "abcd"},
		{connectionSortDownloadRate, "abcd", "cabd"},
		{connectionSortUploadRate, "cabd", "bacd"},
		{connectionSortDownloadTotal, "bacd", "cabd"},
		{connectionSortUploadTotal, "abcd", "cabd"},
		{connectionSortDuration, "abcd", "cabd"},
		{connectionSortDestination, "abcd", "cabd"},
		{connectionSortProtocol, "acbd", "bacd"},
		{connectionSortProcess, "abcd", "bcad"},
		{connectionSortChain, "bacd", "cabd"},
		{connectionSortRule, "acbd", "bcad"},
	}
	for _, test := range tests {
		t.Run(test.order.key(), func(t *testing.T) {
			m.connections.sort = test.order
			for i, want := range []string{test.first, test.reversed} {
				m.connections.sortReversed = i == 1
				var ids strings.Builder
				for _, c := range m.filteredConnections() {
					ids.WriteString(c.ID)
				}
				if ids.String() != want {
					t.Fatalf("reversed=%t: got %q, want %q", m.connections.sortReversed, ids.String(), want)
				}
			}
		})
	}
	if rows[0].ID != "c" || rows[1].ID != "b" {
		t.Fatal("sorting reordered the runtime snapshot")
	}
}

func TestConnectionSortArchiveUsesObservedEndForDurationAndNewest(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m.connections.closedView = true
	m.connections.history.closed = []closedConnection{
		{Connection: mihomo.Connection{ID: "reused", Start: time.Unix(60, 0)}, ClosedAt: time.Unix(62, 0)},
		{Connection: mihomo.Connection{ID: "reused", Start: time.Unix(20, 0)}, ClosedAt: time.Unix(28, 0)},
		{Connection: mihomo.Connection{ID: "long", Start: time.Unix(1, 0)}, ClosedAt: time.Unix(70, 0)},
		{Connection: mihomo.Connection{ID: "invalid", Start: time.Unix(90, 0)}, ClosedAt: time.Unix(80, 0)},
	}
	for _, test := range []struct {
		order    connectionSort
		reversed bool
		starts   []int64
	}{
		{connectionSortDuration, false, []int64{1, 20, 60, 90}},
		{connectionSortDuration, true, []int64{60, 20, 1, 90}},
		{connectionSortNewest, false, []int64{90, 1, 60, 20}},
		{connectionSortNewest, true, []int64{20, 60, 1, 90}},
	} {
		m.connections.sort, m.connections.sortReversed = test.order, test.reversed
		var starts []int64
		for _, c := range m.filteredConnections() {
			starts = append(starts, c.Start.Unix())
		}
		if !slices.Equal(starts, test.starts) {
			t.Fatalf("%s reversed=%t: starts %v, want %v", test.order.key(), test.reversed, starts, test.starts)
		}
	}
}

func TestConnectionRateSaturatesLargeRates(t *testing.T) {
	if got := connectionBytesPerSecond(math.MaxInt64, time.Nanosecond); got != math.MaxInt64 {
		t.Fatalf("overflowing positive rate became %d", got)
	}
}
