package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

type ModeStatus struct {
	Desired string `json:"desired"`
	Actual  string `json:"actual,omitempty"`
	Running bool   `json:"running"`
	Error   string `json:"error,omitempty"`
}

func (a *App) GetMode(ctx context.Context) (ModeStatus, error) {
	var out ModeStatus
	err := a.locked(ctx, func() error {
		if err := a.requireService(); err != nil {
			return err
		}
		m, _, err := a.load()
		if err != nil {
			return err
		}
		bin, config, data := a.paths()
		if err := a.Service.Verify(ctx, bin, config, data); err != nil {
			return err
		}
		raw, err := store.Read(config)
		if err != nil {
			return err
		}
		out.Desired, err = mihomo.ConfigMode(raw)
		if err != nil {
			return err
		}
		status, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		out.Running = status.Active
		if out.Running {
			cfg, err := a.client(m).Configuration(ctx)
			if err != nil {
				return err
			}
			out.Actual = cfg.Mode
		}
		return nil
	})
	return out, err
}

// SetMode commits the restart configuration before applying a live API change.
// The existing guarded apply journal restores the old generation after a failed
// PATCH, failed readback or interrupted operation. Successful changes need no
// restart and remain effective when systemd later restarts Mihomo.
func (a *App) SetMode(ctx context.Context, mode string) error {
	if !mihomo.ValidMode(mode) {
		return errors.New("proxy mode must be rule, global or direct")
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
			return errors.New("service is changing state; wait before changing the proxy mode")
		}
		raw, err := store.Read(config)
		if err != nil {
			return err
		}
		nextConfig, err := mihomo.WithMode(raw, mode)
		if err != nil {
			return err
		}
		if err := a.validateConfig(ctx, m, nextConfig); err != nil {
			return err
		}
		if m.Mode == mode && string(raw) == string(nextConfig) {
			if status.Active {
				return a.client(m).SetMode(ctx, mode)
			}
			return nil
		}
		m.Mode = mode
		base := filepath.Join(a.Home, "installation")
		next, err := a.writeGeneration(base, m, nextConfig)
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
		if status.Active != observed.Active || status.State != observed.State || status.PID != observed.PID {
			return errors.New("service changed while validating the proxy mode; retry the operation")
		}
		if err := a.Service.Verify(ctx, bin, config, data); err != nil {
			return err
		}
		p := pending{Old: old, New: next, Active: status.Active, OldGuard: oldGuard, NewGuard: newGuard}
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
		if err := store.Switch(filepath.Join(base, "current"), next, old); err != nil {
			return rollback(err)
		}
		if status.Active {
			if err := a.client(m).SetMode(ctx, mode); err != nil {
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

// SetTUN makes the current privilege boundary explicit. This release only
// manages unprivileged user services; it never asks the whole TUI to run as root.
// Disabling is a read-only confirmation because generated configurations already
// prohibit TUN. An unexpectedly enabled runtime is reported for inspection.
func (a *App) SetTUN(ctx context.Context, enabled bool) error {
	if enabled {
		return errors.New("TUN is unavailable in the managed systemd user service: privileged system-service support is not implemented; ordinary HTTP/SOCKS proxying requires no root")
	}
	return a.locked(ctx, func() error {
		if err := a.requireService(); err != nil {
			return err
		}
		m, _, err := a.load()
		if err != nil {
			return err
		}
		bin, config, data := a.paths()
		if err := a.Service.Verify(ctx, bin, config, data); err != nil {
			return err
		}
		raw, err := store.Read(config)
		if err != nil {
			return err
		}
		on, err := mihomo.ConfigTUN(raw)
		if err != nil {
			return err
		}
		if on {
			return errors.New("managed configuration unexpectedly enables TUN; inspect it before restarting the service")
		}
		status, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		if status.Active {
			cfg, err := a.client(m).Configuration(ctx)
			if err != nil {
				return fmt.Errorf("cannot confirm that TUN is disabled: %w", err)
			}
			if cfg.TUN {
				return errors.New("Mihomo unexpectedly reports active TUN in a user service; stop the service and inspect its configuration")
			}
		}
		return nil
	})
}
