package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

// RuntimeSettings owns the managed local listeners. The existing loopback
// address, credentials, routing configuration and raw profiles are preserved.
type RuntimeSettings struct {
	MixedPort      int `json:"mixed_port"`
	ControllerPort int `json:"controller_port"`
}

type RuntimeSettingsPlan struct {
	Current        RuntimeSettings `json:"current"`
	Proposed       RuntimeSettings `json:"proposed"`
	ControllerHost string          `json:"controller_host"`
	Active         bool            `json:"active"`
	Changed        bool            `json:"changed"`
	AppliedProfile string          `json:"applied_profile"`
	guard, seal    string
}

type RuntimeSettingsResult struct {
	Settings   RuntimeSettings `json:"settings"`
	Controller string          `json:"controller"`
	Restarted  bool            `json:"restarted"`
	Changed    bool            `json:"changed"`
}

func manifestSettings(m manifest) RuntimeSettings {
	_, port, _ := net.SplitHostPort(m.Controller)
	controllerPort, _ := strconv.Atoi(port)
	return RuntimeSettings{MixedPort: m.Port, ControllerPort: controllerPort}
}

func (a *App) GetRuntimeSettings(ctx context.Context) (RuntimeSettings, error) {
	var settings RuntimeSettings
	err := a.locked(ctx, func() error {
		m, _, err := a.load()
		if err != nil {
			return err
		}
		settings = manifestSettings(m)
		return nil
	})
	return settings, err
}

func validateRuntimeSettings(settings RuntimeSettings) error {
	if settings.MixedPort < 1024 || settings.MixedPort > 65535 || settings.ControllerPort < 1024 || settings.ControllerPort > 65535 {
		return errors.New("proxy and controller ports must be between 1024 and 65535")
	}
	if settings.MixedPort == settings.ControllerPort {
		return errors.New("proxy and controller ports must differ")
	}
	return nil
}

func runtimeSettingsSeal(plan RuntimeSettingsPlan) string {
	raw, _ := json.Marshal(plan)
	return digest(append(raw, []byte(plan.guard)...))
}

func (a *App) PrepareRuntimeSettings(ctx context.Context, proposed RuntimeSettings) (RuntimeSettingsPlan, error) {
	var plan RuntimeSettingsPlan
	err := a.locked(ctx, func() error {
		var err error
		plan, _, _, _, err = a.prepareRuntimeSettingsLocked(ctx, proposed)
		return err
	})
	return plan, err
}

func (a *App) prepareRuntimeSettingsLocked(ctx context.Context, proposed RuntimeSettings) (RuntimeSettingsPlan, manifest, string, []byte, error) {
	var plan RuntimeSettingsPlan
	if err := validateRuntimeSettings(proposed); err != nil {
		return plan, manifest{}, "", nil, err
	}
	if err := a.requireService(); err != nil {
		return plan, manifest{}, "", nil, err
	}
	m, target, err := a.load()
	if err != nil {
		return plan, m, target, nil, err
	}
	bin, config, data := a.paths()
	if err := a.Service.Verify(ctx, bin, config, data); err != nil {
		return plan, m, target, nil, err
	}
	status, err := a.Service.Inspect(ctx)
	if err != nil {
		return plan, m, target, nil, err
	}
	if status.State == "activating" || status.State == "deactivating" || status.State == "reloading" {
		return plan, m, target, nil, errors.New("service is changing state; wait and review the port settings again")
	}
	host, _, _ := net.SplitHostPort(m.Controller)
	plan = RuntimeSettingsPlan{Current: manifestSettings(m), Proposed: proposed, ControllerHost: host, Active: status.Active, AppliedProfile: m.Applied}
	plan.Changed = plan.Current != plan.Proposed
	controller := net.JoinHostPort(host, strconv.Itoa(proposed.ControllerPort))
	// Read the actual applied generation, not the saved source: a raw profile
	// may have been edited since the user last applied it.
	raw, err := store.Read(config)
	if err != nil {
		return plan, m, target, nil, err
	}
	effective, err := mihomo.Effective(raw, controller, m.Secret, proposed.MixedPort)
	if err != nil {
		return plan, m, target, nil, err
	}
	if plan.Changed {
		if err := checkRuntimePorts(ctx, m, proposed, status.Active); err != nil {
			return plan, m, target, nil, err
		}
		if err := a.validateConfig(ctx, m, effective); err != nil {
			return plan, m, target, nil, err
		}
	}
	guard, err := a.generationGuard(target)
	if err != nil {
		return plan, m, target, nil, err
	}
	identity, _ := json.Marshal([]any{a.Home, a.Name, target, guard, m.BinarySHA, status.Active, status.State, status.PID})
	plan.guard = digest(identity)
	plan.seal = runtimeSettingsSeal(plan)
	return plan, m, target, effective, ctx.Err()
}

// checkRuntimePorts binds only candidate sockets. A running, verified managed
// service may own its unchanged TCP endpoints and its current mixed UDP port;
// all other listeners, including a foreign UDP socket during a TCP-port swap,
// are tested before stopping anything. Apply repeats a full probe after stop.
func checkRuntimePorts(ctx context.Context, current manifest, next RuntimeSettings, active bool) error {
	host, _, _ := net.SplitHostPort(current.Controller)
	controller := net.JoinHostPort(host, strconv.Itoa(next.ControllerPort))
	mixed := net.JoinHostPort("127.0.0.1", strconv.Itoa(next.MixedPort))
	oldMixed := net.JoinHostPort("127.0.0.1", strconv.Itoa(current.Port))
	for _, endpoint := range []struct{ network, address string }{{"tcp", controller}, {"tcp", mixed}, {"udp", mixed}} {
		if err := ctx.Err(); err != nil {
			return err
		}
		owned := active && (endpoint.address == oldMixed || endpoint.network == "tcp" && endpoint.address == current.Controller)
		if owned {
			continue
		}
		var closer interface{ Close() error }
		var err error
		if endpoint.network == "udp" {
			closer, err = net.ListenPacket("udp", endpoint.address)
		} else {
			closer, err = net.Listen("tcp", endpoint.address)
		}
		if err != nil {
			return fmt.Errorf("local %s port %s is unavailable; no other service was stopped", endpoint.network, endpoint.address)
		}
		if err := closer.Close(); err != nil {
			return errors.New("port check could not release its temporary socket")
		}
	}
	return ctx.Err()
}

// ApplyRuntimeSettings accepts only a reviewed, unchanged plan. The persistent
// generation pointer and manifest move together; the existing apply journal
// restores the previous ports and prior running state after errors or crashes.
func (a *App) ApplyRuntimeSettings(ctx context.Context, plan RuntimeSettingsPlan) (RuntimeSettingsResult, error) {
	var result RuntimeSettingsResult
	err := a.locked(ctx, func() error {
		if plan.seal == "" || plan.seal != runtimeSettingsSeal(plan) {
			return errors.New("port settings plan changed; review the settings again")
		}
		fresh, m, old, config, err := a.prepareRuntimeSettingsLocked(ctx, plan.Proposed)
		if err != nil {
			return err
		}
		if fresh.guard != plan.guard {
			return errors.New("installation or service changed since review; review the port settings again")
		}
		controller := net.JoinHostPort(fresh.ControllerHost, strconv.Itoa(fresh.Proposed.ControllerPort))
		if !fresh.Changed {
			result = RuntimeSettingsResult{Settings: fresh.Current, Controller: controller}
			return nil
		}
		m.Controller, m.Port = controller, fresh.Proposed.MixedPort
		base := filepath.Join(a.Home, "installation")
		next, err := a.writeGeneration(base, m, config)
		if err != nil {
			return err
		}
		keep := false
		defer func() {
			if !keep {
				_ = os.RemoveAll(filepath.Join(base, next))
			}
		}()
		oldGuard, err := a.generationGuard(old)
		if err != nil {
			return err
		}
		newGuard, err := a.generationGuard(next)
		if err != nil {
			return err
		}
		// Validation may be slow; inspect again immediately before authorizing
		// the stop. A different running process needs a new user review.
		status, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		identity, _ := json.Marshal([]any{a.Home, a.Name, old, oldGuard, m.BinarySHA, status.Active, status.State, status.PID})
		if digest(identity) != fresh.guard {
			return errors.New("service changed while checking the settings; review them again")
		}
		bin, activeConfig, data := a.paths()
		if err := a.Service.Verify(ctx, bin, activeConfig, data); err != nil {
			return err
		}
		p := pending{Old: old, New: next, Active: fresh.Active, OldGuard: oldGuard, NewGuard: newGuard}
		raw, _ := json.Marshal(p)
		journal := filepath.Join(a.Home, "apply-pending.json")
		if err := store.Replace(journal, raw, nil, 0600); err != nil {
			return err
		}
		rollback := func(cause error) error {
			cleanup, cancel := cleanupContext()
			defer cancel()
			recoveryErr := a.recover(cleanup)
			keep = recoveryErr != nil
			return errors.Join(cause, recoveryErr)
		}
		if fresh.Active {
			if err := a.Service.Action(ctx, "stop"); err != nil {
				return rollback(err)
			}
			stopped, err := a.Service.Inspect(ctx)
			if err != nil || stopped.Active {
				return rollback(errors.Join(errors.New("managed service did not stop before changing ports"), err))
			}
		}
		if err := checkRuntimePorts(ctx, m, fresh.Proposed, false); err != nil {
			return rollback(err)
		}
		if err := store.Switch(filepath.Join(base, "current"), next, old); err != nil {
			return rollback(err)
		}
		if fresh.Active {
			if err := a.restartReady(ctx, m); err != nil {
				return rollback(err)
			}
		}
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		if err := os.Remove(journal); err != nil {
			return rollback(err)
		}
		keep = true
		result = RuntimeSettingsResult{Settings: fresh.Proposed, Controller: controller, Restarted: fresh.Active, Changed: true}
		return nil
	})
	return result, err
}

// generationGuard protects both settings metadata and bytes. Recovery checks
// both generations before moving the pointer, preserving any later user edits.
func (a *App) generationGuard(target string) (string, error) {
	if !generationName.MatchString(target) {
		return "", errors.New("invalid configuration generation")
	}
	dir := filepath.Join(a.Home, "installation", target)
	if err := existingDir(dir); err != nil {
		return "", err
	}
	raw, err := store.Read(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return "", err
	}
	config, err := store.Read(filepath.Join(dir, "config.yaml"))
	if err != nil {
		return "", err
	}
	var m manifest
	if json.Unmarshal(raw, &m) != nil || m.ID == "" || m.Service != a.Name || digest(config) != m.SHA {
		return "", errors.New("configuration generation changed; preserving files for manual inspection")
	}
	tree, err := installationTree(dir)
	if err != nil {
		return "", err
	}
	if tree["manifest.json"] != "file:"+digest(raw) || tree["config.yaml"] != "file:"+digest(config) {
		return "", errors.New("configuration changed while checking its generation; review again")
	}
	identity, _ := json.Marshal(tree)
	return digest(identity), nil
}
