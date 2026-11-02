package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/downloadnet"
)

func TestPreferencesPersistSafeSettingsAndKeepCredentialsSessionOnly(t *testing.T) {
	a, _ := testApp(t)
	prefs, err := a.LoadPreferences(t.Context())
	if err != nil || prefs.Network.Method != downloadnet.MethodDirect || prefs.HasRun || prefs.GuideDismissed {
		t.Fatalf("defaults=%+v err=%v", prefs, err)
	}
	for _, cfg := range []downloadnet.Config{
		{Method: downloadnet.MethodAccelerated, Prefix: "https://mirror.example/?token=private-token"},
		{Method: downloadnet.MethodAccelerated, Prefix: "https://user:private-token@mirror.example/"},
		{Method: downloadnet.MethodAccelerated, Prefix: "https://mirror.example/private-token"},
		{Method: downloadnet.MethodAccelerated, Prefix: "https://mirror.example/#private-token"},
	} {
		if err := a.SaveNetworkPreferences(t.Context(), cfg); err == nil || strings.Contains(err.Error(), "private-token") {
			t.Fatalf("credential preference accepted or echoed: %v", err)
		}
	}
	assertMissing(t, filepath.Join(a.Home, "preferences.json"))
	if err := a.SaveNetworkPreferences(t.Context(), downloadnet.Config{Method: downloadnet.MethodDirect, Prefix: "https://private-token.example/access"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(read(t, filepath.Join(a.Home, "preferences.json"))), "private-token") {
		t.Fatal("inactive connection setting leaked into preferences")
	}
	good := downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: "https://mirror.example/proxy/"}
	if err := a.SaveNetworkPreferences(t.Context(), good); err != nil {
		t.Fatal(err)
	}
	if err := a.SetGuideDismissed(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(a.Home, "preferences.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions=%v err=%v", info, err)
	}
	other, err := New(a.Home, a.Name)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := other.LoadPreferences(t.Context())
	if err != nil || loaded.Network != good || !loaded.GuideDismissed {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestSuccessfulFirstRunSurvivesStopAndLaterSessions(t *testing.T) {
	a, _ := installedApp(t)
	snapshot, err := a.Snapshot(t.Context())
	if err != nil || !snapshot.ConfigReady || snapshot.HasRun {
		t.Fatalf("before=%+v err=%v", snapshot, err)
	}
	if err := a.Control(t.Context(), "start"); err != nil {
		t.Fatal(err)
	}
	if err := a.Control(t.Context(), "stop"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = a.Snapshot(t.Context())
	if err != nil || !snapshot.HasRun || snapshot.Service.Active {
		t.Fatalf("after stop=%+v err=%v", snapshot, err)
	}
	loaded, err := a.LoadPreferences(t.Context())
	if err != nil || !loaded.HasRun {
		t.Fatalf("persisted=%+v err=%v", loaded, err)
	}
}

func TestImportBeforeInstallationIsPrivateAndNotAutomaticallyApplied(t *testing.T) {
	a, _ := testApp(t)
	source := filepath.Join(t.TempDir(), "import.yaml")
	write(t, source, []byte("proxies: []\nrules: ['MATCH,DIRECT']\n"))
	if err := a.Add(t.Context(), "imported", source); err != nil {
		t.Fatal(err)
	}
	write(t, source, []byte("proxies: private-token-scalar\n"))
	if err := a.Add(t.Context(), "bad-structure", source); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("invalid Mihomo structure accepted or echoed: %v", err)
	}
	write(t, source, []byte("proxies: []\nrules: ['MATCH,DIRECT']\n"))
	assertMissing(t, filepath.Join(a.Home, "installation"))
	snapshot, err := a.Snapshot(t.Context())
	if err != nil || snapshot.Installed || len(snapshot.UnvalidatedProfiles) != 1 || snapshot.UnvalidatedProfiles[0] != "imported" {
		t.Fatalf("pre-install=%+v err=%v", snapshot, err)
	}
	if err := a.Install(t.Context(), "", nil); err != nil {
		t.Fatal(err)
	}
	snapshot, err = a.Snapshot(t.Context())
	if err != nil || snapshot.Applied != "default" || !snapshot.ConfigReady || len(snapshot.UnvalidatedProfiles) != 1 {
		t.Fatalf("after install=%+v err=%v", snapshot, err)
	}
	if err := a.Apply(t.Context(), "imported"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = a.Snapshot(t.Context())
	if err != nil || snapshot.Applied != "imported" || len(snapshot.UnvalidatedProfiles) != 0 {
		t.Fatalf("after apply=%+v err=%v", snapshot, err)
	}
	info, err := os.Stat(savedSourcePath(t, a, "imported"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions=%v err=%v", info, err)
	}
	write(t, source, []byte("secret-token: [malformed"))
	if err := a.Add(t.Context(), "invalid", source); err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("invalid import: %v", err)
	}
}

func TestInvalidPreferencesArePreserved(t *testing.T) {
	a, _ := testApp(t)
	path := filepath.Join(a.Home, "preferences.json")
	if err := os.MkdirAll(a.Home, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, path, []byte("user edited not JSON"))
	if err := a.SetGuideDismissed(t.Context(), true); err == nil {
		t.Fatal("invalid existing preferences overwritten")
	}
	if string(read(t, path)) != "user edited not JSON" {
		t.Fatal("user preferences changed")
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		t.Fatal("user preferences removed")
	}
}

func TestObservedOwnedRunningEngineKeepsHistoryAfterAnIntentionalStop(t *testing.T) {
	a, svc := installedApp(t)
	// Model an existing installation started with systemctl rather than Control.
	svc.active = true
	snapshot, err := a.Snapshot(t.Context())
	if err != nil || !snapshot.Service.Active || !snapshot.HasRun {
		t.Fatalf("observed running engine=%+v err=%v", snapshot, err)
	}
	if err := a.Control(t.Context(), "stop"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = a.Snapshot(t.Context())
	if err != nil || snapshot.Service.Active || !snapshot.HasRun {
		t.Fatalf("history disappeared after stop: %+v err=%v", snapshot, err)
	}
	prefs, err := a.LoadPreferences(t.Context())
	if err != nil || !prefs.HasRun {
		t.Fatalf("history was not persisted: %+v err=%v", prefs, err)
	}
}

func TestUnsupportedNetworkPreferencesNeverSilentlyBecomeDirectOrGetOverwritten(t *testing.T) {
	for _, raw := range []string{
		`{"Network":{"source":"prefix","prefix":"https://accelerator.example/","proxy_mode":"environment"}}`,
		`{"Network":{"source":"mirror","mirror":"https://mirror.example/{version}/{asset}","proxy_mode":"none"}}`,
		`{"Network":{"source":"official","proxy_mode":"custom","proxy_url":"http://user:private-token@proxy.example"}}`,
		`{"Network":{"source":"official","proxy_mode":"none","future_setting":"private-token"}}`,
		`{"Network":{"method":"direct","source":"official","proxy_mode":"none"}}`,
		`{"Network":{"source":null,"proxy_mode":null}}`,
		`{"Network":{"method":"unknown"}}`,
		`{"Network":{"method":"accelerated","prefix":"https://user:private-token@accelerator.example/"}}`,
	} {
		a, _ := testApp(t)
		path := filepath.Join(a.Home, "preferences.json")
		write(t, path, []byte(raw))
		if _, err := a.LoadPreferences(t.Context()); err == nil || strings.Contains(err.Error(), "private-") {
			t.Fatalf("unsupported stored settings were accepted or exposed: %v", err)
		}
		if err := a.SaveNetworkPreferences(t.Context(), downloadnet.Config{Method: downloadnet.MethodDirect}); err == nil {
			t.Fatal("save overwrote unsupported stored settings")
		}
		if string(read(t, path)) != raw {
			t.Fatal("unsupported preference file was changed")
		}
	}
}

func TestLegacyNetworkPreferencesMigrateWithoutBlockingStartOrPreferenceUpdates(t *testing.T) {
	for _, tc := range []struct {
		name, network string
		want          downloadnet.Config
	}{
		{"default-direct", `{"source":"official","proxy_mode":"none"}`, downloadnet.Config{Method: downloadnet.MethodDirect}},
		{"environment", `{"source":"official","proxy_mode":"environment"}`, downloadnet.Config{Method: downloadnet.MethodEnvironment}},
		{"accelerated", `{"source":"prefix","prefix":"https://accelerator.example/proxy///","proxy_mode":"none"}`, downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: "https://accelerator.example/proxy/"}},
		{"empty-inactive-fields", `{"source":"official","proxy_mode":"none","mirror":"","proxy_url":""}`, downloadnet.Config{Method: downloadnet.MethodDirect}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := installedApp(t)
			path := filepath.Join(a.Home, "preferences.json")
			raw := `{"Network":` + tc.network + `,"Language":"en","GuideDismissed":true,"HideKeyHints":true}`
			write(t, path, []byte(raw))
			prefs, err := a.LoadPreferences(t.Context())
			if err != nil || prefs.Network != tc.want {
				t.Fatalf("legacy preference load=%+v err=%v", prefs, err)
			}
			if string(read(t, path)) != raw {
				t.Fatal("reading legacy preferences rewrote the file")
			}
			// Starting a previously installed core records HasRun through the
			// normal locked writer, including when the file still has old fields.
			if err := a.Control(t.Context(), "start"); err != nil {
				t.Fatal("legacy preferences prevented recording the first run", err)
			}
			if err := a.SaveLanguagePreference(t.Context(), "zh-Hans"); err != nil {
				t.Fatal("migrated preferences prevented a later update", err)
			}
			prefs, err = a.LoadPreferences(t.Context())
			if err != nil || prefs.Network != tc.want || !prefs.HasRun || !prefs.GuideDismissed || !prefs.HideKeyHints || prefs.Language != "zh-Hans" {
				t.Fatalf("updated preference=%+v err=%v", prefs, err)
			}
			var saved struct {
				Network map[string]json.RawMessage
			}
			if err := json.Unmarshal(read(t, path), &saved); err != nil {
				t.Fatal(err)
			}
			for field := range saved.Network {
				if field != "method" && field != "prefix" {
					t.Fatal("preference writer retained a legacy field", field)
				}
			}
			if string(saved.Network["method"]) != `"`+tc.want.Method+`"` {
				t.Fatalf("preference writer did not publish the new method: %s", saved.Network["method"])
			}
		})
	}
}
