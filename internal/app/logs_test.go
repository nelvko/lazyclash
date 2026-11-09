package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestLogsUnavailableStatesDoNotOpenAPIOrChangeService(t *testing.T) {
	for _, state := range []string{"uninstalled", "stopped", "unsupported", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			a, svc := testApp(t)
			if state != "uninstalled" {
				if err := a.Install(t.Context(), "", nil); err != nil {
					t.Fatal(err)
				}
			}
			if state == "unsupported" {
				a.Service, a.PlatformError = nil, errors.New("unsupported fixture")
			}
			if state == "unavailable" {
				svc.verifyErr = errors.New("private-service-error")
			}
			var calls atomic.Int32
			monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
			err := a.StreamLogs(t.Context(), func(mihomo.LogEntry) { t.Error("unavailable core emitted logs") })
			if !errors.Is(err, ErrLogsNotReady) || calls.Load() != 0 || len(svc.actions) != 0 || strings.Contains(err.Error(), "private-") {
				t.Fatalf("bad unavailable outcome: %v", err)
			}
		})
	}
}

func TestLogsAreReadOnlyAndParentCancellationClosesLiveConnection(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	before, err := installationTree(filepath.Join(a.Home, "installation"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, _, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	started, finished := make(chan struct{}), make(chan struct{})
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			fmt.Fprint(w, `{"version":"v1.19.33"}`)
			return
		}
		if r.URL.Path != "/logs" {
			t.Error("unexpected endpoint")
		}
		conn := runtimeWebSocket(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		close(started)
		defer close(finished)
		writeRuntimeWebSocket(t, conn, map[string]string{"type": "info", "payload": "current secret " + manifest.Secret})
		<-conn.CloseRead(r.Context()).Done()
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var entries []mihomo.LogEntry
	err = a.StreamLogs(ctx, func(entry mihomo.LogEntry) { entries = append(entries, entry); cancel() })
	<-started
	if !errors.Is(err, context.Canceled) || len(entries) != 1 || strings.Contains(entries[0].Message, manifest.Secret) {
		t.Fatalf("bad live observation: %+v %v", entries, err)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("stream left open")
	}
	after, err := installationTree(filepath.Join(a.Home, "installation"))
	if err != nil || !maps.Equal(before, after) || len(svc.actions) != 0 || svc.installs != 1 {
		t.Fatal("log observation modified installation or service", err)
	}
}

func TestLogsDetectChangedCoreEvenWhileStreamIsQuiet(t *testing.T) {
	a, svc := installedApp(t)
	var changed atomic.Bool
	a.Service = monitorService{Services: svc, inspect: func(ctx context.Context) (service.Status, error) {
		pid := 101
		if changed.Load() {
			pid = 202
		}
		return service.Status{State: "active", Active: true, PID: pid}, ctx.Err()
	}}
	started := make(chan struct{})
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			fmt.Fprint(w, `{"version":"v1.19.33"}`)
			return
		}
		conn := runtimeWebSocket(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		close(started)
		<-conn.CloseRead(r.Context()).Done()
	})
	done := make(chan error, 1)
	go func() {
		done <- a.StreamLogs(t.Context(), func(mihomo.LogEntry) { t.Error("quiet stream emitted a record") })
	}()
	<-started
	changed.Store(true)
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "Core changed") {
			t.Fatalf("old process stream survived: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("quiet stream did not recheck ownership and process")
	}
}

func TestQuietLogsDoNotHoldServiceActionLock(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	started := make(chan struct{})
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			fmt.Fprint(w, `{"version":"v1.19.33"}`)
			return
		}
		conn := runtimeWebSocket(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		close(started)
		<-conn.CloseRead(r.Context()).Done()
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.StreamLogs(ctx, func(mihomo.LogEntry) {}) }()
	<-started
	stopCtx, stop := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer stop()
	if err := a.Control(stopCtx, "stop"); err != nil {
		t.Fatalf("log stream held service lock: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not cancel")
	}
}
