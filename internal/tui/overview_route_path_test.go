package tui

import (
	"slices"
	"testing"

	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestOverviewRoutePath(t *testing.T) {
	locale := i18n.New(i18n.Chinese)
	for _, tc := range []struct {
		name         string
		groups       []mihomo.Group
		want         []string
		wantEndpoint bool
	}{
		{"direct", []mihomo.Group{{Name: "PROXY", Now: "DIRECT"}}, []string{"DIRECT"}, true},
		{"nested", []mihomo.Group{{Name: "GLOBAL", Now: "Work"}, {Name: "主用", Type: "URLTest", Now: "东京"}, {Name: "Work", Now: "主用"}}, []string{"Work", "主用", "东京"}, true},
		{"self reference", []mihomo.Group{{Name: "PROXY", Now: "PROXY"}}, []string{locale.T("proxies.cycle")}, false},
		{"nested cycle", []mihomo.Group{{Name: "PROXY", Now: "A"}, {Name: "A", Now: "B"}, {Name: "B", Now: "A"}}, []string{"A", "B", locale.T("proxies.cycle")}, false},
		{"empty nested choice", []mihomo.Group{{Name: "PROXY", Now: "AUTO"}, {Name: "AUTO", Type: "URLTest"}}, []string{"AUTO", locale.T("proxies.no_selection")}, false},
		{"distributed", []mihomo.Group{{Name: "Work", Type: "LoadBalance", Now: "not-a-fixed-route"}}, []string{locale.T("proxies.distributed")}, false},
		{"nested relay", []mihomo.Group{{Name: "Work", Now: "Chain"}, {Name: "Chain", Type: "Relay", Now: "not-a-fixed-route"}}, []string{"Chain", locale.T("proxies.distributed")}, false},
		{"missing member", []mihomo.Group{{Name: "PROXY", Now: "AUTO"}, {Name: "AUTO", Now: "removed", All: []string{"Tokyo"}}}, []string{"AUTO", locale.T("dashboard.overview.selectionUnavailable")}, false},
		{"raw identity and safe display", []mihomo.Group{{Name: "PROXY", Now: "A\x1b[32m"}, {Name: "A\x1b[32m", Now: "叶\n子"}}, []string{"A", "叶子"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, hasEndpoint := overviewRoutePath(tc.groups, tc.groups[0], locale)
			if !slices.Equal(got, tc.want) || hasEndpoint != tc.wantEndpoint {
				t.Fatalf("path = %q, endpoint = %t; want %q, %t", got, hasEndpoint, tc.want, tc.wantEndpoint)
			}
		})
	}
}
