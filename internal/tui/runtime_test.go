package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func runtimeModel(t *testing.T, b *fakeBackend) Model {
	t.Helper()
	b.snapshot.Installed, b.snapshot.ConfigReady = true, true
	b.snapshot.Service = service.Status{Active: true, State: "active"}
	m := New(t.Context(), b)
	m.snapshot = b.snapshot
	m.busy, m.job = false, ""
	m.width, m.height = 120, 32
	m.layout()
	return m
}

func liveRuntime() app.RuntimeSnapshot {
	memory := uint64(16 << 20)
	return app.RuntimeSnapshot{State: "ready", Service: service.Status{Active: true, State: "active"}, RunningVersion: "v1.19.33", ObservedAt: time.Now(), Traffic: &mihomo.Traffic{Up: 2048, Down: 4096}, Connections: &mihomo.ConnectionSnapshot{UploadTotal: 1 << 20, DownloadTotal: 2 << 20, Connections: []mihomo.Connection{}}, Memory: &memory}
}

func TestRuntimeSamplesAreIndependentOfBusyJobsAndResize(t *testing.T) {
	calls := 0
	b := &fakeBackend{runtime: func(context.Context) (app.RuntimeSnapshot, error) { calls++; return liveRuntime(), nil }}
	m := runtimeModel(t, b)
	cmd := m.syncRuntime()
	if cmd == nil || m.busy || !m.runtime.pending || m.syncRuntime() != nil {
		t.Fatal("sampling must have exactly one independent pending operation")
	}
	id := m.runtime.generation
	for _, size := range [][2]int{{80, 24}, {20, 7}, {160, 45}} {
		var next tea.Cmd
		m, next = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if next != nil || m.runtime.generation != id || !m.runtime.pending {
			t.Fatal("resize restarted a running sample")
		}
	}
	result := cmd().(runtimeResultMsg)
	m, next := update(m, result)
	if calls != 1 || !m.runtime.received || m.runtime.pending || next == nil || m.busy {
		t.Fatalf("sample did not complete and schedule its next tick: calls=%d", calls)
	}
	if m.snapshot.RunningVersion != "v1.19.33" || !m.snapshot.HasRun {
		t.Fatal("observed running core did not update dashboard history/version")
	}
}

func TestRuntimeRetryAndTicksRejectOldSamples(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	old := m.syncRuntime()
	oldID := m.runtime.generation
	newCmd := m.retryRuntime()
	newID := m.runtime.generation
	if oldID == newID || newCmd == nil {
		t.Fatal("retry must create a new task identity")
	}
	m, _ = update(m, old())
	if !m.runtime.pending || m.runtime.received || m.runtime.generation != newID {
		t.Fatal("old sample replaced retry")
	}
	m, _ = update(m, runtimeResultMsg{generation: newID, snapshot: liveRuntime()})
	m, next := update(m, runtimeTickMsg{generation: newID})
	if next == nil || !m.runtime.pending || m.runtime.generation == newID {
		t.Fatal("next tick did not start a distinct sample")
	}
	current := m.runtime.generation
	m, duplicate := update(m, runtimeTickMsg{generation: newID})
	if duplicate != nil || m.runtime.generation != current {
		t.Fatal("duplicate tick started another sample")
	}
	m, _ = update(m, runtimeResultMsg{generation: newID, snapshot: app.RuntimeSnapshot{State: "stopped"}})
	if !m.runtime.pending || m.runtime.snapshot.State != "ready" {
		t.Fatal("late earlier sample canceled or overwrote current sample")
	}
}

func TestRuntimeCancelsRequestsWhenLeavingOrOpeningModal(t *testing.T) {
	for _, action := range []string{"2", "i", "n", "a", "s", "q", "ctrl+c"} {
		t.Run(action, func(t *testing.T) {
			started := make(chan context.Context, 1)
			finished := make(chan tea.Msg, 1)
			b := &fakeBackend{runtime: func(ctx context.Context) (app.RuntimeSnapshot, error) {
				started <- ctx
				<-ctx.Done()
				return app.RuntimeSnapshot{}, ctx.Err()
			}}
			m := runtimeModel(t, b)
			cmd := m.syncRuntime()
			go func() { finished <- cmd() }()
			var requestCtx context.Context
			select {
			case requestCtx = <-started:
			case <-time.After(time.Second):
				t.Fatal("sample did not start")
			}
			k := key(action)
			if action == "ctrl+c" {
				k = ctrlC()
			}
			m, _ = update(m, k)
			if requestCtx.Err() == nil || m.runtime.active {
				t.Fatal("background request survived leaving its visible context")
			}
			select {
			case late := <-finished:
				m, _ = update(m, late)
				if m.runtime.received {
					t.Fatal("late canceled request was accepted")
				}
			case <-time.After(time.Second):
				t.Fatal("request did not observe cancellation")
			}
		})
	}
}

func TestRuntimeFailuresContinueRetryingAndDoNotShowStaleData(t *testing.T) {
	for _, failure := range []error{errors.New("controller unavailable"), context.Canceled, context.DeadlineExceeded} {
		m := runtimeModel(t, &fakeBackend{})
		m.runtime.snapshot = liveRuntime()
		m.syncRuntime()
		m, next := update(m, runtimeResultMsg{generation: m.runtime.generation, err: failure})
		if next == nil || !m.runtime.received || m.runtime.snapshot.State != "unavailable" || m.runtime.snapshot.Connections != nil {
			t.Fatalf("failure stopped retrying or retained old observations: %v", failure)
		}
	}
}

func TestRuntimeStoppedAndRestartedCountersReplacePreviousValues(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	m.syncRuntime()
	m, _ = update(m, runtimeResultMsg{generation: m.runtime.generation, snapshot: liveRuntime()})
	id := m.runtime.generation
	m, _ = update(m, runtimeTickMsg{generation: id})
	m, _ = update(m, runtimeResultMsg{generation: m.runtime.generation, snapshot: app.RuntimeSnapshot{State: "stopped", Service: service.Status{State: "inactive"}}})
	if m.snapshot.Service.Active || m.snapshot.RunningVersion != "" || !m.snapshot.HasRun {
		t.Fatal("stopped core retained its running version or lost setup history")
	}
	m, _ = update(m, runtimeTickMsg{generation: m.runtime.generation})
	fresh := liveRuntime()
	fresh.Connections.UploadTotal, fresh.Connections.DownloadTotal = 0, 0
	m, _ = update(m, runtimeResultMsg{generation: m.runtime.generation, snapshot: fresh})
	if m.runtime.snapshot.Connections.UploadTotal != 0 || m.runtime.snapshot.Connections.DownloadTotal != 0 {
		t.Fatal("counters were accumulated across core restarts")
	}
}

func TestRuntimeCardDistinguishesMissingValuesFromZero(t *testing.T) {
	o := dashboardOptions{Snapshot: app.Snapshot{Installed: true}, Runtime: liveRuntime(), RuntimeReceived: true}
	card := func() string { return ansi.Strip(strings.Join(overviewCards(o)[2], "\n")) }
	if value := card(); !strings.Contains(value, "Since core start") || !strings.Contains(value, "1.0 MiB") || !strings.Contains(value, "2.0 KiB/s") || !strings.Contains(value, "16.0 MiB") {
		t.Fatalf("actual runtime metrics missing: %s", value)
	}
	o.Runtime.Traffic, o.Runtime.Memory = nil, nil
	if value := card(); !strings.Contains(value, "Partially available") || strings.Contains(value, "0 B/s") || strings.Contains(value, "16.0 MiB") {
		t.Fatalf("missing metric appeared as zero or stale data: %s", value)
	}
	o.Runtime.State = "unavailable"
	if value := card(); strings.Contains(value, "1.0 MiB") || !strings.Contains(value, "API unavailable") {
		t.Fatalf("unavailable runtime displayed old counters: %s", value)
	}
}

func TestRuntimeUnknownServiceStateIsNeverShownAsStopped(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	m.syncRuntime()
	m, _ = update(m, runtimeResultMsg{generation: m.runtime.generation, snapshot: app.RuntimeSnapshot{State: "unavailable", Error: "user manager unavailable"}})
	if m.snapshot.ServiceError == "" || !m.snapshot.Service.Active {
		t.Fatal("unknown service observation replaced the previous state with stopped")
	}
	card := ansi.Strip(strings.Join(overviewCards(dashboardOptions{Snapshot: m.snapshot})[3], "\n"))
	if !strings.Contains(card, "Unavailable") || strings.Contains(card, "Stopped") {
		t.Fatalf("unknown service was presented as stopped: %s", card)
	}
	m, _ = update(m, runtimeTickMsg{generation: m.runtime.generation})
	s := liveRuntime()
	s.State, s.Error = "unavailable", "controller unavailable"
	m, _ = update(m, runtimeResultMsg{generation: m.runtime.generation, snapshot: s})
	if m.snapshot.ServiceError != "" || !m.snapshot.Service.Active {
		t.Fatal("API failure incorrectly hid a confirmed running service")
	}
	group := ansi.Strip(strings.Join(overviewCards(dashboardOptions{Snapshot: m.snapshot})[1], "\n"))
	if !strings.Contains(group, "API unavailable") || strings.Contains(group, "Ready to select") {
		t.Fatal("API failure presented cached proxy groups as ready")
	}
	m, _ = update(m, runtimeTickMsg{generation: m.runtime.generation})
	m, _ = update(m, runtimeResultMsg{generation: m.runtime.generation, snapshot: liveRuntime()})
	if m.snapshot.APIError != "" || m.snapshot.ServiceError != "" {
		t.Fatal("recovered runtime retained its old unavailability diagnostic")
	}
}

func TestRuntimeTransitionRefreshesPersistentIntentAndActiveProfile(t *testing.T) {
	b := &fakeBackend{}
	m := runtimeModel(t, b)
	m.runtime.snapshot = liveRuntime()
	b.snapshot.Desired = app.DesiredState{Known: true, Running: false}
	b.snapshot.Applied = "changed-from-cli"
	b.snapshot.Service = service.Status{State: "inactive"}
	m.syncRuntime()
	m, cmd := update(m, runtimeResultMsg{generation: m.runtime.generation, snapshot: app.RuntimeSnapshot{State: "stopped", Service: service.Status{State: "inactive"}}})
	commands := commandBatch(t, cmd)
	m, _ = update(m, commands[1]())
	if !m.snapshot.Desired.Known || m.snapshot.Desired.Running || m.snapshot.Applied != "changed-from-cli" || m.snapshot.Service.Active {
		t.Fatal("runtime transition retained another client's old intent or active profile")
	}
}
