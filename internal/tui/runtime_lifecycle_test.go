package tui

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestRuntimeUnexpectedSessionEndInvalidatesMetricsAndRetriesWithoutSpinning(t *testing.T) {
	for _, failure := range []error{errors.New("session failed"), nil} {
		t.Run(map[bool]string{true: "error", false: "clean unexpected end"}[failure != nil], func(t *testing.T) {
			var calls atomic.Int64
			b := &fakeBackend{streamRuntime: func(context.Context, func(app.RuntimeEvent)) error { calls.Add(1); return failure }}
			m := runtimeModel(t, b)
			defer m.stopRuntime()
			commands := commandBatch(t, m.syncRuntime())
			sample := liveRuntime()
			sample.ObservedAt = time.Now().Add(-time.Second)
			m = acceptRuntimeFixture(m, sample)
			m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "retained"}})
			ended, ok := commands[0]().(runtimeEndedMsg)
			if !ok {
				t.Fatal("outer backend return did not produce a session-ended message")
			}
			retry := m.acceptRuntimeEnded(ended)
			if retry == nil || m.runtime.active || m.runtime.pending || m.runtime.snapshot.State != "unavailable" || m.runtime.snapshot.Traffic != nil || m.runtime.snapshot.Memory != nil || m.runtime.snapshot.Connections != nil {
				t.Fatal("ended session retained live metrics or failed to schedule recovery")
			}
			points := m.runtime.history.points
			if len(points) != 2 || points[1].Valid || len(m.logs.entries) != 1 || !m.logs.reconnecting || m.logs.err == nil {
				t.Fatal("ended session fabricated zero, erased history, or lost its log diagnostic")
			}
			for range 8 {
				m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
				if m.runtime.active || m.syncRuntime() != nil || calls.Load() != 1 {
					t.Fatal("generic Update restarted a failed session before its retry deadline")
				}
			}
			retryMsg, ok := retry().(runtimeRetryMsg)
			if !ok {
				t.Fatal("recovery timer did not preserve its generation identity")
			}
			m, _ = update(m, retryMsg)
			if !m.runtime.active || !m.runtime.pending || m.runtime.generation == ended.generation {
				t.Fatal("guarded retry did not establish a new shared session")
			}
			generation := m.runtime.generation
			m, _ = update(m, ended)
			if !m.runtime.active || m.runtime.generation != generation {
				t.Fatal("old session-ended message interrupted its replacement")
			}
		})
	}
}

func TestRuntimeManualRetryWaitsForOldReadersToFinishBeforeStartingReplacement(t *testing.T) {
	started := make(chan int64, 2)
	releaseOld := make(chan struct{})
	var release sync.Once
	releaseCleanup := func() { release.Do(func() { close(releaseOld) }) }
	defer releaseCleanup()
	var calls, active atomic.Int64
	var overlap atomic.Bool
	b := &fakeBackend{streamRuntime: func(ctx context.Context, emit func(app.RuntimeEvent)) error {
		call := calls.Add(1)
		if active.Add(1) != 1 {
			overlap.Store(true)
		}
		defer active.Add(-1)
		started <- call
		<-ctx.Done()
		if call == 1 {
			<-releaseOld
		}
		return ctx.Err()
	}}
	m := runtimeModel(t, b)
	defer m.stopRuntime()
	first := commandBatch(t, m.syncRuntime())
	firstDone := make(chan tea.Msg, 1)
	go func() { firstDone <- first[0]() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("initial shared session did not start")
	}
	second := commandBatch(t, m.retryRuntime())
	// A second retry can cancel an unexecuted replacement. Its cancellation
	// must not let a third session overtake the first owner's slow cleanup.
	third := commandBatch(t, m.retryRuntime())
	if second[0]() != nil {
		t.Fatal("cancelled queued replacement executed a backend")
	}
	secondDone := make(chan tea.Msg, 1)
	go func() { secondDone <- third[0]() }()
	select {
	case <-started:
		t.Fatal("replacement opened sources before old readers finished cleanup")
	case <-time.After(40 * time.Millisecond):
	}
	releaseCleanup()
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("old shared session did not finish")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("replacement remained blocked after old readers finished")
	}
	if overlap.Load() || calls.Load() != 2 {
		t.Fatal("manual retry created duplicate source owners")
	}
	m.stopRuntime()
	select {
	case result := <-secondDone:
		if result != nil {
			t.Fatal("cancelled replacement reported an unexpected session failure")
		}
	case <-time.After(time.Second):
		t.Fatal("replacement did not cancel")
	}
}

func TestRuntimeCancelBeforeCommandExecutionDoesNotBlockReplacement(t *testing.T) {
	started := make(chan struct{}, 1)
	var calls atomic.Int64
	b := &fakeBackend{streamRuntime: func(ctx context.Context, emit func(app.RuntimeEvent)) error {
		calls.Add(1)
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}}
	m := runtimeModel(t, b)
	defer m.stopRuntime()
	unexecuted := commandBatch(t, m.syncRuntime())
	replacement := commandBatch(t, m.retryRuntime())
	done := make(chan tea.Msg, 1)
	go func() { done <- replacement[0]() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("unexecuted cancelled command blocked its replacement")
	}
	if unexecuted[0]() != nil || unexecuted[1]() != nil || calls.Load() != 1 {
		t.Fatal("cancelled command started another owner when eventually executed")
	}
	m.stopRuntime()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("replacement did not finish after cancellation")
	}
}
