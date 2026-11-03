package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"
)

type tunTransition struct {
	Old bool `json:"old"`
	New bool `json:"new"`
}

func (a *App) bindTUN(m manifest, raw []byte) error {
	enabled, err := mihomo.ConfigTUN(raw)
	if err != nil {
		return err
	}
	if enabled != m.TUN {
		return fmt.Errorf("saved TUN policy differs from the managed configuration")
	}
	if s, ok := a.Service.(*service.Systemd); ok {
		if m.TUN && s.Scope != service.System {
			return fmt.Errorf("TUN is not supported by a user service")
		}
		s.TUN = m.TUN
	}
	return nil
}

func (a *App) verifyForStop(ctx context.Context, bin, config, data string) error {
	if s, ok := a.Service.(interface {
		VerifyForStop(context.Context, string, string, string) error
	}); ok {
		return s.VerifyForStop(ctx, bin, config, data)
	}
	return a.Service.Verify(ctx, bin, config, data)
}

func (a *App) setTUNPolicy(ctx context.Context, enabled bool) error {
	s, ok := a.Service.(interface {
		SetTUNPolicy(context.Context, bool, string, string, string) error
	})
	if !ok {
		if enabled {
			return fmt.Errorf("TUN requires root's system service")
		}
		return nil
	}
	bin, config, data := a.paths()
	return s.SetTUNPolicy(ctx, enabled, bin, config, data)
}

// SetTUN applies the host traffic policy after the CLI or TUI has obtained the
// explicit scope confirmation. User instances never request elevation.
func (a *App) SetTUN(ctx context.Context, enabled bool) error {
	if enabled && !a.Capabilities().TUN.Supported {
		return diagnostic.New("diagnostic.runtime.tunUnavailable", "TUN requires root's system service; an ordinary user service supports HTTP/SOCKS proxying only")
	}
	return a.locked(ctx, func() error {
		if err := a.requireService(); err != nil {
			return err
		}
		m, old, err := a.load()
		if err != nil {
			return err
		}
		bin, config, data := a.paths()
		if err := a.Service.Verify(ctx, bin, config, data); err != nil {
			return err
		}
		status, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		if status.State == "activating" || status.State == "deactivating" || status.State == "reloading" {
			return fmt.Errorf("service is changing state; wait before changing TUN")
		}
		if m.TUN == enabled {
			if status.Active {
				cfg, err := a.client(m).Configuration(ctx)
				if err != nil {
					return fmt.Errorf("cannot confirm the running TUN state: %w", err)
				}
				if cfg.TUN != enabled {
					return fmt.Errorf("running TUN differs from saved policy; restart the service")
				}
			}
			return nil
		}
		if enabled {
			if err := a.checkTUNConflict(ctx, m); err != nil {
				return err
			}
		}
		raw, err := store.Read(config)
		if err != nil {
			return err
		}
		nextConfig, err := mihomo.WithTUN(raw, enabled, a.Name)
		if err != nil {
			return err
		}
		if err := a.validateConfig(ctx, m, nextConfig); err != nil {
			return err
		}
		nextManifest := m
		nextManifest.TUN = enabled
		base := filepath.Join(a.Home, "installation")
		next, err := a.writeGeneration(base, nextManifest, nextConfig)
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
		observed, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		if observed.Active != status.Active || observed.State != status.State || observed.PID != status.PID {
			return fmt.Errorf("service changed while validating TUN; retry")
		}
		p := pending{Old: old, New: next, Active: status.Active, OldGuard: oldGuard, NewGuard: newGuard, TUNChange: &tunTransition{Old: m.TUN, New: enabled}}
		journal := filepath.Join(a.Home, "apply-pending.json")
		encoded, _ := json.Marshal(p)
		if err := store.Replace(journal, encoded, nil, 0600); err != nil {
			return err
		}
		rollback := func(cause error) error {
			cleanup, cancel := cleanupContext()
			defer cancel()
			recoveryErr := a.recover(cleanup)
			keep = recoveryErr != nil
			return errors.Join(cause, recoveryErr)
		}
		// Stop while the previous policy is still published so its cleanup hook can
		// identify its own resources, then update the capabilities and generation.
		if status.Active {
			if err := a.Service.Action(ctx, "stop"); err != nil {
				return rollback(err)
			}
		}
		if err := a.setTUNPolicy(ctx, enabled); err != nil {
			return rollback(err)
		}
		if err := store.Switch(filepath.Join(base, "current"), next, old); err != nil {
			return rollback(err)
		}
		if status.Active {
			if err := a.restartReady(ctx, nextManifest); err != nil {
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
		return nil
	})
}

// ServiceLifecycle is a finite root operation invoked by systemd. It deliberately
// does not take the application lock: the caller may be waiting on systemctl.
func (a *App) ServiceLifecycle(ctx context.Context, action string) error {
	s, ok := a.Service.(*service.Systemd)
	if !ok || s.Scope != service.System || os.Geteuid() != 0 {
		return fmt.Errorf("service lifecycle requires root's system service")
	}
	if action == "cleanup" {
		id, err := a.lifecycleIdentity()
		if err != nil {
			return err
		}
		if err := trustedRootFile(s.Lifecycle); err != nil {
			return err
		}
		bin, config, data := a.paths()
		if err := a.verifyForStop(ctx, bin, config, data); err != nil {
			return err
		}
		return (service.TUNLifecycle{Home: a.Home, Name: a.Name, InstanceID: id}).Cleanup(ctx)
	}
	m, _, err := a.load()
	if err != nil {
		return err
	}
	bin, config, data := a.paths()
	if err := a.Service.Verify(ctx, bin, config, data); err != nil {
		return err
	}
	lifecycle := service.TUNLifecycle{Home: a.Home, Name: a.Name, InstanceID: m.ID, Enabled: m.TUN}
	switch action {
	case "prepare":
		return lifecycle.Prepare(ctx)
	case "ready":
		if err := lifecycle.Ready(ctx); err != nil {
			return err
		}
		if m.TUN {
			return a.waitTUNAPI(ctx, m)
		}
		return nil
	default:
		return fmt.Errorf("invalid service lifecycle action")
	}
}

func (a *App) checkTUNConflict(ctx context.Context, m manifest) error {
	if s, ok := a.Service.(*service.Systemd); ok && s.Scope == service.System {
		return (service.TUNLifecycle{Home: a.Home, Name: a.Name, InstanceID: m.ID, Enabled: true}).CheckConflict(ctx)
	}
	return nil
}

// Mihomo exposes the TUN interface before its stack initialization completes.
// The systemd post-start hook must also observe the final API state, without
// waiting on ActiveState (which itself waits for this hook).
func (a *App) waitTUNAPI(ctx context.Context, m manifest) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		cfg, err := a.client(m).Configuration(ctx)
		if err == nil && cfg.TUN {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Mihomo TUN API did not become ready: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}
