package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/store"
)

func (a *App) Apply(ctx context.Context, name string) error {
	return a.locked(ctx, func() error {
		lib, _, err := a.library().Load()
		if err != nil {
			return err
		}
		id, err := resolveProfileID(lib, name)
		if err != nil {
			return err
		}
		return a.applyProfileLocked(ctx, lib, id, nil)
	})
}

// DeactivateProfile keeps an explicit card action bound to its original ID.
// An empty expectedID deliberately addresses whichever profile is selected.
func (a *App) DeactivateProfile(ctx context.Context, expectedID string) error {
	return a.locked(ctx, func() error {
		m, installed, err := a.buildContext()
		if err != nil || !installed || m.selectedProfile() == "" {
			return err
		}
		if expectedID != "" && expectedID != m.Applied {
			return diagnostic.New("diagnostic.profile.deactivateSelectionChanged", "the selected profile changed; review the selection before deactivating it")
		}
		return a.applyProfileLocked(ctx, configuration.Library{}, "default", nil)
	})
}

func (a *App) applyProfileLocked(ctx context.Context, lib configuration.Library, name string, reviewed *CustomRulesDraft) error {
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
		return diagnostic.New("diagnostic.profile.serviceChanging", "service is changing state; wait before applying a profile")
	}
	if reviewed != nil {
		if err := validateCustomRulesApply(lib, m, name, *reviewed); err != nil {
			return err
		}
	}
	cfg, inputs, err := a.buildProfile(ctx, lib, name, m, true)
	if err != nil {
		return err
	}
	m.Build = &inputs
	m.Applied = name
	base := filepath.Join(a.Home, "installation")
	next, err := a.writeGeneration(base, m, cfg)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(filepath.Join(base, next))
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	oldGuard, err := a.generationGuard(old)
	if err != nil {
		return err
	}
	newGuard, err := a.generationGuard(next)
	if err != nil {
		return err
	}
	// Validation can take time. Do not restart an instance that stopped,
	// recovered or otherwise changed while preparing the new profile.
	observed, err := a.Service.Inspect(ctx)
	if err != nil {
		return err
	}
	if status.Active != observed.Active || status.State != observed.State || status.PID != observed.PID {
		return diagnostic.New("diagnostic.profile.serviceChanged", "service changed while validating the profile; retry the operation")
	}
	if err := a.Service.Verify(ctx, bin, config, data); err != nil {
		return err
	}
	p := pending{Old: old, New: next, Active: status.Active, OldGuard: oldGuard, NewGuard: newGuard}
	raw, _ := json.Marshal(p)
	journal := filepath.Join(a.Home, "apply-pending.json")
	if err := store.Replace(journal, raw, nil, 0600); err != nil {
		return err
	}
	rollback := func(cause error) error {
		c, cancel := cleanupContext()
		defer cancel()
		err := a.recover(c)
		if err != nil {
			committed = true
		}
		return errors.Join(cause, err)
	}
	if err := store.Switch(filepath.Join(base, "current"), next, old); err != nil {
		return rollback(err)
	}
	if status.Active {
		if err := a.restartReady(ctx, m); err != nil {
			return rollback(err)
		}
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	if err := store.Remove(journal, raw); err != nil {
		return rollback(err)
	}
	committed = true
	if err := a.markProfileValidated(name); err != nil {
		return diagnostic.New("diagnostic.profile.metadataSave", "configuration applied, but profile validation metadata could not be saved: %w", err)
	}
	return nil
}
