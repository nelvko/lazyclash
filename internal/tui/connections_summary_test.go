package tui

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

var connectionSummaryKeys = []string{"connections.summary_download", "connections.summary_upload", "connections.summary_memory", "connections.summary_active"}

func summaryMetric(t *testing.T, m Model, rows []string, key string) string {
	t.Helper()
	label := m.tr(key)
	for _, row := range rows {
		plain := ansi.Strip(row)
		start := strings.Index(plain, label)
		if start < 0 {
			continue
		}
		value := plain[start+len(label):]
		for _, other := range connectionSummaryKeys {
			if next := strings.Index(value, m.tr(other)); next >= 0 {
				value = value[:next]
			}
		}
		return strings.Trim(value, " :│\t")
	}
	t.Fatalf("summary field %q missing from %q", label, rows)
	return ""
}

func summaryFixture(t *testing.T, language string) Model {
	t.Helper()
	m := connectionsModel(t, &fakeBackend{})
	m.locale = i18n.New(language)
	m.runtime.snapshot.Connections.DownloadTotal = 3 << 20
	m.runtime.snapshot.Connections.UploadTotal = 5 << 20
	m.runtime.snapshot.Connections.Memory = 7 << 20
	otherMemory := uint64(91 << 20)
	m.runtime.snapshot.Memory = &otherMemory
	m.notification.items = nil
	m.layout()
	return m
}

func TestConnectionsSummaryShowsResponseTotalsMemoryAndUnfilteredCount(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := summaryFixture(t, language)
			rows := m.connectionSummary(160)
			for key, want := range map[string]string{
				"connections.summary_download": "3.0 MiB", "connections.summary_upload": "5.0 MiB",
				"connections.summary_memory": "7.0 MiB", "connections.summary_active": "2",
			} {
				if got := summaryMetric(t, m, rows, key); got != want {
					t.Fatalf("%s = %q, want %q", key, got, want)
				}
			}
			m.connections.search.SetValue("one.example.invalid")
			m.reconcileConnections()
			if got := m.connectionSummary(160); !reflect.DeepEqual(got, rows) {
				t.Fatal("filter changed response totals or total active count")
			}
			bar := m.connectionsToolbar(160, m.connectionToolbarRows(160))
			if !strings.Contains(ansi.Strip(strings.Join(bar.lines[len(rows):], "\n")), m.tr("connections.count", 1, 2)) {
				t.Fatal("filtered count is missing from the status below the summary")
			}
			if m.pageHeader().summary != "" {
				t.Fatal("header duplicates a live count that conflicts with paused summary data")
			}
		})
	}
}

func TestConnectionsSummaryDistinguishesUnavailableAndEmpty(t *testing.T) {
	for _, state := range []string{"nil", "unreceived", "unavailable", "empty"} {
		t.Run(state, func(t *testing.T) {
			m := summaryFixture(t, i18n.English)
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
			rows := m.connectionSummary(160)
			for _, key := range connectionSummaryKeys {
				want := "--"
				if state == "empty" {
					want = map[string]string{"connections.summary_download": "3.0 MiB", "connections.summary_upload": "5.0 MiB", "connections.summary_memory": "7.0 MiB", "connections.summary_active": "0"}[key]
				}
				if got := summaryMetric(t, m, rows, key); got != want {
					t.Fatalf("%s = %q, want %q", key, got, want)
				}
			}
		})
	}
}

func TestConnectionsSummaryFreezesResponseMemoryWithPause(t *testing.T) {
	m := summaryFixture(t, i18n.English)
	m, _ = update(m, key("p"))
	before := m.connectionSummary(160)
	fresh := liveRuntime()
	fresh.ObservedAt = time.Now()
	fresh.Connections = &mihomo.ConnectionSnapshot{DownloadTotal: 13 << 20, UploadTotal: 15 << 20, Memory: 11 << 20, Connections: []mihomo.Connection{fixtureConnection("new", "new.example.invalid", 20)}}
	independentMemory := uint64(123 << 20)
	fresh.Memory = &independentMemory
	m = acceptConnectionsSample(m, fresh)
	if !m.connections.paused || !reflect.DeepEqual(m.connectionSummary(160), before) {
		t.Fatal("live connections or independent memory updates changed the paused summary")
	}
	m, _ = update(m, key("p"))
	rows := m.connectionSummary(160)
	if summaryMetric(t, m, rows, "connections.summary_memory") != "11.0 MiB" || summaryMetric(t, m, rows, "connections.summary_download") != "13.0 MiB" || summaryMetric(t, m, rows, "connections.summary_active") != "1" {
		t.Fatalf("resume did not use the latest connections response: rows=%q paused=%v live=%+v focus=%s pending=%v", rows, m.connections.paused, m.runtime.snapshot.Connections, m.focus, m.runtime.pending)
	}
}

func TestConnectionsSummaryFormatsUnsignedMemoryWithoutOverflow(t *testing.T) {
	m := summaryFixture(t, i18n.English)
	m.runtime.snapshot.Connections.Memory = math.MaxUint64
	value := summaryMetric(t, m, m.connectionSummary(160), "connections.summary_memory")
	if value == "--" || strings.HasPrefix(value, "-") || !strings.Contains(value, "EiB") {
		t.Fatalf("unsigned response memory overflowed: %q", value)
	}
}

func TestConnectionsSummaryResponsiveRowsAndToolbarHeight(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, item := range []struct{ width, height, rows int }{{160, 45, 1}, {120, 32, 2}, {80, 24, 2}, {60, 20, 2}, {40, 16, 4}} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, item.width, item.height), func(t *testing.T) {
				m := summaryFixture(t, language)
				m, _ = update(m, tea.WindowSizeMsg{Width: item.width, Height: item.height})
				o := m.dashboardOptions()
				g := dashboardGeometry(o)
				rows := m.connectionSummary(g.width)
				wantRows := item.rows
				if language == i18n.Chinese && item.width == 120 {
					wantRows = 1
				}
				if len(rows) != wantRows {
					t.Fatalf("summary at body width %d has %d rows, want %d", g.width, len(rows), wantRows)
				}
				for _, row := range rows {
					if ansi.StringWidth(row) > g.width {
						t.Fatalf("summary exceeded body width %d: %q", g.width, row)
					}
				}
				for _, key := range connectionSummaryKeys {
					_ = summaryMetric(t, m, rows, key)
				}
				wantHeight := len(rows) + m.searchToolbarRows(g.width, m.connectionControls())
				if got := m.connectionToolbarRows(g.width); got != wantHeight {
					t.Fatalf("toolbar height %d does not include summary rows: want %d", got, wantHeight)
				}
				bar := m.connectionsToolbar(g.width, wantHeight)
				if len(bar.lines) != wantHeight {
					t.Fatalf("toolbar rendered %d rows, reserved %d", len(bar.lines), wantHeight)
				}
				if o.BodyTopRows == 0 || g.height < 2 {
					t.Fatalf("summary displaced the fixed toolbar or two-row list at %dx%d", item.width, item.height)
				}
				findTarget(t, m, "conn.search")
				findTarget(t, m, "connection.first")
				assertCanvas(t, m.screen, item.width, item.height)
				assertRegions(t, m.targets, item.width, item.height)
			})
		}
	}
}

func TestConnectionsSummaryResizeFallbackAndNotificationsKeepSearchUsable(t *testing.T) {
	m := summaryFixture(t, i18n.English)
	for _, size := range [][2]int{{160, 45}, {80, 24}, {60, 20}, {40, 16}, {40, 10}, {20, 7}, {5, 2}, {120, 32}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		assertCanvas(t, m.screen, size[0], size[1])
		assertRegions(t, m.targets, size[0], size[1])
		if size == [2]int{40, 10} && m.dashboardOptions().BodyTopRows != 0 {
			t.Fatal("short terminal did not retain the scrolling toolbar fallback")
		}
		if m.connections.selected != "first" {
			t.Fatal("resize changed the selected connection identity")
		}
	}
	m.notification.items = []notificationItem{{ID: 42, Kind: "error", Page: "connections", Message: "Controller temporarily unavailable; retry later."}}
	m.layout()
	input := findTarget(t, m, "conn.search")
	o := m.dashboardOptions()
	g := dashboardGeometry(o)
	o.BodyTop = m.connectionsToolbar(g.width, o.BodyTopRows)
	for _, layer := range notificationLayout(o) {
		if regionsOverlap(input, layer.Bounds) {
			t.Fatalf("notification covers search below the summary: %+v", layer.Bounds)
		}
	}
}
