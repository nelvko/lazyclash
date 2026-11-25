package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestOverviewGroupSelectionReportsImmediateMember(t *testing.T) {
	global := mihomo.Group{Name: "GLOBAL", Type: "Selector", Now: "global-only"}
	auto := mihomo.Group{Name: "自动", Type: "URLTest", Now: "香港"}
	work := mihomo.Group{Name: "Work", Type: "Selector", Now: auto.Name}
	o := dashboardOptions{Locale: i18n.New(i18n.Chinese), Snapshot: settingsTestSnapshot()}
	o.Snapshot.Mode.Actual, o.Snapshot.Service.Active = "rule", true
	for _, tc := range []struct {
		name   string
		groups []mihomo.Group
		want   string
	}{
		{"prefer selector", []mihomo.Group{global, auto, work}, "Work → 自动"},
		{"automatic group", []mihomo.Group{global, auto}, "自动 → 香港"},
		{"only GLOBAL", []mihomo.Group{global}, o.Locale.T("dashboard.groups.empty")},
		{"empty choice", []mihomo.Group{{Name: "Work", Type: "Selector"}}, o.Locale.T("proxies.no_selection")},
		{"load balance", []mihomo.Group{{Name: "Work", Type: "LoadBalance", Now: "misleading-member"}}, o.Locale.T("proxies.distributed")},
		{"relay", []mihomo.Group{{Name: "Work", Type: "Relay", Now: "misleading-member"}}, o.Locale.T("proxies.distributed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o.Snapshot.Groups = tc.groups
			card := overviewModeCard(o, 80)
			body := ansi.Strip(strings.Join(card.lines, "\n"))
			if !strings.Contains(body, tc.want) || strings.Contains(body, "global-only") || strings.Contains(body, "misleading-member") {
				t.Fatalf("unexpected group summary, wanted %q:\n%s", tc.want, body)
			}
		})
	}
}

func TestOverviewGroupSelectionKeepsBothNamesOnNarrowCards(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.Chinese), Snapshot: settingsTestSnapshot()}
	o.Snapshot.Mode.Actual, o.Snapshot.Service.Active = "rule", true
	o.Snapshot.Groups = []mihomo.Group{{Name: "策略组" + strings.Repeat("组", 12), Type: "Selector", Now: "节点" + strings.Repeat("选", 15)}}
	for _, width := range []int{24, 25, 80} {
		card := overviewModeCard(o, width)
		body := ansi.Strip(strings.Join(card.lines, "\n"))
		assertCanvas(t, body, width, len(card.lines))
		var names []string
		for _, line := range strings.Split(body, "\n") {
			value := strings.TrimSpace(line)
			if value == o.Locale.T("dashboard.overview.groupSelection") || strings.Contains(value, "策略组组") || strings.Contains(value, "节点选") {
				before, _, _ := strings.Cut(line, value)
				if balance := 2*ansi.StringWidth(before) + ansi.StringWidth(value) - width; balance < -1 || balance > 1 {
					t.Fatalf("summary at width %d is off-center: %q", width, line)
				}
			}
			if strings.Contains(value, "策略组组") || strings.Contains(value, "节点选") {
				names = append(names, value)
			}
		}
		if width == 80 {
			if len(names) != 1 || names[0] != o.Snapshot.Groups[0].Name+" → "+o.Snapshot.Groups[0].Now {
				t.Fatalf("wide group summary: %v", names)
			}
		} else if len(names) != 2 || !strings.Contains(names[0], "策略组组") || !strings.Contains(names[1], "节点选") || !strings.HasSuffix(names[0], "…") || !strings.HasSuffix(names[1], "…") {
			t.Fatalf("narrow summary at width %d: %v", width, names)
		}
	}
}
