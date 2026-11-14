package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

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
	for _, label := range []string{"↑ Upload", "↓ Download", "Connections", "Core memory", "Total upload", "Total download"} {
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
