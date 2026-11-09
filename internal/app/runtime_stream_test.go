package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"
)

func runtimeWebSocket(t *testing.T, w http.ResponseWriter, r *http.Request) *websocket.Conn {
	t.Helper()
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		t.Error(err)
		return nil
	}
	return conn
}

func writeRuntimeWebSocket(t *testing.T, conn *websocket.Conn, value any) bool {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return conn.Write(context.Background(), websocket.MessageText, raw) == nil
}

func readRuntimeEvent(t *testing.T, events <-chan RuntimeEvent) RuntimeEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(4 * time.Second):
		t.Fatal("runtime event did not arrive")
		return RuntimeEvent{}
	}
}

func runRuntimeStream(t *testing.T, a *App) (<-chan RuntimeEvent, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	events := make(chan RuntimeEvent, 256)
	done := make(chan error, 1)
	go func() {
		done <- a.StreamRuntime(ctx, func(event RuntimeEvent) {
			select {
			case events <- event:
			case <-ctx.Done():
			}
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("runtime stream did not release its readers")
		}
	})
	return events, cancel, done
}

func TestStreamRuntimeSourcesAdvanceIndependentlyAndVersionIsCached(t *testing.T) {
	a, svc := installedApp(t)
	var inspections atomic.Int32
	a.Service = monitorService{Services: svc, inspect: func(ctx context.Context) (service.Status, error) {
		inspections.Add(1)
		return service.Status{Active: true, State: "active", PID: 101}, ctx.Err()
	}}
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	counts := map[string]*atomic.Int32{}
	for _, path := range []string{"/version", "/traffic", "/memory", "/connections", "/logs"} {
		counts[path] = new(atomic.Int32)
	}
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		count := counts[r.URL.Path]
		if count == nil {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			return
		}
		count.Add(1)
		if r.URL.Path == "/version" {
			select {
			case <-release:
				fmt.Fprint(w, `{"version":"v1.19.33"}`)
			case <-r.Context().Done():
			}
			return
		}
		conn := runtimeWebSocket(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		closed := conn.CloseRead(r.Context())
		switch r.URL.Path {
		case "/traffic":
			for index := 1; index <= 3; index++ {
				if !writeRuntimeWebSocket(t, conn, map[string]int{"up": index, "down": index * 2, "upTotal": index * 10, "downTotal": index * 20}) {
					return
				}
			}
		case "/memory":
			writeRuntimeWebSocket(t, conn, map[string]int{"inuse": 0})
			select {
			case <-release:
				writeRuntimeWebSocket(t, conn, map[string]int{"inuse": 65536})
			case <-closed.Done():
				return
			}
		case "/connections":
			writeRuntimeWebSocket(t, conn, map[string]any{"uploadTotal": 10, "downloadTotal": 20, "memory": 0, "connections": []any{}})
		case "/logs":
			writeRuntimeWebSocket(t, conn, map[string]string{"type": "info", "payload": "observed a live event"})
		}
		<-closed.Done()
	})
	events, cancel, _ := runRuntimeStream(t, a)
	first := readRuntimeEvent(t, events)
	if first.Source != RuntimeStatus || first.Instance == 0 || first.Snapshot.State != "ready" {
		t.Fatalf("initial status did not precede every reader: %+v", first)
	}
	traffic := 0
	for traffic < 3 {
		event := readRuntimeEvent(t, events)
		if event.Instance != first.Instance || event.ObservedAt.IsZero() {
			t.Fatalf("source lost its independent timestamp or identity: %+v", event)
		}
		if event.Memory != nil || event.Source == RuntimeVersion {
			t.Fatal("slow source was published before its fixture released it")
		}
		if event.Source == RuntimeTraffic && event.Traffic != nil {
			traffic++
			if event.Traffic == nil || event.Traffic.Up != int64(traffic) {
				t.Fatal("traffic stream dropped or reordered a frame")
			}
		}
	}
	releaseOnce.Do(func() { close(release) })
	gotMemory, gotVersion := false, false
	for !gotMemory || !gotVersion {
		event := readRuntimeEvent(t, events)
		if event.Source == RuntimeMemory && event.Err == nil {
			gotMemory = event.Memory != nil && *event.Memory == 65536
		}
		if event.Source == RuntimeVersion && event.Err == nil {
			gotVersion = event.Snapshot.RunningVersion == "v1.19.33"
		}
	}
	// Let two ownership checks run. Neither periodic status checking nor quiet
	// logs should reopen a source or request the same instance's version again.
	deadline := time.Now().Add(3 * time.Second)
	for inspections.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	for path, count := range counts {
		if count.Load() != 1 {
			t.Errorf("%s opened %d requests for one instance", path, count.Load())
		}
	}
	if checks := inspections.Load(); checks < 4 || checks > 5 {
		t.Fatalf("ownership checks were duplicated per endpoint: %d", checks)
	}
	cancel()
}

func TestStreamRuntimeReconnectsOnlyFailedSource(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	var traffic, memory, connections, logs, versions atomic.Int32
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			versions.Add(1)
			fmt.Fprint(w, `{"version":"v1.19.33"}`)
			return
		}
		conn := runtimeWebSocket(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		closed := conn.CloseRead(r.Context())
		switch r.URL.Path {
		case "/traffic":
			query := traffic.Add(1)
			writeRuntimeWebSocket(t, conn, map[string]int32{"up": query, "down": query, "upTotal": query, "downTotal": query})
			if query == 1 {
				return
			}
		case "/memory":
			memory.Add(1)
			writeRuntimeWebSocket(t, conn, map[string]int{"inuse": 0})
			writeRuntimeWebSocket(t, conn, map[string]int{"inuse": 10})
		case "/connections":
			connections.Add(1)
			writeRuntimeWebSocket(t, conn, map[string]any{"uploadTotal": 0, "downloadTotal": 0, "connections": []any{}})
		case "/logs":
			logs.Add(1)
		}
		<-closed.Done()
	})
	events, cancel, _ := runRuntimeStream(t, a)
	var instance uint64
	gotGap, gotRecovery := false, false
	for !gotRecovery {
		event := readRuntimeEvent(t, events)
		if event.Source == RuntimeStatus {
			instance = event.Instance
		}
		if event.Source != RuntimeTraffic {
			continue
		}
		if event.Instance != instance {
			t.Fatal("endpoint reconnect changed core instance")
		}
		gotGap = gotGap || event.Err != nil
		gotRecovery = event.Traffic != nil && event.Traffic.Up == 2
	}
	if !gotGap || traffic.Load() != 2 || memory.Load() != 1 || connections.Load() != 1 || logs.Load() != 1 || versions.Load() != 1 {
		t.Fatalf("reconnect restarted unrelated readers: traffic=%d memory=%d connections=%d logs=%d versions=%d gap=%t", traffic.Load(), memory.Load(), connections.Load(), logs.Load(), versions.Load(), gotGap)
	}
	cancel()
}

func TestStreamRuntimePIDChangeJoinsOldReadersBeforeNewInstance(t *testing.T) {
	a, svc := installedApp(t)
	var pid atomic.Int32
	pid.Store(101)
	a.Service = monitorService{Services: svc, inspect: func(ctx context.Context) (service.Status, error) {
		return service.Status{Active: true, State: "active", PID: int(pid.Load())}, ctx.Err()
	}}
	var active, opened, versions atomic.Int32
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			versions.Add(1)
			fmt.Fprint(w, `{"version":"v1.19.33"}`)
			return
		}
		conn := runtimeWebSocket(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		closed := conn.CloseRead(r.Context())
		active.Add(1)
		opened.Add(1)
		defer active.Add(-1)
		if r.URL.Path == "/traffic" {
			value := pid.Load()
			writeRuntimeWebSocket(t, conn, map[string]int32{"up": value, "down": value, "upTotal": value, "downTotal": value})
		}
		<-closed.Done()
	})
	events, cancel, _ := runRuntimeStream(t, a)
	var initial uint64
	gotTraffic, gotVersion := false, false
	for !gotTraffic || !gotVersion {
		event := readRuntimeEvent(t, events)
		if event.Source == RuntimeStatus {
			initial = event.Instance
		}
		if event.Source == RuntimeTraffic && event.Traffic != nil {
			gotTraffic = true
		}
		if event.Source == RuntimeVersion && event.Err == nil {
			gotVersion = true
		}
	}
	pid.Store(202)
	changed := false
	for {
		event := readRuntimeEvent(t, events)
		if event.Source == RuntimeStatus && event.Instance > initial {
			if event.Snapshot.Service.PID != 202 {
				t.Fatalf("new instance did not describe the new process: %+v", event)
			}
			changed = true
		}
		if !changed {
			continue
		}
		if event.Instance <= initial {
			t.Fatal("old reader event survived instance invalidation")
		}
		if event.Source == RuntimeTraffic && event.Traffic != nil {
			if event.Traffic.Up != 202 {
				t.Fatal("new instance retained old traffic")
			}
			break
		}
	}
	deadline := time.Now().Add(time.Second)
	for (opened.Load() < 8 || versions.Load() < 2 || active.Load() != 4) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if opened.Load() != 8 || active.Load() != 4 || versions.Load() != 2 {
		t.Fatalf("old readers or version cache survived restart: open=%d active=%d version=%d", opened.Load(), active.Load(), versions.Load())
	}
	cancel()
}

func TestStreamRuntimeQuietReadersReleaseTheServiceActionLock(t *testing.T) {
	a, svc := installedApp(t)
	var running atomic.Bool
	running.Store(true)
	a.Service = monitorService{Services: svc, inspect: func(ctx context.Context) (service.Status, error) {
		state := "inactive"
		pid := 0
		if running.Load() {
			state = "active"
			pid = 101
		}
		return service.Status{Active: running.Load(), State: state, PID: pid}, ctx.Err()
	}}
	svc.actionHook = func(ctx context.Context, action string) error {
		if action == "stop" {
			running.Store(false)
		}
		return ctx.Err()
	}
	opened := make(chan struct{}, 4)
	closed := make(chan struct{}, 4)
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
		opened <- struct{}{}
		<-conn.CloseRead(r.Context()).Done()
		closed <- struct{}{}
	})
	events, cancel, _ := runRuntimeStream(t, a)
	for range 4 {
		select {
		case <-opened:
		case <-time.After(time.Second):
			t.Fatal("runtime source did not connect")
		}
	}
	stopCtx, stop := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer stop()
	if err := a.Control(stopCtx, "stop"); err != nil {
		t.Fatalf("quiet realtime readers held the service action lock: %v", err)
	}
	for {
		event := readRuntimeEvent(t, events)
		if event.Source == RuntimeStatus && event.Snapshot.State == "stopped" {
			break
		}
	}
	for range 4 {
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatal("stopped service retained a live reader")
		}
	}
	cancel()
}

func TestStreamRuntimeContinuesFramesWhileOwnershipCheckWaitsForTheActionLock(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	var subscriptions atomic.Int32
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
		subscriptions.Add(1)
		closed := conn.CloseRead(r.Context())
		if r.URL.Path == "/logs" {
			<-closed.Done()
			return
		}
		var payload any
		switch r.URL.Path {
		case "/traffic":
			payload = map[string]int{"up": 1, "down": 2, "upTotal": 10, "downTotal": 20}
		case "/memory":
			writeRuntimeWebSocket(t, conn, map[string]int{"inuse": 0})
			payload = map[string]int{"inuse": 65536}
		case "/connections":
			payload = map[string]any{"uploadTotal": 10, "downloadTotal": 20, "connections": []any{}}
		}
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-closed.Done():
				return
			case <-ticker.C:
				if !writeRuntimeWebSocket(t, conn, payload) {
					return
				}
			}
		}
	})
	events, cancel, _ := runRuntimeStream(t, a)
	for event := readRuntimeEvent(t, events); event.Traffic == nil; event = readRuntimeEvent(t, events) {
	}
	unlock, err := store.Lock(t.Context(), a.Home)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	lockedAt := time.Now()
	// The shared ownership watcher runs after one second and has to wait for
	// this foreground operation's lock. Wait past the former eight-second
	// inspection deadline: a busy action must neither delay live frames nor
	// invalidate/rebuild the subscriptions.
	timeout := time.NewTimer(11 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case event := <-events:
			if event.Source == RuntimeStatus && event.Snapshot.State != "ready" {
				t.Fatalf("foreground lock wait invalidated healthy subscriptions: %+v", event)
			}
			if event.Traffic == nil || event.ObservedAt.Before(lockedAt.Add(9200*time.Millisecond)) {
				continue
			}
			if lag := time.Since(event.ObservedAt); lag > 150*time.Millisecond {
				t.Fatalf("pending ownership inspection delayed traffic by %s", lag)
			}
			if subscriptions.Load() != 4 {
				t.Fatalf("long foreground action rebuilt subscriptions: %d handshakes", subscriptions.Load())
			}
			cancel()
			return
		case <-timeout.C:
			t.Fatal("service inspection held up live frames while waiting for a foreground operation")
		}
	}
}

func TestStreamRuntimeUnavailableServiceDoesNotOpenControllerAndCancels(t *testing.T) {
	a, _ := installedApp(t)
	var queries atomic.Int32
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) { queries.Add(1); w.WriteHeader(500) })
	ctx, cancel := context.WithCancel(t.Context())
	var status RuntimeEvent
	err := a.StreamRuntime(ctx, func(event RuntimeEvent) { status = event; cancel() })
	if !errors.Is(err, context.Canceled) || status.Source != RuntimeStatus || status.Snapshot.State != "stopped" || queries.Load() != 0 {
		t.Fatalf("unavailable service opened controller or lost cancellation: %+v %v %d", status, err, queries.Load())
	}
}

func TestStreamRuntimeRemembersFirstRunOnlyAfterAuthenticatedAPIObservation(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprint(authenticated), func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = true
			before, err := a.ReadPreferences(t.Context())
			if err != nil || before.HasRun {
				t.Fatalf("fixture already remembered a running core: %+v %v", before, err)
			}
			monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/version" && authenticated {
					fmt.Fprint(w, `{"version":"v1.19.33"}`)
					return
				}
				w.WriteHeader(http.StatusBadGateway)
			})
			events, cancel, _ := runRuntimeStream(t, a)
			for {
				event := readRuntimeEvent(t, events)
				if event.Source != RuntimeVersion {
					continue
				}
				if (event.Err == nil) != authenticated {
					t.Fatalf("unexpected version authentication outcome: %+v", event)
				}
				break
			}
			after, err := a.ReadPreferences(t.Context())
			if err != nil || after.HasRun != authenticated {
				t.Fatalf("service active state replaced actual API observation: hasRun=%t want=%t err=%v", after.HasRun, authenticated, err)
			}
			cancel()
		})
	}
}
