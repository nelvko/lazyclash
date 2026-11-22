package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestCompactProxyGridShowsMoreMembersWithoutLosingActions(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {60, 18}, {160, 24}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m, _ := proxiesModel(t, size[0], size[1])
			card := m.proxyNode(0, 0, 50)
			if len(card.lines) != 2 || !strings.Contains(ansi.Strip(card.lines[0]), "AUTO") || !strings.Contains(ansi.Strip(card.lines[1]), "Proxy group") {
				t.Fatalf("compact node lost its two content rows: %q", ansi.Strip(strings.Join(card.lines, "\n")))
			}
			for _, hit := range card.targets {
				if hit.ID == "proxies.delay.0.0" && hit.Y != 0 || hit.ID == "proxies.reference.0.0" && hit.Y != 1 {
					t.Fatalf("compact node action did not follow its content row: %+v", hit)
				}
			}
			assertRegions(t, card.targets, 50, 2)
			body := m.proxyGroupBody(0, dashboardBodyWidth(m.dashboardOptions()))
			for _, line := range body.lines {
				if strings.TrimSpace(ansi.Strip(line)) == "" {
					t.Fatal("compact grid retained an empty padding or separator row")
				}
			}
		})
	}
	m, _ := proxiesModel(t, 80, 24)
	for i := len(m.snapshot.Groups[0].All); i < 27; i++ {
		m.snapshot.Groups[0].All = append(m.snapshot.Groups[0].All, fmt.Sprintf("candidate-%02d", i))
	}
	m.scroll = 0
	m.layout()
	visible := make(map[string]bool)
	for _, hit := range m.targets {
		if strings.HasPrefix(hit.ID, "node.") && hit.H == 2 {
			visible[hit.ID] = true
		}
	}
	if len(visible) < 12 {
		t.Fatalf("80x24 should show at least twelve complete members, got %d", len(visible))
	}
}

func TestCompactProxyDirectoryUsesOnlyContentRows(t *testing.T) {
	for _, test := range []struct{ width, rows int }{{80, 1}, {60, 2}} {
		m, _ := proxiesModel(t, test.width, 24)
		setProxyTestGroup(&m, "")
		m.snapshot.Groups = nil
		for i := range 12 {
			m.snapshot.Groups = append(m.snapshot.Groups, mihomo.Group{Name: fmt.Sprintf("Group %02d", i), Type: "Selector", Now: "DIRECT", All: []string{"DIRECT"}})
		}
		body, hits := m.proxiesBody(dashboardBodyWidth(m.dashboardOptions()))
		if got := len(strings.Split(body, "\n")); got != 12*test.rows {
			t.Fatalf("%d-column directory retained padding: rows=%d want=%d", test.width, got, 12*test.rows)
		}
		assertRegions(t, hits, dashboardBodyWidth(m.dashboardOptions()), 12*test.rows)
	}
}

func TestCompactProfilesKeepMetadataStatusAndTitleActions(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {60, 18}, {160, 24}} {
		m := profilesPreview(false)
		m.width, m.height = size[0], size[1]
		card := m.profileCard(0, 60)
		if len(card.lines) != 4 {
			t.Fatalf("%v: compact profile has %d rows", size, len(card.lines))
		}
		plain := ansi.Strip(strings.Join(card.lines, "\n"))
		for _, want := range []string{"Everyday", "subscription.example", "2h ago", m.tr("profiles.changes_pending")} {
			if !strings.Contains(plain, want) {
				t.Fatalf("%v: compact profile lost %q: %s", size, want, plain)
			}
		}
		for _, hit := range card.targets {
			if hit.ID != "profile.0" && hit.Y != 0 {
				t.Fatalf("title action did not follow the compact title: %+v", hit)
			}
		}
		assertRegions(t, card.targets, 60, 4)
	}
	m := profilesPreview(false)
	m.width, m.height, m.focus, m.scroll = 80, 24, "profiles.url", 0
	m.layout()
	complete := 0
	for _, hit := range m.targets {
		if strings.HasPrefix(hit.ID, "profile.") && hit.H == 4 {
			complete++
		}
	}
	if complete < 2 {
		t.Fatalf("80x24 should show two complete profile cards, got %d", complete)
	}
}
