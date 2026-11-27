package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestOverviewIPCardShowsExitDetailsAndWrapsIPv6(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		o := dashboardOptions{Locale: i18n.New(language), Snapshot: app.Snapshot{
			Installed: true, MixedPort: 7890, Service: service.Status{Active: true, State: "active"},
		}}
		ip := "2001:db8:1020:3040:5060:7080:90ab:cdef"
		o.ipInfo.info = app.IPInfo{IP: ip, Country: "Japan", Region: "Tokyo", City: "Tokyo",
			ISP: "Example ISP", Organization: "Example Org", ASN: "AS64500", Timezone: "Asia/Tokyo"}
		card := overviewIPCard(o, 82)
		view := ansi.Strip(strings.Join(card.lines, "\n"))
		for _, value := range []string{ip, "Japan · Tokyo", "Example ISP", "Example Org", "AS64500", "Asia/Tokyo", o.Locale.T("dashboard.overview.ipInfo")} {
			if !strings.Contains(view, value) {
				t.Fatalf("%s: missing %s in %s", language, value, view)
			}
		}
		if strings.Contains(view, "Tokyo · Tokyo") || !hasAction(card.targets, "ip.refresh") {
			t.Fatalf("duplicate location or missing refresh action: %s", view)
		}
		// At this width the address wraps across multiple lines of a single column.
		narrow := overviewIPCard(o, 28)
		assertCanvas(t, strings.Join(narrow.lines, "\n"), 28, len(narrow.lines))
		compact := strings.Join(strings.Fields(ansi.Strip(strings.Join(narrow.lines, "\n"))), "")
		if !strings.Contains(compact, ip) {
			t.Fatalf("narrow card truncated IPv6: %s", compact)
		}
	}
}

func TestOverviewIPCardReflectsLookupState(t *testing.T) {
	for _, tc := range []struct {
		name, message                            string
		active, loading, failed, focused, target bool
	}{
		{name: "proxy stopped", message: "dashboard.ip.unavailable"},
		{name: "waiting", message: "dashboard.ip.waiting", active: true, target: true},
		{name: "initial request", message: "dashboard.ip.loading", active: true, loading: true},
		{name: "focused request", message: "dashboard.ip.loading", active: true, loading: true, focused: true, target: true},
		{name: "failed", message: "dashboard.ip.failed", active: true, failed: true, target: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := dashboardOptions{Snapshot: app.Snapshot{
				Installed: true, MixedPort: 7890, Service: service.Status{Active: tc.active, State: "active"},
			}, ipInfo: overviewIPState{loading: tc.loading, failed: tc.failed}}
			if !tc.active || tc.failed {
				o.ipInfo.info = app.IPInfo{IP: "198.51.100.7"}
			}
			if tc.focused {
				o.Focus = "ip.refresh"
			}
			card := overviewIPCard(o, 44)
			view := ansi.Strip(strings.Join(card.lines, "\n"))
			if !strings.Contains(view, o.Locale.T(tc.message)) || strings.Contains(view, "198.51.100.7") || hasAction(card.targets, "ip.refresh") != tc.target {
				t.Fatalf("incorrect lookup state or action in %s", view)
			}
		})
	}
}
