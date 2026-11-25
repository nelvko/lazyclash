package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"
)

// The caller holds the instance lock. m contains the updated recipe, and SHA
// still identifies the previously published effective configuration.
func (a *App) publishProfileRefresh(ctx context.Context, old string, m manifest, cfg []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if digest(cfg) == m.SHA {
		// A new source revision can build to exactly the same YAML. Pin the
		// revision on disk without contacting the service or its controller.
		base := filepath.Join(a.Home, "installation")
		next, err := a.writeGeneration(base, m, cfg)
		if err != nil {
			return err
		}
		return store.Switch(filepath.Join(base, "current"), next, old)
	}
	status, err := a.profileServiceStatus(ctx)
	if err != nil {
		return err
	}
	return a.publishProfileGeneration(ctx, old, m, cfg, status, true)
}

func (a *App) publishProfileGeneration(ctx context.Context, old string, m manifest, cfg []byte, status service.Status, hot bool) error {
	base := filepath.Join(a.Home, "installation")
	current := filepath.Join(base, "current")
	next, err := a.writeGeneration(base, m, cfg)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(filepath.Join(base, next))
		}
	}()
	p := pending{Old: old, New: next, Active: status.Active, HotReload: hot}
	if p.OldGuard, err = a.generationGuard(old); err != nil {
		return err
	}
	if p.NewGuard, err = a.generationGuard(next); err != nil {
		return err
	}
	if err := a.checkProfileProcess(ctx, status); err != nil {
		return err
	}
	raw, _ := json.Marshal(p)
	journal := filepath.Join(a.Home, "apply-pending.json")
	if err := store.Replace(journal, raw, nil, 0600); err != nil {
		// Replace may have published the journal before a directory sync failed.
		if _, statErr := os.Lstat(journal); statErr == nil {
			keep = true
		}
		return err
	}
	keep = true
	rollback := func(cause error) error {
		// A recovery can need a full controller request followed by readiness.
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		recoveryErr := a.recover(cleanup)
		if recoveryErr == nil {
			target, err := os.Readlink(current)
			if err != nil || target != old {
				recoveryErr = diagnostic.New("diagnostic.recovery.targetChanged", "recovery target changed; refusing to modify")
			} else {
				keep = false
			}
		}
		if !hot {
			if recoveryErr != nil {
				return diagnostic.New("diagnostic.recovery.profileSwitch", "profile switch failed and recovery could not be confirmed: %w", errors.Join(cause, recoveryErr))
			}
			return cause
		}
		if recoveryErr != nil {
			return diagnostic.New("diagnostic.profile.reloadRecovery", "reload failed and recovery could not be confirmed: %w", errors.Join(cause, recoveryErr))
		}
		if observed, err := a.Service.Inspect(cleanup); err == nil {
			if !observed.Active {
				return diagnostic.New("diagnostic.profile.reloadRestoredStopped", "previous configuration restored; service is stopped: %w", cause)
			}
			if profileProcess(observed) != profileProcess(status) {
				return diagnostic.New("diagnostic.profile.reloadRestoredNewProcess", "previous configuration restored in a new service process: %w", cause)
			}
		}
		return diagnostic.New("diagnostic.profile.reloadRestored", "previous configuration restored: %w", cause)
	}
	if err := store.Switch(current, next, old); err != nil {
		return rollback(err)
	}
	if status.Active {
		if hot {
			err = a.reloadProfileReady(ctx, m, status, &p, &raw)
		} else {
			err = a.restartReady(ctx, m)
		}
	} else if hot {
		err = a.checkProfileProcess(ctx, status)
	}
	if err != nil {
		return rollback(err)
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	if err := store.Remove(journal, raw); err != nil {
		return rollback(err)
	}
	return nil
}

func (a *App) checkProfileProcess(ctx context.Context, expected service.Status) error {
	bin, config, data := a.paths()
	if err := a.Service.Verify(ctx, bin, config, data); err != nil {
		return err
	}
	observed, err := a.Service.Inspect(ctx)
	if err != nil {
		return err
	}
	if observed.Active != expected.Active || observed.State != expected.State || profileProcess(observed) != profileProcess(expected) {
		return diagnostic.New("diagnostic.profile.serviceChanged", "service changed while validating the profile; retry the operation")
	}
	return nil
}

func profileProcess(s service.Status) reloadProcess {
	return reloadProcess{PID: s.PID, Started: s.StartTimestampMonotonic}
}

// Record the PID before dispatch. A crash or lost HTTP reply otherwise leaves
// no way to know if a slow candidate parse can still apply after a rollback.
func (a *App) setReloadProcess(p *pending, raw *[]byte, process *reloadProcess) error {
	next := *p
	next.Reloading = process
	encoded, _ := json.Marshal(next)
	if err := store.Replace(filepath.Join(a.Home, "apply-pending.json"), encoded, *raw, 0600); err != nil {
		return err
	}
	*p, *raw = next, encoded
	return nil
}

func (a *App) reloadProfileReady(ctx context.Context, m manifest, expected service.Status, p *pending, raw *[]byte) error {
	if err := a.checkProfileProcess(ctx, expected); err != nil {
		return err
	}
	target := profileProcess(expected)
	if err := a.setReloadProcess(p, raw, &target); err != nil {
		return err
	}
	replied, err := a.client(m).Reload(ctx)
	if replied {
		if journalErr := a.setReloadProcess(p, raw, nil); journalErr != nil {
			return errors.Join(err, journalErr)
		}
	}
	if err != nil {
		return err
	}
	if err := a.waitReady(ctx, m); err != nil {
		return err
	}
	return a.checkProfileProcess(ctx, expected)
}

// If Mihomo lost a response, its handler can still be parsing the superseded
// configuration outside its apply mutex. A subsequent successful PUT alone
// cannot settle that race. Fence only that process using systemd try-restart,
// which is a no-op for an inactive unit. Completed requests roll back in place.
func (a *App) restoreHotProfile(ctx context.Context, m manifest, p *pending, raw *[]byte, stopRequested bool) error {
	status, err := a.profileServiceStatus(ctx)
	if err != nil || !status.Active {
		return err
	}
	if p.Reloading == nil || *p.Reloading != profileProcess(status) {
		err := a.reloadProfileReady(ctx, m, status, p, raw)
		if err == nil || p.Reloading == nil || *p.Reloading != profileProcess(status) {
			return err
		}
	}
	if stopRequested {
		return diagnostic.New("diagnostic.profile.reloadAwaitStop", "previous configuration saved; waiting for the requested service stop before confirming recovery")
	}
	if err := a.checkProfileProcess(ctx, status); err != nil {
		return err
	}
	if err := a.Service.Action(ctx, "try-restart"); err != nil {
		return err
	}
	after, err := a.profileServiceStatus(ctx)
	if err != nil {
		return err
	}
	if after.State == "failed" {
		return diagnostic.New("diagnostic.runtime.serviceFailed", "Mihomo service failed (result=%s, exit=%d); inspect status before retrying", after.Result, after.ExitCode)
	}
	if !after.Active {
		return nil
	}
	if profileProcess(after) == profileProcess(status) {
		return diagnostic.New("diagnostic.profile.reloadProcessUnconfirmed", "could not confirm the old reload process exited; recovery remains pending")
	}
	if err := a.waitReady(ctx, m); err != nil {
		return err
	}
	return a.checkProfileProcess(ctx, after)
}
