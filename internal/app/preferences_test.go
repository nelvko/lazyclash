package app

import (
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
	if err != nil || prefs.Network.Source != "official" || prefs.Network.ProxyMode != "none" || prefs.HasRun || prefs.GuideDismissed {
		t.Fatalf("defaults=%+v err=%v", prefs, err)
	}
	for _, cfg := range []downloadnet.Config{
		{Source: "mirror", Mirror: "https://mirror.example/{version}/{asset}?token=private-token"},
		{Source: "prefix", Prefix: "https://user:private-token@mirror.example/"},
		{Source: "prefix", Prefix: "https://mirror.example/private-token"},
		{ProxyMode: "custom", ProxyURL: "http://user:private-token@127.0.0.1:18080"},
	} {
		if err := a.SaveNetworkPreferences(t.Context(), cfg); err == nil || strings.Contains(err.Error(), "private-token") {
			t.Fatalf("credential preference accepted or echoed: %v", err)
		}
	}
	assertMissing(t, filepath.Join(a.Home, "preferences.json"))
	if err := a.SaveNetworkPreferences(t.Context(), downloadnet.Config{Source: "official", ProxyMode: "none", ProxyURL: "https:private-token", Prefix: "https://private-token.example/access"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(read(t, filepath.Join(a.Home, "preferences.json"))), "private-token") {
		t.Fatal("inactive connection setting leaked into preferences")
	}
	good := downloadnet.Config{Source: "mirror", Mirror: "https://mirror.example/releases/download/{version}/{asset}", ProxyMode: "custom", ProxyURL: "socks5h://127.0.0.1:18080"}
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
	info, err := os.Stat(filepath.Join(a.Home, "profiles", "imported.yaml"))
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
