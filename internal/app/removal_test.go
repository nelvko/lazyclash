package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/service"
)

type removalService struct {
	*fakeService
	status     service.Status
	removeHook func()
}

func (s removalService) Inspect(ctx context.Context) (service.Status, error) {
	return s.status, ctx.Err()
}
func (s removalService) RemoveOwned(ctx context.Context, bin, config, data string) error {
	if err := s.fakeService.RemoveOwned(ctx, bin, config, data); err != nil {
		return err
	}
	if s.removeHook != nil {
		s.removeHook()
	}
	return nil
}

func removalRecord(t *testing.T, a *App) {
	t.Helper()
	m, _, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.saveRuntimeRecord(runtimeRecord{Installation: m.ID, Service: a.Name, Desired: DesiredState{Known: true}, Operation: OperationState{Action: "stop", State: "complete"}}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveCorePreservesProfilesSourcesAndPreferences(t *testing.T) {
	a, s := installedApp(t)
	addFixtureProfile(t, a, "saved")
	source := filepath.Join(t.TempDir(), "subscription.yaml")
	write(t, source, []byte("mode: rule\nproxies: []\nproxy-groups: []\nrules: ['MATCH,DIRECT']\n"))
	if err := a.AddSubscription(t.Context(), "subscription", source); err != nil {
		t.Fatal(err)
	}
	removalRecord(t, a)
	write(t, filepath.Join(a.Home, "preferences.json"), []byte(`{"Network":{"Source":"official","ProxyMode":"none"}}`))
	profilePath := savedSourcePath(t, a, "saved")
	profile := read(t, profilePath)
	metadataPath := filepath.Join(a.Home, "configuration", "library.json")
	metadata := read(t, metadataPath)
	subscriptionPath := savedSourcePath(t, a, "subscription")
	subscription := read(t, subscriptionPath)
	if err := a.RemoveCore(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, filepath.Join(a.Home, "installation"))
	assertMissing(t, filepath.Join(a.Home, "runtime-state.json"))
	if s.removes != 1 || len(s.actions) != 0 {
		t.Fatal("uninstall changed lifecycle or failed to remove the owned unit")
	}
	if string(read(t, profilePath)) != string(profile) {
		t.Fatal("profile was changed")
	}
	if string(read(t, metadataPath)) != string(metadata) || string(read(t, subscriptionPath)) != string(subscription) {
		t.Fatal("subscription source was changed")
	}
	if _, err := os.Stat(filepath.Join(a.Home, "preferences.json")); err != nil {
		t.Fatal("user preferences were removed", err)
	}
}

func TestRemoveCoreRequiresStoppedAndDisabledOwnedService(t *testing.T) {
	for _, status := range []service.Status{{State: "active", Active: true}, {State: "activating"}, {State: "deactivating"}, {State: "inactive", PID: 42}, {State: "inactive", Enabled: true}, {State: "failed", EnableState: "enabled-runtime"}} {
		a, s := installedApp(t)
		a.Service = removalService{fakeService: s, status: status}
		if err := a.RemoveCore(t.Context()); err == nil {
			t.Fatalf("uninstall accepted unsafe state: %+v", status)
		}
		if s.removes != 0 {
			t.Fatal("unsafe uninstall removed its unit")
		}
		if _, _, err := a.load(); err != nil {
			t.Fatal("unsafe uninstall changed installation", err)
		}
	}
	a, s := installedApp(t)
	s.verifyErr = errors.New("foreign unit")
	if err := a.RemoveCore(t.Context()); err == nil || s.removes != 0 {
		t.Fatal("uninstall accepted a foreign unit")
	}
}

func TestRemoveCorePreservesForeignOrChangedData(t *testing.T) {
	for _, mutation := range []string{"runtime-state", "unexpected-file", "symlink", "foreign-generation"} {
		t.Run(mutation, func(t *testing.T) {
			a, s := installedApp(t)
			switch mutation {
			case "runtime-state":
				write(t, filepath.Join(a.Home, "runtime-state.json"), []byte(`{"installation":"someone-else"}`))
			case "unexpected-file":
				write(t, filepath.Join(a.Home, "installation", "my-data"), []byte("preserve"))
			case "symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(a.Home, "installation", "runtime", "foreign")); err != nil {
					t.Fatal(err)
				}
			case "foreign-generation":
				m, _, _ := a.load()
				m.ID = "other-installation"
				if _, err := a.writeGeneration(filepath.Join(a.Home, "installation"), m, read(t, filepath.Join(a.Home, "installation", "current", "config.yaml"))); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.RemoveCore(t.Context()); err == nil || s.removes != 0 {
				t.Fatal("uninstall removed unverified data")
			}
			if _, err := os.Stat(filepath.Join(a.Home, "installation")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRemoveCoreFailureAndConcurrentChangesAreExplicit(t *testing.T) {
	a, s := installedApp(t)
	removalRecord(t, a)
	s.removeErr = errors.New("unit removal failed")
	if err := a.RemoveCore(t.Context()); !errors.Is(err, s.removeErr) {
		t.Fatal("unit failure hidden", err)
	}
	if _, _, err := a.load(); err != nil {
		t.Fatal("unit failure deleted installation", err)
	}
	s.removeErr = nil
	a.Service = removalService{fakeService: s, status: service.Status{State: "inactive"}, removeHook: func() { write(t, filepath.Join(a.Home, "runtime-state.json"), []byte("changed after unit removal")) }}
	if err := a.RemoveCore(t.Context()); err == nil || !strings.Contains(err.Error(), "service unit removed") || !strings.Contains(err.Error(), "manual cleanup") {
		t.Fatal("partial cleanup was reported as success", err)
	}
	if _, _, err := a.load(); err != nil {
		t.Fatal("concurrent runtime mutation deleted installation", err)
	}
}
