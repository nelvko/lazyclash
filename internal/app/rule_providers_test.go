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
	"github.com/nelvko/lazyclash/internal/store"
)

func fixtureRuleProvider() mihomo.RuleProvider {
	return mihomo.RuleProvider{Name: "fixture", Type: "Rule", VehicleType: "HTTP", Behavior: "Domain", Format: "YamlRule", RuleCount: 42, UpdatedAt: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)}
}

func writeRuleProviders(w http.ResponseWriter, provider mihomo.RuleProvider) {
	_ = json.NewEncoder(w).Encode(map[string]any{"providers": map[string]mihomo.RuleProvider{provider.Name: provider}})
}

func TestRuleProvidersUnavailableStatesNeverQueryOrStartService(t *testing.T) {
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
			var calls atomic.Int32
			monitorFixture(t, a, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) })
			got, err := a.RuleProviders(t.Context())
			if err != nil || got.State != state || got.Providers != nil || got.ObservedAt.IsZero() {
				t.Fatalf("invalid unavailable provider snapshot: %+v %v", got, err)
			}
			if err := a.UpdateRuleProvider(t.Context(), "fixture"); err == nil {
				t.Fatal("unavailable service accepted provider update")
			}
			if calls.Load() != 0 || len(svc.actions) != 0 {
				t.Fatal("unavailable provider operation contacted controller or changed service state")
			}
		})
	}
}

func TestRuleProvidersReadAndUpdateUseLiveMetadataWithoutChangingManagedFiles(t *testing.T) {
	for _, vehicle := range []string{"HTTP", "File"} {
		t.Run(vehicle, func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = true
			before, err := installationTree(filepath.Join(a.Home, "installation"))
			if err != nil {
				t.Fatal(err)
			}
			var updates atomic.Int32
			monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "" {
					t.Error("provider request lacked authentication")
				}
				if r.Method == http.MethodPut {
					if r.URL.Path != "/providers/rules/fixture" || r.URL.RawQuery != "" {
						t.Errorf("unexpected update endpoint: %s", r.URL.Path)
					}
					updates.Add(1)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != "/providers/rules" {
					t.Errorf("unexpected provider observation: %s %s", r.Method, r.URL.Path)
				}
				provider := fixtureRuleProvider()
				provider.VehicleType = vehicle
				provider.RuleCount += int(updates.Load())
				provider.UpdatedAt = provider.UpdatedAt.Add(time.Duration(updates.Load()) * time.Second)
				writeRuleProviders(w, provider)
			})
			initial, err := a.RuleProviders(t.Context())
			if err != nil || initial.State != "ready" || len(initial.Providers) != 1 || initial.Providers[0].RuleCount != 42 {
				t.Fatalf("missing live provider metadata: %+v %v", initial, err)
			}
			if err := a.UpdateRuleProvider(t.Context(), "fixture"); err != nil || updates.Load() != 1 {
				t.Fatalf("single provider update failed: %v updates=%d", err, updates.Load())
			}
			fresh, err := a.RuleProviders(t.Context())
			if err != nil || fresh.State != "ready" || len(fresh.Providers) != 1 || fresh.Providers[0].RuleCount != 43 || !fresh.Providers[0].UpdatedAt.After(initial.Providers[0].UpdatedAt) {
				t.Fatalf("old provider metadata substituted for current response: %+v %v", fresh, err)
			}
			after, err := installationTree(filepath.Join(a.Home, "installation"))
			if err != nil || !maps.Equal(before, after) || len(svc.actions) != 0 || svc.installs != 1 || svc.removes != 0 {
				t.Fatalf("provider operation modified managed configuration or service: %v", err)
			}
			if _, err := os.Stat(filepath.Join(a.Home, "preferences.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("provider operation persisted observations: %v", err)
			}
		})
	}
}

func TestRuleProvidersEmptyAndAPIErrorDoNotRetainOldMetadata(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	var phase atomic.Int32
	monitorFixture(t, a, func(w http.ResponseWriter, _ *http.Request) {
		switch phase.Load() {
		case 0:
			writeRuleProviders(w, fixtureRuleProvider())
		case 1:
			_, _ = fmt.Fprint(w, `{"providers":{}}`)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprint(w, "private-controller-response")
		}
	})
	for value := int32(0); value < 3; value++ {
		phase.Store(value)
		got, err := a.RuleProviders(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		switch value {
		case 0:
			if got.State != "ready" || len(got.Providers) != 1 {
				t.Fatalf("provider observation missing: %+v", got)
			}
		case 1:
			if got.State != "ready" || got.Providers == nil || len(got.Providers) != 0 {
				t.Fatalf("empty provider map confused with stale or unavailable result: %+v", got)
			}
		case 2:
			if got.State != "unavailable" || got.Providers != nil || got.Diagnostic == nil || !strings.Contains(got.Error, "503") || strings.Contains(got.Error, "private-") {
				t.Fatalf("failed provider observation exposed old data or private response: %+v", got)
			}
		}
	}
}

func TestUpdateRuleProviderRejectsMissingAndInlineSources(t *testing.T) {
	for _, name := range []string{"", "private-missing-provider", "fixture"} {
		t.Run(name, func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = true
			var reads, updates atomic.Int32
			monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					updates.Add(1)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				reads.Add(1)
				provider := fixtureRuleProvider()
				provider.VehicleType, provider.Format = "Inline", ""
				writeRuleProviders(w, provider)
			})
			err := a.UpdateRuleProvider(t.Context(), name)
			if err == nil || strings.Contains(err.Error(), "private-") || updates.Load() != 0 || len(svc.actions) != 0 || (name == "" && reads.Load() != 0) {
				t.Fatalf("missing/inline provider update was accepted or leaked identity: %v reads=%d updates=%d", err, reads.Load(), updates.Load())
			}
		})
	}
}

func TestRuleProviderReadAndUpdateCancellationReleaseRequests(t *testing.T) {
	for _, phase := range []string{"read", "discovery", "update"} {
		t.Run(phase, func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = true
			started, finished := make(chan struct{}), make(chan struct{})
			monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
				if phase == "update" && r.Method == http.MethodGet {
					writeRuleProviders(w, fixtureRuleProvider())
					return
				}
				close(started)
				<-r.Context().Done()
				close(finished)
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if phase == "read" {
					_, err := a.RuleProviders(ctx)
					done <- err
				} else {
					done <- a.UpdateRuleProvider(ctx, "fixture")
				}
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("provider request did not start")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("provider cancellation lost: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("provider operation did not cancel promptly")
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("cancelled provider request remained open")
			}
			if len(svc.actions) != 0 {
				t.Fatal("cancelling provider operation controlled the service")
			}
		})
	}
}

func TestRuleProviderOperationsRejectChangedCoreWithoutHoldingServiceLock(t *testing.T) {
	for _, phase := range []string{"read", "discovery", "update"} {
		for _, change := range []string{"process", "installation", "generation", "ownership", "stop"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
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
				var updates atomic.Int32
				monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPut {
						updates.Add(1)
					}
					if phase != "update" || r.Method == http.MethodPut {
						close(started)
						select {
						case <-release:
						case <-r.Context().Done():
							return
						}
					}
					if r.Method == http.MethodPut {
						w.WriteHeader(http.StatusNoContent)
					} else {
						writeRuleProviders(w, fixtureRuleProvider())
					}
				})
				type observation struct {
					snapshot RuleProviderSnapshot
					err      error
				}
				done := make(chan observation, 1)
				go func() {
					if phase == "read" {
						snapshot, err := a.RuleProviders(t.Context())
						done <- observation{snapshot, err}
					} else {
						done <- observation{err: a.UpdateRuleProvider(t.Context(), "fixture")}
					}
				}()
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("provider request did not start")
				}
				ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
				defer cancel()
				var changeErr error
				if change == "stop" {
					changeErr = a.Control(ctx, "stop")
				} else {
					changeErr = a.locked(ctx, func() error {
						if change == "process" || change == "ownership" {
							changed.Store(true)
							return nil
						}
						manifest, old, err := a.load()
						if err != nil {
							return err
						}
						if change == "installation" {
							manifest.ID = "replacement-installation"
							raw, err := json.Marshal(manifest)
							if err != nil {
								return err
							}
							return os.WriteFile(filepath.Join(a.Home, "installation", old, "manifest.json"), raw, 0600)
						}
						_, config, _ := a.paths()
						next, err := a.writeGeneration(filepath.Join(a.Home, "installation"), manifest, read(t, config))
						if err != nil {
							return err
						}
						return store.Switch(filepath.Join(a.Home, "installation", "current"), next, old)
					})
				}
				once.Do(func() { close(release) })
				if changeErr != nil {
					t.Fatalf("slow provider request held application/service lock: %v", changeErr)
				}
				select {
				case got := <-done:
					if phase == "read" {
						if got.err == nil && (got.snapshot.State == "ready" || got.snapshot.Providers != nil) {
							t.Fatalf("stale provider metadata survived %s change: %+v", change, got.snapshot)
						}
					} else if got.err == nil {
						t.Fatalf("stale provider update reported success after %s change", change)
					}
				case <-time.After(time.Second):
					t.Fatal("provider operation did not finish after response release")
				}
				wantUpdates := int32(0)
				if phase == "update" {
					wantUpdates = 1
				}
				if updates.Load() != wantUpdates {
					t.Fatalf("provider PUT reached changed instance after discovery: updates=%d want=%d", updates.Load(), wantUpdates)
				}
				wantActions := []string(nil)
				if change == "stop" {
					wantActions = []string{"stop"}
				}
				if !reflect.DeepEqual(svc.actions, wantActions) {
					t.Fatalf("provider operation unexpectedly controlled the service: %v", svc.actions)
				}
			})
		}
	}
}

func TestUpdateRuleProviderFailureKeepsControllerBodyPrivate(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeRuleProviders(w, fixtureRuleProvider())
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, "private-subscription-url-secret")
	})
	err := a.UpdateRuleProvider(t.Context(), "fixture")
	if err == nil || !strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "private-") || len(svc.actions) != 0 {
		t.Fatalf("update failure accepted or exposed controller error body: %v", err)
	}
}
