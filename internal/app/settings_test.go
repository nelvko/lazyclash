package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"
	"gopkg.in/yaml.v3"
)

func availableSettings(t *testing.T) RuntimeSettings {
	t.Helper()
	mixed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer mixed.Close()
	udp, err := net.ListenPacket("udp", mixed.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	controller, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	return RuntimeSettings{MixedPort: mixed.Addr().(*net.TCPAddr).Port, ControllerPort: controller.Addr().(*net.TCPAddr).Port}
}

func TestRuntimeSettingsPreserveAppliedConfigurationAndSources(t *testing.T) {
	a, svc := installedApp(t)
	source := filepath.Join(t.TempDir(), "custom.yaml")
	write(t, source, []byte("mode: rule\nproxy-groups: [{name: custom, type: select, proxies: [DIRECT, REJECT]}]\nrules: ['MATCH,custom']\n"))
	if err := a.AddSubscription(t.Context(), "custom", source); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "custom"); err != nil {
		t.Fatal(err)
	}
	before, old, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	oldConfig := read(t, filepath.Join(a.Home, "installation", old, "config.yaml"))
	write(t, source, []byte("mode: direct\nrules: ['MATCH,REJECT']\n"))
	if err := a.Refresh(t.Context(), "custom"); err != nil {
		t.Fatal(err)
	}
	snapshotPath := savedSourcePath(t, a, "custom")
	rawSource := read(t, snapshotPath)
	libraryPath := filepath.Join(a.Home, "configuration", "library.json")
	libraryBefore := read(t, libraryPath)
	proposed := availableSettings(t)
	plan, err := a.PrepareRuntimeSettings(t.Context(), proposed)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(plan)
	if strings.Contains(string(public), before.Secret) || plan.Active || !plan.Changed || plan.AppliedProfile != "custom" {
		t.Fatal("invalid or credential-bearing settings plan")
	}
	result, err := a.ApplyRuntimeSettings(t.Context(), plan)
	if err != nil || result.Settings != proposed || result.Restarted || !result.Changed {
		t.Fatalf("settings apply failed: %+v %v", result, err)
	}
	after, current, err := a.load()
	if err != nil || current == old || manifestSettings(after) != proposed || after.Secret != before.Secret || after.ID != before.ID || after.Applied != before.Applied || after.BinarySHA != before.BinarySHA {
		t.Fatal("settings lost installation identity, credentials or profile", err)
	}
	var previous, next map[string]any
	if yaml.Unmarshal(oldConfig, &previous) != nil || yaml.Unmarshal(read(t, filepath.Join(a.Home, "installation", current, "config.yaml")), &next) != nil {
		t.Fatal("cannot inspect generated YAML")
	}
	for _, key := range []string{"mixed-port", "external-controller"} {
		delete(previous, key)
		delete(next, key)
	}
	if !reflect.DeepEqual(previous, next) || string(read(t, snapshotPath)) != string(rawSource) || string(read(t, libraryPath)) != string(libraryBefore) || len(svc.actions) != 0 || svc.installs != 1 || svc.active {
		t.Fatal("changing ports modified routing, raw source or the stopped service")
	}
	settings, err := a.GetRuntimeSettings(t.Context())
	if err != nil || settings != proposed {
		t.Fatal("saved runtime settings not returned", err)
	}
	if err := a.Apply(t.Context(), "default"); err != nil {
		t.Fatal(err)
	}
	settings, err = a.GetRuntimeSettings(t.Context())
	if err != nil || settings != proposed {
		t.Fatal("applying another profile reset runtime ports", err)
	}
}

func TestRuntimeSettingsNoopDoesNotValidateWriteOrRestart(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(fmt.Sprint(active), func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = active
			validations := 0
			a.Validate = func(context.Context, string, string, string) error { validations++; return nil }
			before, _ := installationTree(filepath.Join(a.Home, "installation"))
			settings, err := a.GetRuntimeSettings(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			plan, err := a.PrepareRuntimeSettings(t.Context(), settings)
			if err != nil || plan.Changed {
				t.Fatal("same settings were not treated as a no-op", err)
			}
			result, err := a.ApplyRuntimeSettings(t.Context(), plan)
			after, _ := installationTree(filepath.Join(a.Home, "installation"))
			if err != nil || result.Changed || result.Restarted || !maps.Equal(before, after) || validations != 0 || len(svc.actions) != 0 {
				t.Fatal("no-op changed installation or service", err)
			}
		})
	}
}

func TestRuntimeSettingsValidationConflictsAndOwnershipDoNotMutate(t *testing.T) {
	for _, mode := range []string{"range", "same", "tcp-mixed", "udp-mixed", "tcp-controller", "foreign-unit", "modified-config"} {
		t.Run(mode, func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = true
			next := availableSettings(t)
			switch mode {
			case "range":
				next.MixedPort = 1023
			case "same":
				next.ControllerPort = next.MixedPort
			case "tcp-mixed", "tcp-controller":
				port := next.MixedPort
				if mode == "tcp-controller" {
					port = next.ControllerPort
				}
				l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
			case "udp-mixed":
				l, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", next.MixedPort))
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
			case "foreign-unit":
				svc.verifyErr = errors.New("unit belongs to another installation")
			case "modified-config":
				_, current, _ := a.load()
				write(t, filepath.Join(a.Home, "installation", current, "config.yaml"), []byte("user: configuration\n"))
			}
			before, _ := installationTree(filepath.Join(a.Home, "installation"))
			if _, err := a.PrepareRuntimeSettings(t.Context(), next); err == nil {
				t.Fatal("unsafe port settings accepted")
			}
			after, _ := installationTree(filepath.Join(a.Home, "installation"))
			if !maps.Equal(before, after) || len(svc.actions) != 0 || !svc.active {
				t.Fatal("preflight changed files or stopped a service")
			}
		})
	}
}

func TestRuntimeSettingsPlanRejectsMutationOtherHomeAndChangedProcess(t *testing.T) {
	for _, mode := range []string{"mutated", "other-home", "process", "active-state", "generation"} {
		t.Run(mode, func(t *testing.T) {
			a, svc := installedApp(t)
			var pid atomic.Int64
			pid.Store(10)
			a.Service = monitorService{Services: svc, inspect: func(context.Context) (service.Status, error) {
				return service.Status{Active: svc.active, State: map[bool]string{true: "active", false: "inactive"}[svc.active], PID: int(pid.Load())}, nil
			}}
			plan, err := a.PrepareRuntimeSettings(t.Context(), availableSettings(t))
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "mutated":
				plan.Proposed.MixedPort++
			case "other-home":
				a, svc = installedApp(t)
			case "process":
				pid.Store(20)
			case "active-state":
				svc.active = true
			case "generation":
				if err := a.Apply(t.Context(), "default"); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := installationTree(filepath.Join(a.Home, "installation"))
			if _, err := a.ApplyRuntimeSettings(t.Context(), plan); err == nil {
				t.Fatal("stale or changed plan was applied")
			}
			after, _ := installationTree(filepath.Join(a.Home, "installation"))
			if !maps.Equal(before, after) || len(svc.actions) != 0 {
				t.Fatal("rejected plan changed installation or service")
			}
		})
	}
}

func TestActiveRuntimeSettingsSwapOwnedPortsWithoutStoppingForeignUDP(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			a, svc := installedApp(t)
			initial := availableSettings(t)
			plan, err := a.PrepareRuntimeSettings(t.Context(), initial)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.ApplyRuntimeSettings(t.Context(), plan); err != nil {
				t.Fatal(err)
			}
			mixedTCP, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", initial.MixedPort))
			if err != nil {
				t.Fatal(err)
			}
			defer mixedTCP.Close()
			mixedUDP, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", initial.MixedPort))
			if err != nil {
				t.Fatal(err)
			}
			defer mixedUDP.Close()
			controllerTCP, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", initial.ControllerPort))
			if err != nil {
				t.Fatal(err)
			}
			defer controllerTCP.Close()
			svc.active = true
			next := RuntimeSettings{MixedPort: initial.ControllerPort, ControllerPort: initial.MixedPort}
			if conflict {
				foreign, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", initial.ControllerPort))
				if err != nil {
					t.Fatal(err)
				}
				defer foreign.Close()
				if _, err := a.PrepareRuntimeSettings(t.Context(), next); err == nil || !strings.Contains(err.Error(), "udp") {
					t.Fatal("foreign UDP listener was not rejected before stopping", err)
				}
				if len(svc.actions) != 0 || !svc.active {
					t.Fatal("UDP conflict stopped the managed service")
				}
				return
			}
			plan, err = a.PrepareRuntimeSettings(t.Context(), next)
			if err != nil {
				t.Fatal("owned active endpoints blocked a safe swap", err)
			}
			svc.actionHook = func(_ context.Context, action string) error {
				if action == "stop" {
					_ = mixedTCP.Close()
					_ = mixedUDP.Close()
					_ = controllerTCP.Close()
				}
				return nil
			}
			if _, err := a.ApplyRuntimeSettings(t.Context(), plan); err != nil {
				t.Fatal("safe active swap failed", err)
			}
			actual, err := a.GetRuntimeSettings(t.Context())
			if err != nil || actual != next || !svc.active {
				t.Fatal("swapped settings not committed", err)
			}
		})
	}
}

func TestRuntimeSettingsRecheckProcessAfterEngineValidation(t *testing.T) {
	a, svc := installedApp(t)
	var pid atomic.Int64
	pid.Store(10)
	a.Service = monitorService{Services: svc, inspect: func(context.Context) (service.Status, error) {
		return service.Status{Active: true, State: "active", PID: int(pid.Load())}, nil
	}}
	validations := 0
	a.Validate = func(context.Context, string, string, string) error {
		validations++
		if validations == 2 {
			pid.Store(20)
		}
		return nil
	}
	plan, err := a.PrepareRuntimeSettings(t.Context(), availableSettings(t))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := installationTree(filepath.Join(a.Home, "installation"))
	if _, err := a.ApplyRuntimeSettings(t.Context(), plan); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatal("process replacement during validation was not rejected", err)
	}
	after, _ := installationTree(filepath.Join(a.Home, "installation"))
	if !maps.Equal(before, after) || len(svc.actions) != 0 {
		t.Fatal("changed process was stopped or staged files were retained")
	}
}

func TestActiveRuntimeSettingsRestartAndFailureRecovery(t *testing.T) {
	for _, mode := range []string{"success", "restart-failure", "cancel-stop", "cancel-restart", "recovery-failure", "post-stop-conflict"} {
		t.Run(mode, func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = true
			before, old, _ := a.load()
			proposed := availableSettings(t)
			plan, err := a.PrepareRuntimeSettings(t.Context(), proposed)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			restarts := 0
			var intruder net.Listener
			defer func() {
				if intruder != nil {
					intruder.Close()
				}
			}()
			svc.actionHook = func(actionCtx context.Context, action string) error {
				if action == "stop" && mode == "cancel-stop" {
					svc.active = false
					cancel()
					return context.Canceled
				}
				if action == "stop" && mode == "post-stop-conflict" {
					var err error
					intruder, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", proposed.MixedPort))
					return err
				}
				if action == "restart" {
					restarts++
					if mode == "recovery-failure" || mode == "restart-failure" && restarts == 1 {
						return errors.New("service restart fixture failure")
					}
					if mode == "cancel-restart" && restarts == 1 {
						cancel()
						return context.Canceled
					}
				}
				return actionCtx.Err()
			}
			result, err := a.ApplyRuntimeSettings(ctx, plan)
			after, current, loadErr := a.load()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if mode == "success" {
				if err != nil || !result.Restarted || !svc.active || current == old || manifestSettings(after) != proposed || !reflect.DeepEqual(svc.actions, []string{"stop", "restart"}) {
					t.Fatalf("bad running transition: %+v %v %v", result, svc.actions, err)
				}
				return
			}
			if err == nil || current != old || manifestSettings(after) != manifestSettings(before) || after.Secret != before.Secret {
				t.Fatalf("failed settings did not restore old state: %v", err)
			}
			if strings.HasPrefix(mode, "cancel-") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation was lost", err)
			}
			if mode == "recovery-failure" {
				journal := read(t, filepath.Join(a.Home, "apply-pending.json"))
				var p pending
				if json.Unmarshal(journal, &p) != nil {
					t.Fatal("recovery journal missing")
				}
				if _, err := os.Stat(filepath.Join(a.Home, "installation", p.New)); err != nil {
					t.Fatal("failed recovery deleted its staged generation")
				}
				svc.actionHook = nil
				if _, err := a.Snapshot(t.Context()); err != nil || !svc.active {
					t.Fatal("later recovery failed", err)
				}
			} else if !svc.active {
				t.Fatal("original running state was not recovered")
			}
			assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
		})
	}
}

func TestGuardedSettingsRecoveryPreservesLaterUserChanges(t *testing.T) {
	for _, changed := range []string{"none", "old-config", "new-config", "new-manifest", "new-file", "binary", "unit"} {
		t.Run(changed, func(t *testing.T) {
			a, svc := installedApp(t)
			m, old, _ := a.load()
			plan, _, _, cfg, err := a.prepareRuntimeSettingsLocked(t.Context(), availableSettings(t))
			if err != nil {
				t.Fatal(err)
			}
			m.Port = plan.Proposed.MixedPort
			m.Controller = net.JoinHostPort(plan.ControllerHost, fmt.Sprint(plan.Proposed.ControllerPort))
			next, err := a.writeGeneration(filepath.Join(a.Home, "installation"), m, cfg)
			if err != nil {
				t.Fatal(err)
			}
			oldGuard, _ := a.generationGuard(old)
			newGuard, _ := a.generationGuard(next)
			p := pending{Old: old, New: next, Active: true, OldGuard: oldGuard, NewGuard: newGuard}
			raw, _ := json.Marshal(p)
			if err := store.Replace(filepath.Join(a.Home, "apply-pending.json"), raw, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := store.Switch(filepath.Join(a.Home, "installation", "current"), next, old); err != nil {
				t.Fatal(err)
			}
			switch changed {
			case "old-config":
				write(t, filepath.Join(a.Home, "installation", old, "config.yaml"), []byte("user: old\n"))
			case "new-config":
				write(t, filepath.Join(a.Home, "installation", next, "config.yaml"), []byte("user: new\n"))
			case "new-manifest":
				path := filepath.Join(a.Home, "installation", next, "manifest.json")
				write(t, path, append(read(t, path), ' '))
			case "new-file":
				write(t, filepath.Join(a.Home, "installation", next, "notes"), []byte("keep me"))
			case "binary":
				write(t, filepath.Join(a.Home, "installation", "mihomo"), []byte("user binary"))
			case "unit":
				svc.verifyErr = errors.New("unit changed")
			}
			before, _ := installationTree(filepath.Join(a.Home, "installation"))
			_, err = a.Snapshot(t.Context())
			if changed == "none" {
				if err != nil || !svc.active {
					t.Fatal("interrupted settings did not recover", err)
				}
				_, target, _ := a.load()
				if target != old {
					t.Fatal("recovery did not restore old pointer")
				}
				assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
				return
			}
			if err == nil {
				t.Fatal("changed recovery generation accepted")
			}
			after, _ := installationTree(filepath.Join(a.Home, "installation"))
			if !maps.Equal(before, after) || len(svc.actions) != 0 {
				t.Fatal("recovery changed user files or service")
			}
			if _, err := os.Stat(filepath.Join(a.Home, "apply-pending.json")); err != nil {
				t.Fatal("recovery journal was lost")
			}
		})
	}
}

func TestFailedRuntimeSettingsKeepModifiedNewGenerationAndRecoveryJournal(t *testing.T) {
	for _, change := range []string{"config", "extra-file", "unit"} {
		t.Run(change, func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = true
			_, old, _ := a.load()
			plan, err := a.PrepareRuntimeSettings(t.Context(), availableSettings(t))
			if err != nil {
				t.Fatal(err)
			}
			svc.actionHook = func(_ context.Context, action string) error {
				if action != "restart" {
					return nil
				}
				target, err := os.Readlink(filepath.Join(a.Home, "installation", "current"))
				if err != nil {
					return err
				}
				switch change {
				case "config":
					write(t, filepath.Join(a.Home, "installation", target, "config.yaml"), []byte("user: changed during deployment\n"))
				case "extra-file":
					write(t, filepath.Join(a.Home, "installation", target, "keep"), []byte("user notes"))
				case "unit":
					svc.verifyErr = errors.New("unit changed during deployment")
				}
				return errors.New("fixture failed startup")
			}
			if _, err := a.ApplyRuntimeSettings(t.Context(), plan); err == nil {
				t.Fatal("failed startup reported success")
			}
			var p pending
			if json.Unmarshal(read(t, filepath.Join(a.Home, "apply-pending.json")), &p) != nil || p.Old != old {
				t.Fatal("journal was not retained")
			}
			if _, err := os.Stat(filepath.Join(a.Home, "installation", p.New)); err != nil {
				t.Fatal("rollback deleted modified generation", err)
			}
			target, _ := os.Readlink(filepath.Join(a.Home, "installation", "current"))
			if target != p.New || !reflect.DeepEqual(svc.actions, []string{"stop", "restart"}) {
				t.Fatal("unsafe recovery moved changed pointer or acted on changed service")
			}
		})
	}
}

func TestRuntimeSettingsEngineValidationFailureLeavesFilesAndServiceUntouched(t *testing.T) {
	a, svc := installedApp(t)
	svc.active = true
	before, _ := installationTree(filepath.Join(a.Home, "installation"))
	a.Validate = func(context.Context, string, string, string) error {
		return errors.New("engine rejected proposed configuration")
	}
	if _, err := a.PrepareRuntimeSettings(t.Context(), availableSettings(t)); err == nil {
		t.Fatal("engine failure accepted")
	}
	after, _ := installationTree(filepath.Join(a.Home, "installation"))
	if !maps.Equal(before, after) || len(svc.actions) != 0 {
		t.Fatal("engine preflight failure touched current state")
	}
}
