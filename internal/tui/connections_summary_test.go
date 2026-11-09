package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func totalsFixture(t *testing.T, language string) Model {
	t.Helper()
	m := connectionsModel(t, &fakeBackend{})
	m.locale = i18n.New(language)
	m.runtime.snapshot.Connections.DownloadTotal = 3 << 20
	m.runtime.snapshot.Connections.UploadTotal = 5 << 20
	m.runtime.snapshot.Connections.Memory = 7 << 20
	m.runtime.snapshot.Traffic.DownTotal = 91 << 20
	m.runtime.snapshot.Traffic.UpTotal = 93 << 20
	m.notification.items = nil
	m.layout()
	return m
}

func TestConnectionsHeaderTotalsUseResponseAndExcludeMemory(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := totalsFixture(t, language)
			for _, compact := range []bool{false, true} {
				value := ansi.Strip(m.connectionTotals(compact))
				if !strings.Contains(value, "3.0 MiB") || !strings.Contains(value, "5.0 MiB") || strings.Contains(value, "7.0 MiB") || strings.Contains(value, "91.0 MiB") || strings.Contains(value, "93.0 MiB") {
					t.Fatalf("header totals used the wrong source or included memory: %q", value)
				}
			}
			header := m.pageHeader()
			if header.summary != m.connectionTotals(false) || header.compactSummary != m.connectionTotals(true) {
				t.Fatal("connection totals are missing from the page header")
			}
			before := m.connectionTotals(false)
			m.connections.search.SetValue("one.example.invalid")
			m.connectionActivate("conn.closed")
			if m.connectionTotals(false) != before {
				t.Fatal("search or archive selection changed core-wide cumulative totals")
			}
			bar := m.connectionsToolbar(160, m.connectionToolbarRows(160))
			body := ansi.Strip(strings.Join(bar.lines, "\n"))
			if strings.Contains(body, m.tr("connections.summary_download")) || strings.Contains(body, m.tr("connections.summary_upload")) || strings.Contains(body, "7.0 MiB") {
				t.Fatal("old summary metrics remain in the list toolbar")
			}
		})
	}
}

func TestConnectionsHeaderTotalsDistinguishUnavailableAndEmpty(t *testing.T) {
	for _, state := range []string{"nil", "unreceived", "unavailable", "empty"} {
		t.Run(state, func(t *testing.T) {
			m := totalsFixture(t, i18n.English)
			switch state {
			case "nil":
				m.runtime.snapshot.Connections = nil
			case "unreceived":
				m.runtime.received = false
			case "unavailable":
				m.runtime.snapshot.State = "unavailable"
			case "empty":
				m.runtime.snapshot.Connections.Connections = []mihomo.Connection{}
			}
			for _, compact := range []bool{false, true} {
				value := ansi.Strip(m.connectionTotals(compact))
				if state == "empty" {
					if !strings.Contains(value, "3.0 MiB") || !strings.Contains(value, "5.0 MiB") {
						t.Fatalf("empty active array discarded cumulative totals: %q", value)
					}
				} else if strings.Count(value, "--") != 2 || strings.Contains(value, "0 B") {
					t.Fatalf("unavailable totals were displayed as observed zeroes: %q", value)
				}
			}
		})
	}
}

func TestConnectionsHeaderTotalsFreezeWithPauseAndResume(t *testing.T) {
	m := totalsFixture(t, i18n.English)
	m, _ = update(m, key("p"))
	before := m.connectionTotals(false)
	fresh := liveRuntime()
	fresh.ObservedAt = time.Now()
	fresh.Connections = &mihomo.ConnectionSnapshot{DownloadTotal: 13 << 20, UploadTotal: 15 << 20, Connections: []mihomo.Connection{fixtureConnection("new", "new.example.invalid", 20)}}
	m = acceptConnectionsSample(m, fresh)
	if !m.connections.paused || m.connectionTotals(false) != before {
		t.Fatal("live connection updates changed paused header totals")
	}
	m, _ = update(m, key("p"))
	value := ansi.Strip(m.connectionTotals(false))
	if !strings.Contains(value, "13.0 MiB") || !strings.Contains(value, "15.0 MiB") {
		t.Fatalf("resume did not display latest header totals: %q", value)
	}
}

func TestConnectionsHeaderTotalsAndTabsRemainBoundedAcrossSizes(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := totalsFixture(t, language)
		for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {40, 16}, {40, 10}, {20, 7}, {5, 2}, {160, 45}} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, size[0], size[1]), func(t *testing.T) {
				m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
				if size[0] >= 40 && size[1] >= 16 {
					findTarget(t, m, "conn.search")
					findTarget(t, m, "conn.active")
					findTarget(t, m, "conn.closed")
					findTarget(t, m, "connection.first")
				}
				if size[0] == 160 {
					g := dashboardGeometry(m.dashboardOptions())
					header := ansi.Strip(strings.Join(strings.Split(m.screen, "\n")[:g.y], "\n"))
					if !strings.Contains(header, "3.0 MiB") || !strings.Contains(header, "5.0 MiB") {
						t.Fatal("wide header did not render both cumulative totals above the list")
					}
				}
			})
		}
	}
}
