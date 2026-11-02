package app

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"time"

	"github.com/nelvko/lazyclash/internal/store"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

type installJournal struct {
	manifest
	Tree map[string]string `json:"tree"`
}

type pending struct {
	Old, New           string
	Active             bool
	OldGuard, NewGuard string `json:",omitempty"`
}

func cleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 20*time.Second)
}

func (a *App) recover(ctx context.Context) error {
	if err := a.recoverCore(ctx); err != nil {
		return err
	}
	// A pending apply is always rolled back after interruption. The old complete
	// generation remains available until the new service has passed readiness.
	path := filepath.Join(a.Home, "apply-pending.json")
	raw, err := store.Read(path)
	if errors.Is(err, os.ErrNotExist) {
		return a.recoverInstall(ctx)
	}
	if err != nil {
		return err
	}
	var p pending
	if json.Unmarshal(raw, &p) != nil || !generationName.MatchString(p.Old) || !generationName.MatchString(p.New) {
		return diagnostic.New("diagnostic.recovery.invalidJournal", "invalid recovery journal; manual inspection required")
	}
	if err := a.requireService(); err != nil {
		return err
	}
	if p.OldGuard != "" || p.NewGuard != "" {
		if len(p.OldGuard) != 64 || len(p.NewGuard) != 64 || p.Old == p.New {
			return diagnostic.New("diagnostic.recovery.guardedJournal", "invalid guarded recovery journal; manual inspection required")
		}
		if _, _, err := a.load(); err != nil {
			return diagnostic.New("diagnostic.recovery.installationChanged", "recovery installation changed; preserving files and journal: %w", err)
		}
		for target, expected := range map[string]string{p.Old: p.OldGuard, p.New: p.NewGuard} {
			actual, err := a.generationGuard(target)
			if err != nil || actual != expected {
				return diagnostic.New("diagnostic.recovery.generationChanged", "recovery generation changed; preserving files and journal for manual inspection")
			}
		}
		bin, config, data := a.paths()
		if err := a.Service.Verify(ctx, bin, config, data); err != nil {
			return err
		}
	}
	current := filepath.Join(a.Home, "installation", "current")
	target, err := os.Readlink(current)
	if err != nil {
		return err
	}
	if target == p.New {
		if err := store.Switch(current, p.Old, p.New); err != nil {
			return err
		}
	} else if target != p.Old {
		return diagnostic.New("diagnostic.recovery.targetChanged", "recovery target changed; refusing to modify")
	}
	m, _, err := a.load()
	if err != nil {
		return err
	}
	stopped, err := a.stopIntent()
	if err != nil {
		return err
	}
	intent, _, err := a.loadRuntimeRecord(m)
	if err != nil && stopped == nil {
		return err
	}
	if stopped == nil && p.Active && (!intent.Desired.Known || intent.Desired.Running) {
		if err := a.restartReady(ctx, m); err != nil {
			return diagnostic.New("diagnostic.recovery.serviceFailed", "configuration restored but service recovery failed: %w", err)
		}
	}
	return os.Remove(path)
}
func (a *App) recoverInstall(ctx context.Context) error {
	path := filepath.Join(a.Home, "install-pending.json")
	raw, err := store.Read(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal installJournal
	if json.Unmarshal(raw, &journal) != nil || journal.ID == "" || journal.Service != a.Name || len(journal.Tree) == 0 {
		return diagnostic.New("diagnostic.recovery.invalidInstallationJournal", "invalid installation recovery journal")
	}
	m := journal.manifest
	if err := a.requireService(); err != nil {
		return err
	}
	bin, config, data := a.paths()
	if _, err := os.Lstat(filepath.Join(a.Home, "installation")); err == nil {
		current, _, err := a.load()
		if err != nil || current.ID != m.ID {
			return diagnostic.New("diagnostic.recovery.interruptedChanged", "interrupted installation changed; refusing to remove files")
		}
		tree, err := installationTree(filepath.Join(a.Home, "installation"))
		if err != nil || !maps.Equal(tree, journal.Tree) {
			return diagnostic.New("diagnostic.recovery.interruptedFiles", "interrupted installation contains changed or additional files; preserving files for manual inspection")
		}
		if err := a.Service.RemoveOwned(ctx, bin, config, data); err != nil {
			return diagnostic.New("diagnostic.recovery.rollback", "installation rollback: %w", err)
		}
		if err := os.RemoveAll(filepath.Join(a.Home, "installation")); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Remove(path)
}
