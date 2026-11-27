package tui

import (
	"strings"
	"testing"
	"time"

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
			ISP: "Example ISP", ASN: "AS64500"}
		card := overviewIPCard(o, 82)
		view := ansi.Strip(strings.Join(card.lines, "\n"))
		for _, value := range []string{ip, "Japan · Tokyo", "Example ISP", "AS64500", o.Locale.T("dashboard.overview.ipInfo")} {
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

func TestOverviewIPHeaderShowsSuccessfulAgeOutsideRefreshTarget(t *testing.T) {
	for _, lang := range []string{i18n.English, i18n.Chinese} {
		o := dashboardOptions{Locale: i18n.New(lang), Now: time.Now(), Snapshot: app.Snapshot{
			Installed: true, MixedPort: 7890, Service: service.Status{Active: true, State: "active"},
		}}
		o.ipInfo = overviewIPState{info: app.IPInfo{IP: "198.51.100.1"}, checkedAt: o.Now.Add(-3 * time.Minute)}
		updated := o.Locale.T("profiles.updated_minutes", 3)
		for _, width := range []int{66, 44, 30, 18} {
			card := overviewIPCard(o, width)
			view := strings.Join(card.lines, "\n")
			assertCanvas(t, view, width, len(card.lines))
			if !strings.Contains(ansi.Strip(view), updated) || len(card.targets) != 1 {
				t.Fatalf("%s, width %d: missing time or refresh target in %s", lang, width, ansi.Strip(view))
			}
			hit := card.targets[0]
			action := strings.TrimSpace(ansi.Strip(ansi.Cut(card.lines[hit.Y], hit.X, hit.X+hit.W)))
			if hit.ID != "ip.refresh" || action != "↻" || hit.X+hit.W != width-2 {
				t.Fatalf("%s, width %d: incorrect refresh hit area %q at %+v", lang, width, action, hit)
			}
			line := ansi.Strip(card.lines[hit.Y])
			title := o.Locale.T("dashboard.overview.ipInfo")
			if width >= 44 && (hit.Y != 1 || !strings.HasPrefix(strings.TrimSpace(line), title)) {
				t.Fatalf("expected the time and icon beside the title at width %d: %q", width, line)
			}
			if group := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), title)); group != updated+"  ↻" {
				t.Fatalf("%s, width %d: unexpected time and refresh icon %q", lang, width, group)
			}
		}
		o.ipInfo.checkedAt, o.ipInfo.failed = o.Now, true
		failure := overviewIPCard(o, 66)
		if strings.Contains(ansi.Strip(strings.Join(failure.lines, "\n")), o.Locale.T("profiles.just_updated")) {
			t.Fatal("a failed lookup claimed a successful update")
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
			icon := "↻"
			if tc.loading {
				icon = "…"
			}
			if !strings.HasSuffix(strings.TrimSpace(ansi.Strip(card.lines[1])), icon) {
				t.Fatalf("missing %s icon in header %q", icon, ansi.Strip(card.lines[1]))
			}
			if !strings.Contains(view, o.Locale.T(tc.message)) || strings.Contains(view, "198.51.100.7") || hasAction(card.targets, "ip.refresh") != tc.target {
				t.Fatalf("incorrect lookup state or action in %s", view)
			}
		})
	}
}
