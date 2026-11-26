package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestOverviewTrafficCardsDistinguishWaitingIdleAndFailure(t *testing.T) {
	o := dashboardOptions{
		Locale: i18n.New(i18n.Chinese), Snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true}},
		Runtime: app.RuntimeSnapshot{State: "ready"}, RuntimeReceived: true, Height: 45, TrafficNow: trafficChartTestNow(),
	}
	waiting, idle := o.Locale.T("dashboard.overview.trafficWaiting"), o.Locale.T("dashboard.overview.trafficIdle")
	check := func(want string) {
		t.Helper()
		for _, width := range []int{40, 100, 160} {
			card := ansi.Strip(strings.Join(overviewTrafficCard(o, width).lines, "\n"))
			if strings.Contains(card, waiting) != (want == waiting) || strings.Contains(card, idle) != (want == idle) ||
				want == waiting && strings.Contains(card, o.Locale.T("runtime.status.partial")) {
				t.Fatalf("width %d expected chart state %q:\n%s", width, want, card)
			}
		}
	}
	check(waiting)
	o.Runtime.Traffic = &mihomo.Traffic{}
	o.TrafficHistory = []trafficPoint{{At: o.TrafficNow, Valid: true}}
	check(idle)
	o.Runtime.TrafficError = "fixture traffic stream interrupted"
	check("")
}

func TestOverviewTrafficSummaryRejectsStaleValuesForEachFailedSource(t *testing.T) {
	for _, source := range []string{"traffic", "connections", "memory"} {
		t.Run(source, func(t *testing.T) {
			runtime := liveRuntime()
			runtime.Traffic = &mihomo.Traffic{Up: 7 << 10, Down: 11 << 10, UpTotal: 3 << 20, DownTotal: 5 << 20}
			runtime.Connections.Connections = make([]mihomo.Connection, 6)
			memory := uint64(64 << 20)
			runtime.Memory = &memory
			switch source {
			case "traffic":
				runtime.TrafficError = "fixture traffic source interrupted"
			case "connections":
				runtime.ConnectionsError = "fixture connections source interrupted"
			case "memory":
				runtime.MemoryError = "fixture memory source interrupted"
			}
			o := dashboardOptions{Locale: i18n.New(i18n.English), Snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true, State: "active"}}, Runtime: runtime, RuntimeReceived: true, Height: 45}
			plain := ansi.Strip(strings.Join(overviewTrafficCard(o, 100).lines, "\n"))
			for _, value := range []string{"7.0 KiB/s", "11.0 KiB/s", "3.0 MiB", "5.0 MiB"} {
				if strings.Contains(plain, value) != (source != "traffic") {
					t.Fatalf("%s availability did not control %q:\n%s", source, value, plain)
				}
			}
			if strings.Contains(plain, "64.0 MiB") != (source != "memory") {
				t.Fatalf("memory source availability did not control the memory metric:\n%s", plain)
			}
			if slices.Contains(strings.Fields(plain), "6") != (source != "connections") {
				t.Fatalf("connections source availability did not control the count:\n%s", plain)
			}
			if !strings.Contains(plain, "--") || !strings.Contains(plain, "fixture "+source+" source interrupted") {
				t.Fatalf("missing-source placeholder or diagnostic omitted:\n%s", plain)
			}
		})
	}
}

func TestOverviewTrafficSummaryKeepsRunningUnavailableMetricsUnknown(t *testing.T) {
	runtime := liveRuntime()
	runtime.State, runtime.Error = "unavailable", "fixture ownership observation unavailable"
	o := dashboardOptions{Locale: i18n.New(i18n.English), Snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true, State: "active"}}, Runtime: runtime, RuntimeReceived: true, Height: 45}
	plain := ansi.Strip(strings.Join(overviewTrafficCard(o, 100).lines, "\n"))
	for _, key := range []string{"dashboard.overview.upload", "dashboard.overview.download", "dashboard.field.connections", "dashboard.field.coreMemory", "dashboard.overview.totalUpload", "dashboard.overview.totalDownload"} {
		label := o.Locale.T(key)
		if !strings.Contains(plain, label) {
			t.Fatalf("running unavailable summary omitted %q:\n%s", label, plain)
		}
	}
	for _, stale := range []string{"2.0 KiB/s", "4.0 KiB/s", "1.0 MiB", "2.0 MiB", "16.0 MiB", o.Locale.T("dashboard.traffic.startHint")} {
		if strings.Contains(plain, stale) {
			t.Fatalf("running unavailable summary retained %q:\n%s", stale, plain)
		}
	}
	if strings.Count(plain, "--") != 6 {
		t.Fatalf("running unavailable summary did not mark all six metrics unknown:\n%s", plain)
	}
}

func TestOverviewTrafficGroupsReadingsBelowFullWidthChart(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{40, 23}, {40, 24}, {80, 24}, {99, 45}, {100, 45}, {160, 23}, {160, 24}, {160, 45}} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, size[0], size[1]), func(t *testing.T) {
				runtime := liveRuntime()
				runtime.Connections.Connections = make([]mihomo.Connection, 37)
				o := dashboardOptions{Locale: i18n.New(language), Height: size[1], Runtime: runtime, RuntimeReceived: true, TrafficNow: trafficChartTestNow()}
				o.TrafficHistory = []trafficPoint{{At: o.TrafficNow, Up: runtime.Traffic.Up, Down: runtime.Traffic.Down, Valid: true}}
				plain := ansi.Strip(strings.Join(overviewTrafficCard(o, size[0]).lines, "\n"))
				rows := strings.Split(plain, "\n")
				at := func(label string) (x, y int) {
					t.Helper()
					for y, row := range rows {
						if before, _, ok := strings.Cut(row, label); ok {
							return ansi.StringWidth(before), y
						}
					}
					t.Fatalf("missing %q:\n%s", label, plain)
					return 0, 0
				}

				inner := overviewInnerWidth(size[0])
				left := (size[0] - inner) / 2
				nowLabel := o.Locale.T("dashboard.overview.trafficNow")
				nowX, axisY := at(nowLabel)
				height := 6
				if size[0] < 76 || size[1] < 24 {
					height = 4
				}
				if nowX+ansi.StringWidth(nowLabel) != left+inner || axisY < height {
					t.Fatalf("time axis does not span the card:\n%s", plain)
				}
				for i, want := range overviewTrafficChart(o, inner, height) {
					if got := ansi.Cut(rows[axisY-height+i], left, left+inner); got != ansi.Strip(want) {
						t.Fatalf("plot row %d: got %q, want %q", i, got, ansi.Strip(want))
					}
				}

				_, upY := at(o.Locale.T("dashboard.overview.upload"))
				_, connectionsY := at(o.Locale.T("dashboard.field.connections"))
				threeColumns := upY == connectionsY
				if size[0] >= 100 && !threeColumns || size[0] == 40 && threeColumns {
					t.Fatalf("unexpected field grouping below the chart:\n%s", plain)
				}
				withinGroup, betweenBatches := 0, 1
				if size[1] >= 24 {
					if threeColumns {
						withinGroup = 1
					} else {
						betweenBatches = 2
					}
				}
				checkGap := func(from, to, want int) {
					t.Helper()
					if to-from-1 != want {
						t.Fatalf("found %d rows of spacing after row %d, want %d:\n%s", to-from-1, from, want, plain)
					}
					for _, row := range rows[from+1 : to] {
						if strings.TrimSpace(row) != "" {
							t.Fatalf("expected a blank row, got %q", row)
						}
					}
				}
				lastLabelY, lastDetailY := -1, -1
				for _, group := range []struct{ currentLabel, currentValue, detailLabel, detailValue string }{
					{"dashboard.overview.upload", "2.0 KiB/s", "dashboard.overview.totalUpload", "1.0 MiB"},
					{"dashboard.overview.download", "4.0 KiB/s", "dashboard.overview.totalDownload", "2.0 MiB"},
					{"dashboard.field.connections", "37", "dashboard.field.coreMemory", "16.0 MiB"},
				} {
					labelX, labelY := at(o.Locale.T(group.currentLabel))
					valueX, valueY := at(group.currentValue)
					detailX, detailY := at(o.Locale.T(group.detailLabel))
					detailValueX, detailValueY := at(group.detailValue)
					if labelY <= axisY || valueY < labelY || detailY <= valueY || detailValueY < detailY {
						t.Fatalf("misplaced reading for %q:\n%s", group.currentLabel, plain)
					}
					// The optional direction arrow before a cumulative label occupies two cells.
					if size[0] >= 100 && (detailX < labelX || detailX > labelX+2) {
						t.Fatalf("detail %q does not align with its current reading:\n%s", group.detailLabel, plain)
					}
					if valueY == labelY && detailValueY == detailY && valueX != detailValueX {
						t.Fatalf("values for %q start in columns %d and %d:\n%s", group.currentLabel, valueX, detailValueX, plain)
					}
					checkGap(valueY, detailY, withinGroup)
					if lastLabelY >= 0 && labelY != lastLabelY {
						checkGap(lastDetailY, labelY, betweenBatches)
					}
					lastLabelY, lastDetailY = labelY, max(lastDetailY, detailValueY)
				}
			})
		}
	}
}

func TestOverviewTrafficKeepsMetricUnitsTogether(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for width := 28; width <= 140; width++ {
			runtime := liveRuntime()
			runtime.Traffic = &mihomo.Traffic{Up: 1023 << 20, Down: 993 << 10, UpTotal: 44238163149, DownTotal: 95133525606}
			memory := uint64(194 << 20)
			runtime.Memory = &memory
			o := dashboardOptions{Locale: i18n.New(language), Snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true, State: "active"}}, Runtime: runtime, RuntimeReceived: true, Height: 24}
			card := overviewTrafficCard(o, width)
			plain := ansi.Strip(strings.Join(card.lines, "\n"))
			for _, metric := range []string{formatBytes(runtime.Traffic.Up) + "/s", formatBytes(runtime.Traffic.Down) + "/s", formatBytes(runtime.Traffic.UpTotal), formatBytes(runtime.Traffic.DownTotal), formatBytes(int64(memory))} {
				if !strings.Contains(plain, metric) {
					t.Fatalf("%s width %d split metric %q:\n%s", language, width, metric, plain)
				}
			}
			assertCanvas(t, strings.Join(card.lines, "\n"), width, len(card.lines))
		}
	}
}
