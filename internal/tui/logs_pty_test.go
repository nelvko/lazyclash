package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

// TestLogsPTYFixture exercises the real terminal program with synthetic events.
// It never opens managed storage, connects to a core or changes a service.
func TestLogsPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_LOGS_PTY_REPORT")
	if report == "" {
		t.Skip("explicit Logs PTY report path required")
	}
	b := &logsPTYBackend{
		fakeBackend: &fakeBackend{snapshot: app.Snapshot{
			Installed: true, ConfigReady: true, HasRun: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
			Applied: "logs-pty", AppliedName: "Logs preview", Generation: "logs-pty-generation",
			Service:      service.Status{Active: true, State: "active", PID: 4242},
			Mode:         app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
			Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
		}},
		report: report,
	}
	language := os.Getenv("LAZYCLASH_LOGS_PTY_LANGUAGE")
	if language == "" {
		language = "en"
	}
	if err := Run(context.Background(), b, language); err != nil {
		t.Fatal(err)
	}
	b.streamTasks.Wait()
	b.writeReport()
}

type logsPTYBackend struct {
	*fakeBackend
	report                                                  string
	reportMu                                                sync.Mutex
	streamTasks                                             sync.WaitGroup
	streams, canceled, emitted, controls, modes, selections atomic.Int64
}

func (b *logsPTYBackend) writeReport() {
	b.reportMu.Lock()
	defer b.reportMu.Unlock()
	raw, _ := json.Marshal(map[string]int64{
		"streams": b.streams.Load(), "canceled": b.canceled.Load(), "emitted": b.emitted.Load(),
		"service_changes": b.controls.Load(), "modes": b.modes.Load(), "selections": b.selections.Load(),
	})
	file, err := os.CreateTemp(filepath.Dir(b.report), ".calls-*")
	if err != nil {
		return
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return
	}
	if file.Close() == nil {
		_ = os.Rename(name, b.report)
	}
}

func (b *logsPTYBackend) StreamLogs(ctx context.Context, emit func(mihomo.LogEntry)) error {
	b.streamTasks.Add(1)
	defer b.streamTasks.Done()
	stream := b.streams.Add(1)
	b.writeReport()
	defer func() {
		b.canceled.Add(1)
		b.writeReport()
	}()
	entry := func() {
		index := b.emitted.Add(1) - 1
		level := []string{"info", "warning", "error"}[index%3]
		message := fmt.Sprintf("event-%03d %s-MARKER [TCP] 127.0.0.1:%d --> service-%03d.example.invalid:443 match RuleSet(ai) using 默认代理[东京]", index, strings.ToUpper(level), 32000+index, index)
		if index == 54 {
			message += " Case-Needle"
		}
		if index == 57 {
			message += " " + strings.Repeat("中文长日志/", 24) + " LONG-MESSAGE-END"
		}
		emit(mihomo.LogEntry{Time: time.Now(), Level: level, Message: message})
		b.writeReport()
	}
	if stream == 1 {
		for index := 0; index < 60; index++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			entry()
		}
	} else {
		entry()
	}
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			entry()
		}
	}
}

func (b *logsPTYBackend) Runtime(ctx context.Context) (app.RuntimeSnapshot, error) {
	return app.RuntimeSnapshot{State: "ready", Service: b.snapshot.Service, RunningVersion: b.snapshot.RunningVersion}, ctx.Err()
}

func (b *logsPTYBackend) Control(ctx context.Context, _ string) error {
	b.controls.Add(1)
	return ctx.Err()
}

func (b *logsPTYBackend) SetMode(ctx context.Context, _ string) error {
	b.modes.Add(1)
	return ctx.Err()
}

func (b *logsPTYBackend) Select(ctx context.Context, _, _ string) error {
	b.selections.Add(1)
	return ctx.Err()
}
