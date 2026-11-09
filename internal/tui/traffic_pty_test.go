package tui

import (
	"context"
	"encoding/json"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// TestTrafficPTYFixture observes the real TUI against a changing memory backend.
// Samples use actual receipt times; no graph history is seeded or accelerated.
func TestTrafficPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_TRAFFIC_PTY_REPORT")
	if report == "" {
		t.Skip("explicit traffic PTY report path required")
	}
	language := os.Getenv("LAZYCLASH_TRAFFIC_PTY_LANGUAGE")
	if language == "" {
		language = "en"
	}
	b := &trafficPTYBackend{overviewPTYBackend: newOverviewPTYBackend("running"), report: report,
		scenario: os.Getenv("LAZYCLASH_TRAFFIC_PTY_SCENARIO")}
	b.pid.Store(4242)
	if err := Run(context.Background(), b, language); err != nil {
		t.Fatal(err)
	}
	if err := b.writeReport(report, "finished"); err != nil {
		t.Fatal(err)
	}
}

type trafficPTYBackend struct {
	*overviewPTYBackend
	report, scenario string
	pid              atomic.Int64
	missing          atomic.Int64
	observations     atomic.Int64
}

func (b *trafficPTYBackend) Snapshot(ctx context.Context) (app.Snapshot, error) {
	s := b.fakeBackend.snapshot
	s.Service.PID = int(b.pid.Load())
	return s, ctx.Err()
}

func (b *trafficPTYBackend) Runtime(ctx context.Context) (app.RuntimeSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return app.RuntimeSnapshot{}, err
	}
	query := b.runtimeQueries.Add(1)
	up := [...]int64{128 << 10, 256 << 10, 192 << 10, 512 << 10, 320 << 10, 160 << 10}
	down := [...]int64{2 << 20, 3 << 20, 4 << 20, 2 << 20, 5 << 20, 3 << 20}
	s := liveRuntime()
	s.Service, s.RunningVersion, s.ObservedAt = b.fakeBackend.snapshot.Service, b.fakeBackend.snapshot.RunningVersion, time.Now()
	s.Service.PID = int(b.pid.Load())
	s.Traffic = &mihomo.Traffic{Up: up[(query-1)%int64(len(up))], Down: down[(query-1)%int64(len(down))]}
	s.Connections.Connections = []mihomo.Connection{{ID: "traffic-1"}, {ID: "traffic-2"}, {ID: "traffic-3"}}
	s.Connections.UploadTotal += query * (256 << 10)
	s.Connections.DownloadTotal += query * (2 << 20)
	phase := "collecting"
	if query >= 5 {
		phase = "ready"
	}
	if b.scenario == "zero" {
		// Zero remains a valid observation, with cumulative counters intact.
		s.Traffic = &mihomo.Traffic{}
		s.Connections.UploadTotal = 0
		s.Connections.DownloadTotal = 0
	}
	if b.scenario == "gap" && query >= 4 && query <= 6 {
		s.Traffic = nil
		b.missing.Add(1)
		phase = "gap"
	} else if b.scenario == "gap" && query > 6 {
		phase = "recovered"
	}
	if b.scenario == "restart" && query >= 7 {
		b.pid.Store(4243)
		s.Service.PID = 4243
		s.Traffic = &mihomo.Traffic{Up: 16 << 10, Down: 100 << 10}
		s.Connections.UploadTotal = (query - 7) * (16 << 10)
		s.Connections.DownloadTotal = (query - 7) * (100 << 10)
		phase = "restarted"
	}
	if s.Traffic != nil {
		s.Traffic.UpTotal, s.Traffic.DownTotal = s.Connections.UploadTotal, s.Connections.DownloadTotal
	}
	b.observations.Store(time.Now().UnixNano())
	return s, b.writeReport(b.report+".progress", phase)
}

func (b *trafficPTYBackend) writeReport(path, phase string) error {
	raw, err := json.Marshal(map[string]any{
		"phase": phase, "scenario": b.scenario, "pid": b.pid.Load(),
		"runtime_queries": b.runtimeQueries.Load(), "missing_samples": b.missing.Load(),
		"observed_at_unix_nano": b.observations.Load(), "service_changes": b.controls.Load(),
		"modes": b.modes.Load(), "selections": b.selections.Load(), "tun_changes": b.tunChanges.Load(),
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", raw, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func (b *trafficPTYBackend) StreamRuntime(ctx context.Context, emit func(app.RuntimeEvent)) error {
	return streamRuntimeFixture(ctx, b, emit)
}
