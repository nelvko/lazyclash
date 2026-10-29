package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

func TestStoppedModePersistsAcrossAppAndProfileChanges(t *testing.T) {
	a, svc := installedApp(t)
	if err := a.SetMode(t.Context(), "global"); err != nil {
		t.Fatal(err)
	}
	if svc.active || len(svc.actions) != 0 {
		t.Fatal("mode change started the stopped service")
	}
	cache := filepath.Join(a.Home, "installation", "runtime", "cache.db")
	write(t, cache, []byte("engine-owned-persistent-cache"))
	profile := filepath.Join(t.TempDir(), "subscription.yaml")
	write(t, profile, []byte("mode: direct\nproxies: []\nrules: [MATCH,DIRECT]\n"))
	if err := a.Add(t.Context(), "saved", profile); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "saved"); err != nil {
		t.Fatal(err)
	}
	again, err := New(a.Home, a.Name)
	if err != nil {
		t.Fatal(err)
	}
	again.Service = svc
	actual, err := again.GetMode(t.Context())
	if err != nil || actual.Desired != "global" || actual.Running || actual.Actual != "" {
		t.Fatalf("mode was not preserved across entry points: %+v %v", actual, err)
	}
	_, config, _ := a.paths()
	if !strings.Contains(string(read(t, config)), "store-selected: true") || string(read(t, cache)) != "engine-owned-persistent-cache" {
		t.Fatal("profile switch disabled or removed the selection cache")
	}
}

func TestModeLiveApplyAndRollback(t *testing.T) {
	for _, scenario := range []string{"success", "patch-failure", "ignored-patch", "cancel-patch", "readback-failure"} {
		t.Run(scenario, func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = true
			_, old, _ := a.load()
			observedMode := "rule"
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			patched := false
			a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
				code := 200
				body := `{"version":"v1.19.33"}`
				if r.URL.Path == "/configs" && r.Method == http.MethodPatch {
					patched = true
					if scenario == "patch-failure" {
						code = 500
					} else if scenario != "ignored-patch" {
						var payload map[string]string
						if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
							t.Fatal(err)
						}
						observedMode = payload["mode"]
					}
					if scenario == "cancel-patch" {
						cancel()
						return nil, context.Canceled
					}
				}
				if r.URL.Path == "/configs" && r.Method == http.MethodGet {
					body = `{"mode":"` + observedMode + `","tun":{"enable":false}}`
					if patched && scenario == "readback-failure" {
						code = 503
					}
				}
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})}
			svc.actionHook = func(_ context.Context, action string) error {
				if action == "restart" {
					_, config, _ := a.paths()
					var err error
					observedMode, err = mihomo.ConfigMode(read(t, config))
					return err
				}
				return nil
			}
			err := a.SetMode(ctx, "direct")
			m, current, loadErr := a.load()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if scenario == "success" {
				if err != nil || observedMode != "direct" || current == old || m.Mode != "direct" || len(svc.actions) != 0 {
					t.Fatalf("live mode did not commit without restart: %v %+v %v", err, m, svc.actions)
				}
				mode, err := a.GetMode(t.Context())
				if err != nil || mode.Desired != "direct" || mode.Actual != "direct" || !mode.Running {
					t.Fatalf("readback did not expose desired and actual state: %+v %v", mode, err)
				}
			} else {
				if err == nil || current != old || m.Mode != "" || observedMode != "rule" || !reflect.DeepEqual(svc.actions, []string{"restart"}) {
					t.Fatalf("failed mode was not rolled back: %v %+v %v", err, m, svc.actions)
				}
				if scenario == "cancel-patch" && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
			}
			assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
		})
	}
}

func TestUserServiceTUNBoundary(t *testing.T) {
	a, svc := installedApp(t)
	before, _ := installationTree(filepath.Join(a.Home, "installation"))
	if err := a.SetTUN(t.Context(), true); err == nil || !strings.Contains(err.Error(), "user service") {
		t.Fatal("TUN enable did not expose its unsupported privilege boundary", err)
	}
	if err := a.SetTUN(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	c := a.Capabilities()
	if c.Scope != "user" || c.TUN.Supported || c.SystemProxy.Supported || c.TUN.Reason == "" {
		t.Fatalf("incorrect capability report: %+v", c)
	}
	after, _ := installationTree(filepath.Join(a.Home, "installation"))
	if !maps.Equal(before, after) || len(svc.actions) != 0 {
		t.Fatal("unsupported TUN operation modified the installation or service")
	}
	profile := filepath.Join(t.TempDir(), "tun.yaml")
	write(t, profile, []byte("tun: {enable: true}\nrules: [MATCH,DIRECT]\n"))
	if err := a.Add(t.Context(), "tun", profile); err == nil || !strings.Contains(err.Error(), "TUN") {
		t.Fatal("privileged profile was silently imported", err)
	}
	if _, err := os.Stat(filepath.Join(a.Home, "profiles", "tun.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected TUN profile was saved")
	}
}

func TestTUNOffConfirmsActualState(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	for _, body := range []string{`{"mode":"rule","tun":{"enable":false}}`, `{"mode":"rule","tun":{"enable":true}}`, `{}`} {
		a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
		})}
		err := a.SetTUN(t.Context(), false)
		if (err == nil) != strings.Contains(body, "false") {
			t.Fatalf("incorrect TUN confirmation: %s %v", body, err)
		}
	}
}

func TestLegacySelectionPersistenceMustBeEnabledBeforeSelection(t *testing.T) {
	a, svc := installedApp(t)
	m, old, _ := a.load()
	_, config, _ := a.paths()
	raw := strings.Replace(string(read(t, config)), "store-selected: true", "store-selected: false", 1)
	next, err := a.writeGeneration(filepath.Join(a.Home, "installation"), m, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Switch(filepath.Join(a.Home, "installation", "current"), next, old); err != nil {
		t.Fatal(err)
	}
	svc.active = true
	requests := 0
	a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected API call")
	})}
	if err := a.Select(t.Context(), "PROXY", "REJECT"); err == nil || !strings.Contains(err.Error(), "reapply") || requests != 0 {
		t.Fatal("nonpersistent legacy node selection was reported as successful", err)
	}
	svc.active = false
	if err := a.Apply(t.Context(), "default"); err != nil {
		t.Fatal(err)
	}
	persistent, err := mihomo.ConfigSelectionPersistence(read(t, config))
	if err != nil || !persistent {
		t.Fatal("explicit profile application did not upgrade legacy cache setting", err)
	}
}
