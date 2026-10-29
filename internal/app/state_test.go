package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"
)

func TestRuntimeIntentSurvivesNewControllerAndShowsDrift(t *testing.T) {
	a, s := installedApp(t)
	if err := a.Control(t.Context(), "start"); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(a.Home, a.Name)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Service, reopened.HTTP = s, a.HTTP
	snap, err := reopened.Snapshot(t.Context())
	if err != nil || !snap.Desired.Known || !snap.Desired.Running || snap.Operation.State != "complete" || !snap.Service.Active {
		t.Fatalf("reopen: %+v %v", snap, err)
	}
	if len(s.actions) != 1 {
		t.Fatalf("observation changed lifecycle: %v", s.actions)
	}
	s.active = false // an actual runtime failure, not another controller's intent
	snap, err = reopened.Snapshot(t.Context())
	if err != nil || snap.Drift == "" || !snap.Desired.Running {
		t.Fatalf("drift hidden: %+v %v", snap, err)
	}
	if len(s.actions) != 1 {
		t.Fatal("observation implicitly restarted a service")
	}
	if err := reopened.Control(t.Context(), "stop"); err != nil {
		t.Fatal(err)
	}
	snap, err = reopened.Snapshot(t.Context())
	if err != nil || snap.Desired.Running || snap.Drift != "" || snap.Operation.State != "complete" {
		t.Fatalf("stop: %+v %v", snap, err)
	}
	info, err := os.Stat(filepath.Join(a.Home, "runtime-state.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private state permissions: %v %v", info, err)
	}
}

func TestStopSupersedesInterruptedApplyRecovery(t *testing.T) {
	a, s := installedApp(t)
	if err := a.Control(t.Context(), "start"); err != nil {
		t.Fatal(err)
	}
	m, old, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	_, cfg, _ := a.paths()
	next, err := a.writeGeneration(filepath.Join(a.Home, "installation"), m, read(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(pending{Old: old, New: next, Active: true})
	if err := store.Replace(filepath.Join(a.Home, "apply-pending.json"), raw, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Switch(filepath.Join(a.Home, "installation", "current"), next, old); err != nil {
		t.Fatal(err)
	}
	s.actions = nil
	if err := a.Control(t.Context(), "stop"); err != nil {
		t.Fatal(err)
	}
	if s.active || len(s.actions) != 1 || s.actions[0] != "stop" {
		t.Fatalf("recovery resurrected the service: %v", s.actions)
	}
	_, target, err := a.load()
	if err != nil || target != old {
		t.Fatalf("configuration not recovered: %s %v", target, err)
	}
	assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
}

type preferenceService struct {
	*fakeService
	enabled                  bool
	persistentErr, enableErr error
}

func (s *preferenceService) CheckPersistent(context.Context) error { return s.persistentErr }
func (s *preferenceService) Inspect(ctx context.Context) (service.Status, error) {
	out, err := s.fakeService.Inspect(ctx)
	out.Enabled = s.enabled
	return out, err
}
func (s *preferenceService) SetEnabled(_ context.Context, enabled bool) error {
	if s.enableErr != nil {
		return s.enableErr
	}
	s.enabled = enabled
	return nil
}

func TestAutostartIsDurableIndependentOfCurrentProcess(t *testing.T) {
	a, s := installedApp(t)
	svc := &preferenceService{fakeService: s}
	a.Service = svc
	if err := a.SetAutostart(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	snap, err := a.Snapshot(t.Context())
	if err != nil || !snap.Desired.Autostart || snap.Desired.Running || !snap.Service.Enabled || s.active || len(s.actions) != 0 {
		t.Fatalf("enable changed process: %+v %v", snap, err)
	}
	svc.enableErr = errors.New("authorization denied")
	if err := a.SetAutostart(t.Context(), false); err == nil {
		t.Fatal("denied disable succeeded")
	}
	snap, err = a.Snapshot(t.Context())
	if err != nil || snap.Desired.Autostart || !snap.Service.Enabled || snap.Drift == "" || snap.Operation.State != "failed" {
		t.Fatalf("failed preference hidden: %+v %v", snap, err)
	}
}

func TestMissingLingerBlocksStartBeforeIntentOrAction(t *testing.T) {
	a, s := installedApp(t)
	a.Service = &preferenceService{fakeService: s, persistentErr: errors.New("enable linger explicitly")}
	if err := a.Control(t.Context(), "start"); err == nil {
		t.Fatal("nonpersistent runtime accepted")
	}
	if len(s.actions) != 0 {
		t.Fatalf("unexpected action: %v", s.actions)
	}
	assertMissing(t, filepath.Join(a.Home, "runtime-state.json"))
}

func TestStartRevalidatesConfigurationBeforeChangingIntent(t *testing.T) {
	a, s := installedApp(t)
	a.Validate = func(context.Context, string, string, string) error { return errors.New("configuration invalid") }
	if err := a.Control(t.Context(), "start"); err == nil {
		t.Fatal("invalid config started")
	}
	if len(s.actions) != 0 {
		t.Fatalf("unexpected action: %v", s.actions)
	}
	assertMissing(t, filepath.Join(a.Home, "runtime-state.json"))
}

func TestUnsupportedLocalPreflightNeverExecutesBinary(t *testing.T) {
	a, _ := testApp(t)
	a.Service = nil
	a.PlatformError = errors.New("unprivileged Linux service required")
	a.Version = func(context.Context, string) (string, error) { t.Fatal("unauthorized binary executed"); return "", nil }
	plan, err := a.PrepareInstall(t.Context(), mihomo.Release{}, "/some/local/binary", false)
	if err != nil || plan.BlockedReason == "" {
		t.Fatalf("missing platform block: %+v %v", plan, err)
	}
}

func TestStopStillStopsOwnedServiceWhenRecoveryOrConfigIsBroken(t *testing.T) {
	for _, broken := range []string{"journal", "config", "runtime-state"} {
		t.Run(broken, func(t *testing.T) {
			a, s := installedApp(t)
			if err := a.Control(t.Context(), "start"); err != nil {
				t.Fatal(err)
			}
			switch broken {
			case "journal":
				write(t, filepath.Join(a.Home, "apply-pending.json"), []byte("invalid journal"))
			case "config":
				_, config, _ := a.paths()
				write(t, config, []byte("user modified config"))
			case "runtime-state":
				write(t, filepath.Join(a.Home, "runtime-state.json"), []byte("invalid runtime intent"))
			}
			s.actions = nil
			if err := a.Control(t.Context(), "stop"); err == nil {
				t.Fatal("lost corruption diagnostic")
			}
			if s.active || len(s.actions) != 1 || s.actions[0] != "stop" {
				t.Fatalf("damaged state prevented stop: active=%v actions=%v", s.active, s.actions)
			}
		})
	}
}

func TestStopMarkerSurvivesDamagedStateAndLaterRecovery(t *testing.T) {
	for _, broken := range []string{"config", "runtime-state"} {
		t.Run(broken, func(t *testing.T) {
			a, s := installedApp(t)
			if err := a.Control(t.Context(), "start"); err != nil {
				t.Fatal(err)
			}
			m, old, err := a.load()
			if err != nil {
				t.Fatal(err)
			}
			_, config, _ := a.paths()
			originalConfig := read(t, config)
			runtimePath := filepath.Join(a.Home, "runtime-state.json")
			originalState := read(t, runtimePath)
			next, err := a.writeGeneration(filepath.Join(a.Home, "installation"), m, originalConfig)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(pending{Old: old, New: next, Active: true})
			write(t, filepath.Join(a.Home, "apply-pending.json"), raw)
			if err := store.Switch(filepath.Join(a.Home, "installation", "current"), next, old); err != nil {
				t.Fatal(err)
			}
			if broken == "config" {
				write(t, config, []byte("damaged"))
			} else {
				write(t, runtimePath, []byte("damaged"))
			}
			s.actions = nil
			if err := a.Control(t.Context(), "stop"); err == nil {
				t.Fatal("missing damaged-state error")
			}
			if broken == "config" {
				write(t, config, originalConfig)
			} else {
				write(t, runtimePath, originalState)
			}
			snapshot, err := a.Snapshot(t.Context())
			if err != nil || s.active || snapshot.Desired.Running || len(s.actions) != 1 || s.actions[0] != "stop" {
				t.Fatalf("repair revived explicitly stopped core: %+v %v actions=%v", snapshot, err, s.actions)
			}
			assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
			if err := a.Control(t.Context(), "start"); err != nil {
				t.Fatal(err)
			}
			assertMissing(t, filepath.Join(a.Home, "stop-intent.json"))
			snapshot, err = a.Snapshot(t.Context())
			if err != nil || !snapshot.Desired.Running || !s.active {
				t.Fatalf("explicit start did not supersede stop: %+v %v", snapshot, err)
			}
		})
	}
}

func TestStartWaitsForProxyListenerAfterAPIBecomesReady(t *testing.T) {
	a, s := installedApp(t)
	attempts := 0
	a.ProbeProxy = func(context.Context, int) error {
		attempts++
		if attempts == 1 {
			return errors.New("listener still starting")
		}
		return nil
	}
	if err := a.Control(t.Context(), "start"); err != nil {
		t.Fatal(err)
	}
	if attempts < 2 || !s.active {
		t.Fatal("API availability was mistaken for proxy readiness")
	}
}
