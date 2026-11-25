package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"gopkg.in/yaml.v3"
)

func appliedConfigFields(t *testing.T, a *App) map[string]any {
	t.Helper()
	_, path, _ := a.paths()
	var cfg map[string]any
	if err := yaml.Unmarshal(read(t, path), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestCoreSettingsOverrideProfileSwitchRefreshAndReopen(t *testing.T) {
	a, service := installedApp(t)
	ctx := t.Context()
	first, second := filepath.Join(t.TempDir(), "one.yaml"), filepath.Join(t.TempDir(), "two.yaml")
	source := []byte("ipv6: true\nunified-delay: false\nlog-level: error\nrules: ['MATCH,DIRECT']\n")
	write(t, first, source)
	write(t, second, []byte("ipv6: false\nlog-level: info\nrules: ['MATCH,REJECT']\n"))
	if err := a.Add(ctx, "one", first); err != nil {
		t.Fatal(err)
	}
	if err := a.AddSubscription(ctx, "two", second); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	if cfg := appliedConfigFields(t, a); cfg["ipv6"] != true || cfg["unified-delay"] != false || cfg["log-level"] != "error" {
		t.Fatalf("lost inherited values before editing")
	}
	for key, value := range map[string]string{"ipv6": "off", "unified-delay": "on", "log-level": "debug"} {
		if err := a.SetCoreSetting(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	assertOverrides := func(a *App) {
		t.Helper()
		cfg := appliedConfigFields(t, a)
		if cfg["ipv6"] != false || cfg["unified-delay"] != true || cfg["log-level"] != "debug" {
			t.Fatalf("wrong core settings: ipv6=%v unified-delay=%v log-level=%v", cfg["ipv6"], cfg["unified-delay"], cfg["log-level"])
		}
	}
	assertOverrides(a)
	if !bytes.Equal(source, read(t, savedSourcePath(t, a, "one"))) {
		t.Fatal("editing settings changed the raw subscription")
	}
	if err := a.SetProfileOverrides(ctx, "two", []byte("set:\n  log-level: warning\n")); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	assertOverrides(a)
	revision := []byte("ipv6: true\nunified-delay: false\nlog-level: info\nrules: ['MATCH,DIRECT']\n")
	write(t, second, revision)
	if err := a.Refresh(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	assertOverrides(a)
	if !bytes.Equal(revision, read(t, savedSourcePath(t, a, "two"))) {
		t.Fatal("refresh overwrote source fields with instance overrides")
	}
	reopened, err := New(a.Home, a.Name)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Service, reopened.Validate = service, a.Validate
	snap, err := reopened.Snapshot(ctx)
	want := mihomo.GeneralSettings{IPv6: "off", UnifiedDelay: "on", LogLevel: "debug"}
	if err != nil || snap.CoreSettings != want {
		t.Fatalf("reopened settings = %+v, err = %v", snap.CoreSettings, err)
	}
	if err := reopened.SetCoreSetting(ctx, "log-level", ""); err != nil {
		t.Fatal(err)
	}
	if cfg := appliedConfigFields(t, reopened); cfg["log-level"] != "warning" || cfg["ipv6"] != false || cfg["unified-delay"] != true {
		t.Fatal("clearing a setting must reveal the per-profile override and preserve other choices")
	}
	if err := reopened.DeactivateProfile(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SetCoreSetting(ctx, "ipv6", ""); err != nil {
		t.Fatal(err)
	}
	if cfg := appliedConfigFields(t, reopened); cfg["ipv6"] != nil || cfg["unified-delay"] != true || cfg["log-level"] != "warning" {
		t.Fatal("clearing a setting must preserve the built-in profile's omission")
	}
	if len(service.actions) != 0 || service.active {
		t.Fatal("editing settings on a stopped service changed its state")
	}
}

func TestCoreSettingsChangeOnRunningServiceCommitsOnlyAfterReload(t *testing.T) {
	for _, reject := range []bool{false, true} {
		a, svc := installedApp(t)
		svc.active = true
		_, old, err := a.load()
		if err != nil {
			t.Fatal(err)
		}
		requests := 0
		a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
			code := http.StatusOK
			if r.Method == http.MethodPut && r.URL.Path == "/configs" {
				requests++
				if reject && requests == 1 {
					code = http.StatusBadRequest
				}
			}
			return &http.Response{StatusCode: code, Header: make(http.Header), Request: r, Body: io.NopCloser(strings.NewReader(`{"version":"v1.19.33"}`))}, nil
		})}
		err = a.SetCoreSetting(t.Context(), "log-level", "debug")
		m, current, loadErr := a.load()
		if loadErr != nil || !svc.active || len(svc.actions) != 0 {
			t.Fatal("reload should preserve the service process", loadErr)
		}
		if reject {
			if err == nil || current != old || m.CoreSettings.LogLevel != "" || requests != 2 {
				t.Fatal("rejected reload failed to restore the old generation", err, requests)
			}
		} else if err != nil || current == old || m.CoreSettings.LogLevel != "debug" || requests != 1 {
			t.Fatal("successful hot reload failed to commit the new generation", err, requests)
		}
	}
}

func TestPinMatchingCoreSettingWithoutReload(t *testing.T) {
	a, svc := installedApp(t)
	ctx := t.Context()
	svc.active = true
	svc.verifyErr = errors.New("unchanged YAML must not contact the service")
	if err := a.SetCoreSetting(ctx, "log-level", "warning"); err != nil {
		t.Fatal(err)
	}
	m, _, err := a.load()
	if err != nil || m.CoreSettings.LogLevel != "warning" {
		t.Fatal("pinning the built-in log level was not saved", err)
	}
	svc.active, svc.verifyErr = false, nil
	addFixtureProfile(t, a, "source")
	if err := a.Apply(ctx, "source"); err != nil {
		t.Fatal(err)
	}
	if cfg := appliedConfigFields(t, a); cfg["log-level"] != "warning" {
		t.Fatal("matching override was lost when selecting a profile without a log level")
	}
	// A failed background refresh can leave newer content in the library
	// than in the deployed generation. Inherit from the deployed revision.
	lib, old, err := a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := lib.Profiles["source"]
	entry.Spec.Revision, err = a.library().Put([]byte("log-level: error\nrules: ['MATCH,REJECT']\n"))
	if err != nil {
		t.Fatal(err)
	}
	lib.Profiles["source"] = entry
	if err := a.library().Save(lib, old); err != nil {
		t.Fatal(err)
	}
	if err := a.SetCoreSetting(ctx, "log-level", ""); err != nil {
		t.Fatal(err)
	}
	if cfg := appliedConfigFields(t, a); cfg["log-level"] != nil {
		t.Fatal("resetting the override activated a pending source revision")
	}
}

func TestCoreSettingsValidationAndNoopPreserveGeneration(t *testing.T) {
	a, _ := installedApp(t)
	_, old, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	a.Validate = func(context.Context, string, string, string) error { return errors.New("core rejected settings") }
	for key, value := range map[string]string{"log-level": "verbose", "ipv6": "auto", "allow-lan": "on", "unified-delay": "on"} {
		if err := a.SetCoreSetting(t.Context(), key, value); err == nil {
			t.Fatalf("accepted %s=%s", key, value)
		}
	}
	if err := a.SetCoreSetting(t.Context(), "log-level", ""); err != nil {
		t.Fatal("unchanged settings should succeed without validation", err)
	}
	if _, current, err := a.load(); current != old || err != nil {
		t.Fatal("invalid and unchanged edits should leave the original generation intact", err)
	}
}

func TestTUNTemporarilyOverridesSavedIPv6(t *testing.T) {
	a, svc := installedApp(t)
	a.Service = &tunService{fakeService: svc}
	if err := a.SetTUN(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if err := a.SetCoreSetting(t.Context(), "ipv6", "off"); err != nil {
		t.Fatal(err)
	}
	if cfg := appliedConfigFields(t, a); cfg["ipv6"] != true {
		t.Fatal("active TUN lost required IPv6")
	}
	if err := a.SetTUN(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	if cfg := appliedConfigFields(t, a); cfg["ipv6"] != false {
		t.Fatal("saved IPv6 preference lost when TUN turned off")
	}
}

func TestTUNCanBeDisabledWithoutPinnedInputs(t *testing.T) {
	for _, missing := range []string{"source", "recipe"} {
		t.Run(missing, func(t *testing.T) {
			a, svc := installedApp(t)
			a.Service = &tunService{fakeService: svc}
			addFixtureProfile(t, a, "profile")
			if err := a.Apply(t.Context(), "profile"); err != nil {
				t.Fatal(err)
			}
			if err := a.SetCoreSetting(t.Context(), "ipv6", "off"); err != nil {
				t.Fatal(err)
			}
			if missing == "source" {
				if err := os.Remove(savedSourcePath(t, a, "profile")); err != nil {
					t.Fatal(err)
				}
			} else {
				m, current, err := a.load()
				if err != nil {
					t.Fatal(err)
				}
				m.Build = nil
				raw, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(a.Home, "installation", current, "manifest.json"), raw)
			}
			if err := a.SetTUN(t.Context(), true); err != nil {
				t.Fatal("enable from deployed config", err)
			}
			if err := a.SetTUN(t.Context(), false); err != nil {
				t.Fatal("disable from deployed config", err)
			}
			m, _, err := a.load()
			if err != nil || m.TUN || m.CoreSettings.IPv6 != "off" || appliedConfigFields(t, a)["ipv6"] != false {
				t.Fatal("failed to restore saved IPv6 when disabling TUN without pinned inputs", err)
			}
		})
	}
}
