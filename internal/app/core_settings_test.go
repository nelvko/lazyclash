package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
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

func TestInstanceCoreSettingsSurviveProfileSwitchRefreshAndReopen(t *testing.T) {
	a, service := installedApp(t)
	ctx := t.Context()
	first, second := filepath.Join(t.TempDir(), "one.yaml"), filepath.Join(t.TempDir(), "two.yaml")
	source := []byte(`ipv6: false
unified-delay: true
log-level: error
tcp-concurrent: true
disable-keep-alive: true
find-process-mode: always
etag-support: false
keep-alive-idle: 34
keep-alive-interval: 9
geo-auto-update: true
geo-update-interval: 4
geodata-loader: standard
profile: {store-fake-ip: true}
rules: ['MATCH,DIRECT']
`)
	write(t, first, source)
	write(t, second, []byte("ipv6: false\nlog-level: info\nrules: ['MATCH,REJECT']\n"))
	if err := a.AddSubscription(ctx, "one", first); err != nil {
		t.Fatal(err)
	}
	if err := a.AddSubscription(ctx, "two", second); err != nil {
		t.Fatal(err)
	}
	wantDefaults := mihomo.DefaultGeneralSettings()
	assertInstanceSettings(t, a, wantDefaults)
	if err := a.Apply(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	assertInstanceSettings(t, a, wantDefaults)
	for key, value := range map[string]string{
		"ipv6": "off", "unified-delay": "on", "log-level": "debug",
		"tcp-concurrent": "on", "disable-keep-alive": "on", "find-process-mode": "off", "etag-support": "off",
		"keep-alive-idle": "45", "keep-alive-interval": "15", "geo-auto-update": "on", "geo-update-interval": "8", "geodata-loader": "standard", "profile.store-fake-ip": "on",
	} {
		if err := a.SetCoreSetting(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	want := mihomo.GeneralSettings{
		IPv6: "off", UnifiedDelay: "on", LogLevel: "debug",
		TCPConcurrent: "on", DisableKeepAlive: "on", FindProcessMode: "off", ETagSupport: "off",
		KeepAliveIdle: "45", KeepAliveInterval: "15", GeoAutoUpdate: "on", GeoUpdateInterval: "8", GeodataLoader: "standard", StoreFakeIP: "on",
	}
	assertInstanceSettings(t, a, want)
	otherInstance, _ := installedApp(t)
	assertInstanceSettings(t, otherInstance, wantDefaults)
	if !bytes.Equal(source, read(t, savedSourcePath(t, a, "one"))) {
		t.Fatal("editing settings changed the raw subscription")
	}
	overrides := []byte("set: {log-level: warning, tcp-concurrent: false, disable-keep-alive: false, find-process-mode: always, etag-support: true}\n")
	if err := a.SetProfileOverrides(ctx, "two", overrides); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	assertInstanceSettings(t, a, want)
	revision := []byte("ipv6: true\nunified-delay: false\nlog-level: info\nrules: ['MATCH,DIRECT']\n")
	write(t, second, revision)
	if err := a.Refresh(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	assertInstanceSettings(t, a, want)
	if !bytes.Equal(revision, read(t, savedSourcePath(t, a, "two"))) {
		t.Fatal("refresh overwrote source fields with instance overrides")
	}
	reopened, err := New(a.Home, a.Name)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Service, reopened.Validate = service, a.Validate
	snap, err := reopened.Snapshot(ctx)
	if err != nil || snap.CoreSettings != want {
		t.Fatalf("reopened settings = %+v, err = %v", snap.CoreSettings, err)
	}
	if err := reopened.DeactivateProfile(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	assertInstanceSettings(t, reopened, want)
	if err := reopened.SetMode(ctx, "global"); err != nil {
		t.Fatal(err)
	}
	plan, err := reopened.PrepareRuntimeSettings(ctx, availableSettings(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ApplyRuntimeSettings(ctx, plan); err != nil {
		t.Fatal(err)
	}
	assertInstanceSettings(t, reopened, want)
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
			if err == nil || current != old || m.CoreSettings != mihomo.DefaultGeneralSettings() || requests != 2 {
				t.Fatal("rejected reload failed to restore the old generation", err, requests)
			}
		} else if err != nil || current == old || m.CoreSettings.LogLevel != "debug" || requests != 1 {
			t.Fatal("successful hot reload failed to commit the new generation", err, requests)
		}
	}
}

func TestCoreSettingEditsDeployedConfigWithMissingProfileSource(t *testing.T) {
	a, _ := installedApp(t)
	source := filepath.Join(t.TempDir(), "source.yaml")
	write(t, source, []byte("keep-alive-idle: 30\nkeep-alive-interval: 10\nrules: ['MATCH,DIRECT']\n"))
	if err := a.AddSubscription(t.Context(), "source", source); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "source"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(savedSourcePath(t, a, "source")); err != nil {
		t.Fatal(err)
	}
	want := appliedConfigFields(t, a)
	want["disable-keep-alive"] = true
	if err := a.SetCoreSetting(t.Context(), "disable-keep-alive", "on"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(appliedConfigFields(t, a), want) {
		t.Fatal("setting edit changed unrelated deployed configuration")
	}
	settings := mihomo.DefaultGeneralSettings()
	settings.DisableKeepAlive = "on"
	assertInstanceSettings(t, a, settings)
}

// Read the raw manifest so load's defaults cannot mask a missing write.
func assertInstanceSettings(t *testing.T, a *App, want mihomo.GeneralSettings) {
	t.Helper()
	var m manifest
	path := filepath.Join(a.Home, "installation", "current", "manifest.json")
	if err := json.Unmarshal(read(t, path), &m); err != nil {
		t.Fatal(err)
	}
	if m.CoreSettings != want {
		t.Fatalf("stored settings = %+v, want %+v", m.CoreSettings, want)
	}
	cfg := appliedConfigFields(t, a)
	for key, value := range map[string]string{"keep-alive-idle": want.KeepAliveIdle, "keep-alive-interval": want.KeepAliveInterval} {
		if value == "default" && cfg[key] != nil || value != "default" && fmt.Sprint(cfg[key]) != value {
			t.Fatalf("applied %s = %v, want %v", key, cfg[key], value)
		}
	}
	profile, _ := cfg["profile"].(map[string]any)
	if profile["store-fake-ip"] != (want.StoreFakeIP == "on") || profile["store-selected"] != true {
		t.Fatal("unexpected profile cache settings", profile)
	}
	interval, _ := strconv.Atoi(want.GeoUpdateInterval)
	for key, value := range map[string]any{
		"geo-auto-update": want.GeoAutoUpdate == "on", "geo-update-interval": interval, "geodata-loader": want.GeodataLoader,
		"ipv6": m.TUN || want.IPv6 == "on", "unified-delay": want.UnifiedDelay == "on", "log-level": want.LogLevel,
		"tcp-concurrent": want.TCPConcurrent == "on", "disable-keep-alive": want.DisableKeepAlive == "on",
		"find-process-mode": want.FindProcessMode, "etag-support": want.ETagSupport == "on",
	} {
		if cfg[key] != value {
			t.Fatalf("applied %s = %v, want %v", key, cfg[key], value)
		}
	}
}

func TestUnsetCoreSettingsUseDeployedGenerationUntilNextWrite(t *testing.T) {
	omitted := mihomo.DefaultGeneralSettings()
	omitted.LogLevel = "warning"
	pinned := mihomo.GeneralSettings{
		IPv6: "off", UnifiedDelay: "on", LogLevel: "debug",
		TCPConcurrent: "on", DisableKeepAlive: "on", FindProcessMode: "always", ETagSupport: "off",
		KeepAliveIdle: "55", KeepAliveInterval: "18", GeoAutoUpdate: "on", GeoUpdateInterval: "48", GeodataLoader: "standard", StoreFakeIP: "on",
	}
	applied := map[string]any{
		"ipv6": false, "unified-delay": true, "log-level": "debug",
		"tcp-concurrent": true, "disable-keep-alive": true, "find-process-mode": "always", "etag-support": false,
		"keep-alive-idle": 55, "keep-alive-interval": 18, "geo-auto-update": true, "geo-update-interval": 48, "geodata-loader": "standard", "profile": map[string]any{"store-selected": true, "store-fake-ip": true},
	}
	for _, tc := range []struct {
		name    string
		saved   map[string]string
		applied map[string]any
		want    mihomo.GeneralSettings
	}{
		{"omitted fields", nil, map[string]any{"log-level": "warning"}, omitted},
		{"applied loader alias and inactive schedule", nil, map[string]any{"log-level": "warning", "geodata-loader": "memc", "geo-auto-update": true, "geo-update-interval": 0}, omitted},
		{"partly pinned", map[string]string{"log_level": "debug"}, applied, pinned},
		{"three saved fields", map[string]string{"ipv6": "off", "unified_delay": "on", "log_level": "debug"}, applied, pinned},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := installedApp(t)
			addFixtureProfile(t, a, "source")
			_, current, err := a.load()
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(a.Home, "installation", current)
			manifestPath := filepath.Join(dir, "manifest.json")
			cfg := appliedConfigFields(t, a)
			for _, key := range []string{"ipv6", "unified-delay", "log-level", "tcp-concurrent", "disable-keep-alive", "find-process-mode", "etag-support", "keep-alive-idle", "keep-alive-interval", "geo-auto-update", "geo-update-interval", "geodata-loader", "profile"} {
				delete(cfg, key)
				if value, ok := tc.applied[key]; ok {
					cfg[key] = value
				}
			}
			cfgBytes, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(dir, "config.yaml"), cfgBytes)
			var disk map[string]any
			if err := json.Unmarshal(read(t, manifestPath), &disk); err != nil {
				t.Fatal(err)
			}
			disk["config_sha256"] = digest(cfgBytes)
			if tc.saved == nil {
				delete(disk, "core_settings")
			} else {
				disk["core_settings"] = tc.saved
			}
			rawManifest, err := json.Marshal(disk)
			if err != nil {
				t.Fatal(err)
			}
			write(t, manifestPath, rawManifest)
			snap, err := a.Snapshot(t.Context())
			if err != nil || snap.CoreSettings != tc.want {
				t.Fatalf("resolved settings = %+v, want %+v, err = %v", snap.CoreSettings, tc.want, err)
			}
			if _, target, err := a.load(); err != nil || target != current || !bytes.Equal(rawManifest, read(t, manifestPath)) {
				t.Fatal("reading settings changed the generation", err)
			}
			if err := a.Apply(t.Context(), "source"); err != nil {
				t.Fatal(err)
			}
			assertInstanceSettings(t, a, tc.want)
		})
	}
}

func TestCoreSettingsValidationAndNoopPreserveGeneration(t *testing.T) {
	a, _ := installedApp(t)
	_, old, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"log-level": "verbose", "ipv6": "auto", "allow-lan": "on", "unified-delay": "inherit",
		"tcp-concurrent": "true", "disable-keep-alive": "auto", "find-process-mode": "on", "etag-support": "true",
		"keep-alive-idle": "0", "keep-alive-interval": "-2", "geo-update-interval": "999999999999999999", "geo-auto-update": "auto", "geodata-loader": "fast", "profile.store-fake-ip": "yes",
	} {
		if err := a.SetCoreSetting(t.Context(), key, value); err == nil {
			t.Fatalf("accepted %s=%s", key, value)
		}
	}
	for _, key := range []string{"ipv6", "unified-delay", "log-level", "tcp-concurrent", "disable-keep-alive", "find-process-mode", "etag-support"} {
		if err := a.SetCoreSetting(t.Context(), key, ""); err == nil {
			t.Fatalf("accepted empty %s", key)
		}
	}
	a.Validate = func(context.Context, string, string, string) error { return errors.New("core rejected settings") }
	if err := a.SetCoreSetting(t.Context(), "unified-delay", "on"); err == nil {
		t.Fatal("published a setting after the core rejected the configuration")
	}
	if err := a.SetCoreSetting(t.Context(), "log-level", "info"); err != nil {
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

func TestGeoScheduleTracksRunningProcessAndRevertedEdits(t *testing.T) {
	a, svc := installedApp(t)
	manager := &profileLifecycleService{Services: svc, status: service.Status{Active: true, State: "active", PID: 1711, StartTimestampMonotonic: 100}}
	a.Service = manager
	a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Request: r, Body: io.NopCloser(strings.NewReader(`{"version":"v1.19.33","mode":"rule"}`))}, nil
	})}
	check := func(key, value string, pending bool) {
		t.Helper()
		if key != "" {
			if err := a.SetCoreSetting(t.Context(), key, value); err != nil {
				t.Fatal(err)
			}
		}
		snap, err := a.Snapshot(t.Context())
		if err != nil || snap.GeoRestartPending != pending {
			t.Fatalf("%s=%s pending=%t, want %t: %v", key, value, snap.GeoRestartPending, pending, err)
		}
	}
	check("geo-update-interval", "3", false)
	check("geo-auto-update", "on", true)
	check("geo-update-interval", "4", true)
	check("geo-auto-update", "off", false)
	check("geo-auto-update", "on", true)
	manager.status.StartTimestampMonotonic++
	check("", "", false)
	check("geo-update-interval", "6", true)
	check("geo-update-interval", "4", false)
	check("geo-auto-update", "off", true)
	manager.status.Active, manager.status.State = false, "inactive"
	check("", "", false)
}
