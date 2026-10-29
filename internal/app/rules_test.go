package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestRulesUnavailableStatesDoNotQueryController(t *testing.T) {
	for _, state := range []string{"uninstalled", "stopped", "unsupported", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			a, svc := testApp(t)
			if state != "uninstalled" {
				if err := a.Install(t.Context(), "", nil); err != nil {
					t.Fatal(err)
				}
			}
			switch state {
			case "unsupported":
				a.Service, a.PlatformError = nil, errors.New("fixture service unsupported")
			case "unavailable":
				svc.active = true
				svc.verifyErr = errors.New("loaded service belongs to another installation")
			}
			var queries atomic.Int32
			monitorFixture(t, a, func(w http.ResponseWriter, _ *http.Request) { queries.Add(1); w.WriteHeader(500) })
			got, err := a.Rules(t.Context())
			if err != nil || got.State != state || got.Rules != nil || queries.Load() != 0 {
				t.Fatalf("unavailable core produced live rules: %+v error=%v queries=%d", got, err, queries.Load())
			}
			if len(svc.actions) != 0 {
				t.Fatal("reading rules changed service state")
			}
		})
	}
}

func TestRulesUseCurrentAPIOrderAndDoNotPersistOrModifyConfiguration(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	before, err := installationTree(filepath.Join(a.Home, "installation"))
	if err != nil {
		t.Fatal(err)
	}
	want := []mihomo.Rule{{Index: 3, Type: "DomainSuffix", Payload: "example.invalid", Proxy: "PROXY", Size: -1}, {Index: 8, Type: "Match", Payload: "", Proxy: "DIRECT", Size: -1}}
	var phase atomic.Int32
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rules" || r.Header.Get("Authorization") == "" {
			t.Error("rules did not use the authenticated read-only endpoint")
		}
		switch phase.Load() {
		case 0:
			_ = json.NewEncoder(w).Encode(map[string]any{"rules": want})
		case 1:
			_, _ = fmt.Fprint(w, `{"rules":[]}`)
		case 2:
			w.WriteHeader(http.StatusBadGateway)
			_, _ = fmt.Fprint(w, "private-controller-response")
		}
	})
	for phaseValue := int32(0); phaseValue < 3; phaseValue++ {
		phase.Store(phaseValue)
		got, err := a.Rules(t.Context())
		if err != nil || got.ObservedAt.IsZero() {
			t.Fatalf("phase=%d snapshot=%+v error=%v", phaseValue, got, err)
		}
		switch phaseValue {
		case 0:
			if got.State != "ready" || !reflect.DeepEqual(got.Rules, want) {
				t.Fatalf("live matching order was not preserved: %+v", got)
			}
		case 1:
			if got.State != "ready" || got.Rules == nil || len(got.Rules) != 0 {
				t.Fatalf("valid empty list was confused with old or unavailable rules: %+v", got)
			}
		case 2:
			if got.State != "unavailable" || got.Rules != nil || !strings.Contains(got.Error, "502") || strings.Contains(got.Error, "private-") {
				t.Fatalf("API failure retained old rules or leaked its body: %+v", got)
			}
		}
	}
	after, err := installationTree(filepath.Join(a.Home, "installation"))
	if err != nil || !maps.Equal(before, after) || len(svc.actions) != 0 || svc.installs != 1 || svc.removes != 0 {
		t.Fatalf("reading rules modified installation/configuration/service: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.Home, "preferences.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reading rules persisted preferences or fetched rules", err)
	}
}

func TestRulesCancellationClosesSlowHTTPResponse(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	started, finished := make(chan struct{}), make(chan struct{})
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"rules":[`)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(finished)
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := a.Rules(ctx); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("rules request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("rules cancellation lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("rules request did not cancel promptly")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("cancelled rules request was left open")
	}
}

type ruleObservation struct {
	value RuleSnapshot
	err   error
}

func TestRulesSlowRequestDoesNotHoldServiceLockOrPublishAfterStop(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
			_, _ = fmt.Fprint(w, `{"rules":[{"index":0,"type":"Match","payload":"","proxy":"DIRECT","size":-1}]}`)
		case <-r.Context().Done():
		}
	})
	done := make(chan ruleObservation, 1)
	go func() { value, err := a.Rules(t.Context()); done <- ruleObservation{value, err} }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("rules request did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	if err := a.Control(ctx, "stop"); err != nil {
		t.Fatalf("slow rules endpoint blocked service stop: %v", err)
	}
	once.Do(func() { close(release) })
	select {
	case got := <-done:
		if got.err != nil || got.value.State != "stopped" || got.value.Rules != nil {
			t.Fatalf("late rules survived a service stop: %+v %v", got.value, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("rules request did not finish after release")
	}
}

type rulesOwnerService struct {
	Services
	changed *atomic.Bool
}

func (s rulesOwnerService) Verify(ctx context.Context, bin, config, data string) error {
	if s.changed.Load() {
		return errors.New("loaded service ownership changed")
	}
	return s.Services.Verify(ctx, bin, config, data)
}

func TestRulesRejectResponseAfterProcessInstallationOrOwnershipChanges(t *testing.T) {
	for _, change := range []string{"process", "installation", "ownership"} {
		t.Run(change, func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = true
			var changed atomic.Bool
			if change == "ownership" {
				a.Service = rulesOwnerService{Services: svc, changed: &changed}
			} else if change == "process" {
				a.Service = monitorService{Services: svc, inspect: func(ctx context.Context) (service.Status, error) {
					pid := 101
					if changed.Load() {
						pid = 202
					}
					return service.Status{Active: true, State: "active", PID: pid}, ctx.Err()
				}}
			}
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-release:
					_, _ = fmt.Fprint(w, `{"rules":[{"index":0,"type":"Match","payload":"","proxy":"DIRECT","size":-1}]}`)
				case <-r.Context().Done():
				}
			})
			done := make(chan ruleObservation, 1)
			go func() { value, err := a.Rules(t.Context()); done <- ruleObservation{value, err} }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("rules request did not start")
			}
			if change == "installation" {
				if err := a.locked(t.Context(), func() error {
					m, target, err := a.load()
					if err != nil {
						return err
					}
					m.ID = "replacement-installation"
					raw, err := json.Marshal(m)
					if err != nil {
						return err
					}
					return os.WriteFile(filepath.Join(a.Home, "installation", target, "manifest.json"), raw, 0600)
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				changed.Store(true)
			}
			once.Do(func() { close(release) })
			select {
			case got := <-done:
				if got.err == nil && (got.value.State == "ready" || got.value.Rules != nil) {
					t.Fatalf("old rules survived %s change: %+v", change, got.value)
				}
			case <-time.After(time.Second):
				t.Fatal("rules request did not finish after release")
			}
		})
	}
}
