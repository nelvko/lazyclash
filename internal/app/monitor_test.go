package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

const monitorConnectionID = "4c87089e-0cbc-4a18-bb08-35e0bd073324"
const monitorOtherID = "218f06f1-e157-4ce0-bf7c-d218c1fb06d8"

// Route the private controller requests to a real, randomly allocated HTTP
// fixture. The installation's controller and all host services remain intact.
func monitorFixture(t *testing.T, a *App, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	t.Cleanup(transport.CloseIdleConnections)
	a.HTTP = &http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
		copy := req.Clone(req.Context())
		destination := *req.URL
		destination.Scheme, destination.Host = target.Scheme, target.Host
		copy.URL = &destination
		return transport.RoundTrip(copy)
	})}
}

func monitorConnection(id string) mihomo.Connection {
	return mihomo.Connection{ID: id, Metadata: mihomo.ConnectionMetadata{Network: "tcp", Type: "HTTP", SourceIP: "127.0.0.1", SourcePort: "42123", DestinationIP: "192.0.2.1", DestinationPort: "443", Host: "example.invalid"}, Upload: 7, Download: 11, Start: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), Chains: []string{"DIRECT"}}
}

func monitorResponse(w http.ResponseWriter, r *http.Request, connections mihomo.ConnectionSnapshot) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/version":
		_, _ = fmt.Fprint(w, `{"version":"v1.19.33"}`)
	case "/connections":
		_ = json.NewEncoder(w).Encode(connections)
	case "/traffic":
		_, _ = fmt.Fprintln(w, `{"up":123,"down":456}`)
	case "/memory":
		_, _ = fmt.Fprintln(w, "{\"inuse\":0}\n{\"inuse\":65536}")
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestRuntimeUnavailableStatesNeverContactController(t *testing.T) {
	for _, state := range []string{"uninstalled", "stopped", "unsupported", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			a, svc := testApp(t)
			if state != "uninstalled" {
				if err := a.Install(t.Context(), "", nil); err != nil {
					t.Fatal(err)
				}
			}
			if state == "unsupported" {
				a.Service, a.PlatformError = nil, errors.New("unsupported fixture platform")
			}
			if state == "unavailable" {
				svc.active = true
				svc.verifyErr = errors.New("unit belongs to another installation")
			}
			var calls atomic.Int32
			monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
			got, err := a.Runtime(t.Context())
			if err != nil || got.State != state || got.Traffic != nil || got.Connections != nil || got.Memory != nil {
				t.Fatalf("state=%+v error=%v", got, err)
			}
			if _, err := a.Connections(t.Context()); err == nil {
				t.Fatal("connections accepted an unavailable owned service")
			}
			if err := a.CloseConnection(t.Context(), monitorConnectionID); err == nil {
				t.Fatal("close accepted an unavailable owned service")
			}
			if calls.Load() != 0 {
				t.Fatal("unavailable service caused controller requests")
			}
		})
	}
}

func TestRuntimeUsesCoreTotalsIncludingClosedConnectionsAndCounterReset(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	var phase atomic.Int32
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		snapshot := mihomo.ConnectionSnapshot{UploadTotal: 1000, DownloadTotal: 2000, Connections: []mihomo.Connection{monitorConnection(monitorConnectionID)}}
		if phase.Load() == 1 {
			snapshot.Connections = []mihomo.Connection{}
		}
		if phase.Load() == 2 {
			snapshot.UploadTotal, snapshot.DownloadTotal = 3, 4
			snapshot.Connections = []mihomo.Connection{}
		}
		monitorResponse(w, r, snapshot)
	})
	var preferences []byte
	for p := int32(0); p < 3; p++ {
		phase.Store(p)
		got, err := a.Runtime(t.Context())
		if err != nil || got.State != "ready" || got.RunningVersion != "v1.19.33" || got.ObservedAt.IsZero() {
			t.Fatalf("phase=%d state=%+v error=%v", p, got, err)
		}
		if got.Traffic == nil || got.Traffic.Up != 123 || got.Traffic.Down != 456 || got.Memory == nil || *got.Memory != 65536 || got.Connections == nil {
			t.Fatalf("missing actual fixture metrics: %+v", got)
		}
		wantUpload, wantDownload := int64(1000), int64(2000)
		if p == 2 {
			wantUpload, wantDownload = 3, 4
		}
		if got.Connections.UploadTotal != wantUpload || got.Connections.DownloadTotal != wantDownload || (p > 0 && len(got.Connections.Connections) != 0) {
			t.Fatalf("totals were integrated or derived from active rows: %+v", got.Connections)
		}
		current := read(t, filepath.Join(a.Home, "preferences.json"))
		if p == 0 {
			preferences = current
		} else if !bytes.Equal(current, preferences) {
			t.Fatal("runtime counters were persisted between observations")
		}
	}
	prefs, err := a.LoadPreferences(t.Context())
	if err != nil || !prefs.HasRun {
		t.Fatalf("observed successful external start was not remembered: %+v %v", prefs, err)
	}
}

func TestRuntimeMetricFailuresRemainIndependentAndRedacted(t *testing.T) {
	for _, failed := range []string{"traffic", "memory", "connections", "version"} {
		t.Run(failed, func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = true
			monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/"+failed {
					w.WriteHeader(http.StatusBadGateway)
					_, _ = fmt.Fprint(w, "private-controller-token")
					return
				}
				monitorResponse(w, r, mihomo.ConnectionSnapshot{UploadTotal: 100, DownloadTotal: 200, Connections: []mihomo.Connection{}})
			})
			got, err := a.Runtime(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			want := "ready"
			if failed == "version" {
				want = "unavailable"
			}
			if got.State != want || (got.Traffic == nil) != (failed == "traffic" || failed == "version") || (got.Memory == nil) != (failed == "memory" || failed == "version") || (got.Connections == nil) != (failed == "connections" || failed == "version") {
				t.Fatalf("independent source states lost: %+v", got)
			}
			raw, _ := json.Marshal(got)
			if !strings.Contains(string(raw), "502") || strings.Contains(string(raw), "private-controller-token") {
				t.Fatalf("unsafe or unhelpful runtime error: %s", raw)
			}
		})
	}
}

func TestRuntimeSamplingReleasesLockAndRejectsLateStoppedSample(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/memory" {
			_, _ = fmt.Fprintln(w, `{"inuse":0}`)
			w.(http.Flusher).Flush()
			close(started)
			select {
			case <-release:
				_, _ = fmt.Fprintln(w, `{"inuse":65536}`)
			case <-r.Context().Done():
			}
			return
		}
		monitorResponse(w, r, mihomo.ConnectionSnapshot{UploadTotal: 10, DownloadTotal: 20, Connections: []mihomo.Connection{}})
	})
	type observation struct {
		value RuntimeSnapshot
		err   error
	}
	done := make(chan observation, 1)
	go func() { value, err := a.Runtime(t.Context()); done <- observation{value, err} }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("metric sources were not requested concurrently")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	if err := a.Control(ctx, "stop"); err != nil {
		t.Fatalf("metric stream blocked service stop: %v", err)
	}
	once.Do(func() { close(release) })
	select {
	case got := <-done:
		if got.err == nil && (got.value.State == "ready" || got.value.Service.Active || got.value.Traffic != nil || got.value.Connections != nil || got.value.Memory != nil) {
			t.Fatalf("late sample advertised a stopped service as live: %+v", got.value)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not finish after releasing its fixture stream")
	}
}

func TestRuntimeCancellationClosesAllMetricStreams(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	started, finished := make(chan string, 2), make(chan string, 2)
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/memory" || r.URL.Path == "/traffic" {
			started <- r.URL.Path
			<-r.Context().Done()
			finished <- r.URL.Path
			return
		}
		monitorResponse(w, r, mihomo.ConnectionSnapshot{UploadTotal: 0, DownloadTotal: 0, Connections: []mihomo.Connection{}})
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := a.Runtime(ctx); done <- err }()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("metric stream did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled runtime did not finish")
	}
	for range 2 {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("cancelled request remained open")
		}
	}
}

type monitorService struct {
	Services
	inspect func(context.Context) (service.Status, error)
}

func (s monitorService) Inspect(ctx context.Context) (service.Status, error) {
	return s.inspect(ctx)
}

func TestRuntimeRejectsSamplesSpanningDifferentCoreProcesses(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	var inspections atomic.Int32
	a.Service = monitorService{Services: svc, inspect: func(ctx context.Context) (service.Status, error) {
		pid := 101
		if inspections.Add(1) > 1 {
			pid = 202
		}
		return service.Status{Active: true, State: "active", PID: pid}, ctx.Err()
	}}
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		monitorResponse(w, r, mihomo.ConnectionSnapshot{UploadTotal: 1000, DownloadTotal: 2000, Connections: []mihomo.Connection{monitorConnection(monitorConnectionID)}})
	})
	got, err := a.Runtime(t.Context())
	if err == nil && (got.State == "ready" || got.Traffic != nil || got.Connections != nil || got.Memory != nil) {
		t.Fatalf("samples from different core processes were published as live: %+v", got)
	}
}

func TestRuntimeRechecksServiceOwnershipAfterSampling(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/memory" {
			_, _ = fmt.Fprintln(w, `{"inuse":0}`)
			w.(http.Flusher).Flush()
			close(started)
			select {
			case <-release:
				_, _ = fmt.Fprintln(w, `{"inuse":65536}`)
			case <-r.Context().Done():
			}
			return
		}
		monitorResponse(w, r, mihomo.ConnectionSnapshot{UploadTotal: 100, DownloadTotal: 200, Connections: []mihomo.Connection{}})
	})
	type observation struct {
		value RuntimeSnapshot
		err   error
	}
	done := make(chan observation, 1)
	go func() { value, err := a.Runtime(t.Context()); done <- observation{value, err} }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("memory stream did not start")
	}
	svc.verifyErr = errors.New("loaded service was replaced during the observation")
	once.Do(func() { close(release) })
	select {
	case got := <-done:
		if got.err == nil && (got.value.State == "ready" || got.value.Traffic != nil || got.value.Connections != nil || got.value.Memory != nil) {
			t.Fatalf("metrics survived failed ownership revalidation: %+v", got.value)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not finish after releasing its stream")
	}
}

func TestCloseConnectionRechecksExactIDWithoutCloseAll(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	var mu sync.Mutex
	closed := false
	var requests, deletes atomic.Int32
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodDelete {
			if r.URL.Path != "/connections/"+monitorConnectionID || r.URL.RawQuery != "" {
				t.Errorf("wrong connection or close-all route used: %s", r.URL.Path)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			deletes.Add(1)
			closed = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		rows := []mihomo.Connection{monitorConnection(monitorOtherID)}
		if !closed {
			rows = append(rows, monitorConnection(monitorConnectionID))
		}
		monitorResponse(w, r, mihomo.ConnectionSnapshot{UploadTotal: 100, DownloadTotal: 200, Connections: rows})
	})
	for _, id := range []string{"", "../connections", "id/other", "id?query=token"} {
		if err := a.CloseConnection(t.Context(), id); err == nil {
			t.Fatalf("unsafe ID accepted: %q", id)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("unsafe ID reached the controller")
	}
	if err := a.CloseConnection(t.Context(), monitorConnectionID); err != nil {
		t.Fatal(err)
	}
	if err := a.CloseConnection(t.Context(), monitorConnectionID); err == nil || !strings.Contains(err.Error(), "no longer active") {
		t.Fatalf("stale ID was not explicitly rejected: %v", err)
	}
	snapshot, err := a.Connections(t.Context())
	if err != nil || deletes.Load() != 1 || len(snapshot.Connections) != 1 || snapshot.Connections[0].ID != monitorOtherID || snapshot.UploadTotal != 100 {
		t.Fatalf("unrelated connection or counters changed: %+v, deletes=%d, err=%v", snapshot, deletes.Load(), err)
	}
}
