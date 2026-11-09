package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

// TestConnectionsPTYFixture runs the real TUI against an explicitly enabled
// memory backend; it never contacts a core, reads managed data or changes services.
func TestConnectionsPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_CONNECTIONS_PTY_REPORT")
	if report == "" {
		t.Skip("explicit Connections PTY report path required")
	}
	b := newConnectionsPTYBackend()
	b.queryReport = report + ".runtime"
	language := os.Getenv("LAZYCLASH_CONNECTIONS_PTY_LANGUAGE")
	if language == "" {
		language = "en"
	}
	if err := Run(context.Background(), b, language); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	raw, err := json.Marshal(map[string]any{
		"runtime_queries": b.queries.Load(), "closes": len(b.closedIDs), "closed_ids": b.closedIDs,
		"service_changes": b.controls.Load(), "modes": b.modes.Load(), "selections": b.selections.Load(),
		"connections": len(b.entries),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

type connectionsPTYBackend struct {
	*fakeBackend
	mu                                   sync.Mutex
	entries                              []mihomo.Connection
	closedIDs                            []string
	queryReport                          string
	queries, controls, modes, selections atomic.Int64
}

func newConnectionsPTYBackend() *connectionsPTYBackend {
	b := &connectionsPTYBackend{fakeBackend: &fakeBackend{snapshot: app.Snapshot{
		Installed: true, ConfigReady: true, HasRun: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
		Applied: "connections-pty", AppliedName: "Connections preview", Generation: "connections-pty-generation",
		Service:      service.Status{Active: true, State: "active", PID: 4242},
		Mode:         app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
		Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
	}}}
	start := time.Now().Add(-time.Minute)
	for i := range 48 {
		host, process, payload := fmt.Sprintf("flow-%02d.example.invalid", i), "worker", "cn_domain"
		chain := []string{"DIRECT"}
		if i%2 == 0 {
			chain = []string{"HK 01", "PROXY"}
		}
		switch i {
		case 0:
			host, process, payload = "api.github.com", "github-agent", "github"
		case 1:
			host, process, payload = "www.youtube.com", "browser", "streaming"
		case 2:
			host, process = "中文.example.invalid", "中文进程"
		case 3:
			host, process = "", "ipv6-agent"
		}
		network, inbound := "tcp", "HTTP"
		if i%3 == 2 {
			network, inbound = "udp", "TUN"
		}
		b.entries = append(b.entries, mihomo.Connection{
			ID: fmt.Sprintf("pty-connection-%02d", i),
			Metadata: mihomo.ConnectionMetadata{
				Host: host, DestinationIP: "2001:db8::42", DestinationPort: "443",
				SourceIP: "127.0.0.1", SourcePort: fmt.Sprint(42000 + i),
				Network: network, Type: inbound, Process: process, ProcessPath: "/fixture/" + process,
			},
			Start: start.Add(-time.Duration(i) * time.Minute), Upload: int64(i+1) * 8192,
			Download: int64(i+1) * 65536, Chains: chain, Rule: "RuleSet", RulePayload: payload,
		})
	}
	return b
}

func (b *connectionsPTYBackend) Runtime(ctx context.Context) (app.RuntimeSnapshot, error) {
	query := b.queries.Add(1)
	if b.queryReport != "" {
		if err := os.WriteFile(b.queryReport, []byte(strconv.FormatInt(query, 10)), 0600); err != nil {
			return app.RuntimeSnapshot{}, err
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.entries {
		b.entries[i].Upload += int64(i+1) * 512
		b.entries[i].Download += int64(i+1) * 4096
	}
	memory := uint64(32 * 1024 * 1024)
	return app.RuntimeSnapshot{
		State: "ready", Service: b.snapshot.Service, RunningVersion: b.snapshot.RunningVersion,
		ObservedAt: time.Now(), Traffic: &mihomo.Traffic{Up: 128 * 1024, Down: 2 * 1024 * 1024}, Memory: &memory,
		Connections: &mihomo.ConnectionSnapshot{
			UploadTotal:   128*1024*1024 + b.queries.Load()*8192,
			DownloadTotal: 2*1024*1024*1024 + b.queries.Load()*1024*1024,
			Connections:   append([]mihomo.Connection(nil), b.entries...),
		},
	}, ctx.Err()
}

func (b *connectionsPTYBackend) CloseConnection(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, connection := range b.entries {
		if connection.ID == id {
			b.entries = append(b.entries[:i], b.entries[i+1:]...)
			b.closedIDs = append(b.closedIDs, id)
			return nil
		}
	}
	return fmt.Errorf("fixture connection is no longer active")
}

func (b *connectionsPTYBackend) Control(ctx context.Context, _ string) error {
	b.controls.Add(1)
	return ctx.Err()
}

func (b *connectionsPTYBackend) SetMode(ctx context.Context, _ string) error {
	b.modes.Add(1)
	return ctx.Err()
}

func (b *connectionsPTYBackend) Select(ctx context.Context, _, _ string) error {
	b.selections.Add(1)
	return ctx.Err()
}

func (b *connectionsPTYBackend) StreamRuntime(ctx context.Context, emit func(app.RuntimeEvent)) error {
	return streamRuntimeFixture(ctx, b, emit)
}
