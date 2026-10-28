package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"time"

	"github.com/nelvko/lazyclash/internal/store"
)

type installJournal struct {
	manifest
	Tree map[string]string `json:"tree"`
}

type pending struct {
	Old, New string
	Active   bool
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
		return errors.New("invalid recovery journal; manual inspection required")
	}
	if err := a.requireService(); err != nil {
		return err
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
		return errors.New("recovery target changed; refusing to modify")
	}
	m, _, err := a.load()
	if err != nil {
		return err
	}
	if p.Active {
		if err := a.restartReady(ctx, m); err != nil {
			return fmt.Errorf("configuration restored but service recovery failed: %w", err)
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
		return errors.New("invalid installation recovery journal")
	}
	m := journal.manifest
	if err := a.requireService(); err != nil {
		return err
	}
	bin, config, data := a.paths()
	if _, err := os.Lstat(filepath.Join(a.Home, "installation")); err == nil {
		current, _, err := a.load()
		if err != nil || current.ID != m.ID {
			return errors.New("interrupted installation changed; refusing to remove files")
		}
		tree, err := installationTree(filepath.Join(a.Home, "installation"))
		if err != nil || !maps.Equal(tree, journal.Tree) {
			return errors.New("interrupted installation contains changed or additional files; preserving files for manual inspection")
		}
		if err := a.Service.RemoveOwned(ctx, bin, config, data); err != nil {
			return fmt.Errorf("installation rollback: %w", err)
		}
		if err := os.RemoveAll(filepath.Join(a.Home, "installation")); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Remove(path)
}
