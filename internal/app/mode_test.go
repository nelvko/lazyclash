package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/mihomo"
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
	if err := a.AddSubscription(t.Context(), "saved", profile); err != nil {
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
	if strings.Contains(string(read(t, config)), "store-selected:") {
		t.Fatal("profile switch injected an unspecified selection cache policy")
	}
	if string(read(t, cache)) != "engine-owned-persistent-cache" {
		t.Fatal("profile switch modified the engine-owned cache")
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
	write(t, profile, []byte("mixed-port: 7899\ntun: {enable: true, device: foreign}\nrules: ['MATCH,DIRECT']\n"))
	if err := a.AddSubscription(t.Context(), "tun", profile); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "tun"); err != nil {
		t.Fatal(err)
	}
	_, config, _ := a.paths()
	raw := read(t, config)
	if on, err := mihomo.ConfigTUN(raw); err != nil || on || strings.Contains(string(raw), "foreign") {
		t.Fatal("applying the profile imported its TUN policy", err)
	}
	if strings.Contains(string(raw), "mixed-port: 7899") || len(svc.actions) != 0 {
		t.Fatal("applying a profile changed the instance port or started the service")
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

func TestSelectWithSubscriptionSelectionCacheDisabled(t *testing.T) {
	a, svc := installedApp(t)
	path := filepath.Join(t.TempDir(), "subscription.yaml")
	write(t, path, []byte("profile: {store-selected: false}\nproxy-groups: [{name: Choice, type: select, proxies: [DIRECT, REJECT]}]\nrules: ['MATCH,Choice']\n"))
	if err := a.AddSubscription(t.Context(), "saved", path); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "saved"); err != nil {
		t.Fatal(err)
	}
	_, config, _ := a.paths()
	before := read(t, config)
	if !bytes.Contains(before, []byte("store-selected: false")) {
		t.Fatal("applied configuration lost the subscription's explicit cache policy")
	}

	svc.active = true
	selected := "DIRECT"
	var requests []string
	a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		operation := r.Method + " " + r.URL.Path
		requests = append(requests, operation)
		switch operation {
		case "GET /proxies":
			return proxyTestResponse(r, `{"proxies":{"Choice":{"type":"Selector","now":"`+selected+`","all":["DIRECT","REJECT"]}}}`), nil
		case "PUT /proxies/Choice":
			var payload struct{ Name string }
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			selected = payload.Name
			return proxyTestResponse(r, ""), nil
		default:
			t.Fatalf("unexpected controller request %s", operation)
			return nil, nil
		}
	})}
	if err := a.Select(t.Context(), "Choice", "REJECT"); err != nil {
		t.Fatal(err)
	}
	if selected != "REJECT" || !reflect.DeepEqual(requests, []string{"GET /proxies", "PUT /proxies/Choice", "GET /proxies"}) {
		t.Fatalf("selection = %q, controller requests = %v", selected, requests)
	}
	if !bytes.Equal(before, read(t, config)) {
		t.Fatal("selecting a node changed the applied subscription configuration")
	}
}
