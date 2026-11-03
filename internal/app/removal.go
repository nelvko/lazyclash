package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/nelvko/lazyclash/internal/store"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// RemoveCore removes an explicitly stopped, disabled installation. Profiles,
// subscription sources and user preferences live outside this tree and remain
// available for a later installation. Removal never implicitly stops a core.
func (a *App) RemoveCore(ctx context.Context) error {
	return a.locked(ctx, func() error {
		if err := a.requireService(); err != nil {
			return err
		}
		m, _, err := a.load()
		if err != nil {
			return err
		}
		_, runtimeRaw, err := a.loadRuntimeRecord(m)
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
		if status.Active || status.PID != 0 || status.State != "inactive" && status.State != "failed" {
			return diagnostic.New("diagnostic.removal.stopFirst", "stop the managed core before uninstalling; the service is running or changing state")
		}
		if status.Enabled || strings.HasPrefix(status.EnableState, "enabled") {
			return diagnostic.New("diagnostic.removal.disableFirst", "disable service autostart before uninstalling with lazyclash service disable")
		}
		base := filepath.Join(a.Home, "installation")
		tree, err := a.removalTree(m)
		if err != nil {
			return diagnostic.New("diagnostic.removal.unsafe", "installation cannot be safely removed: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.Service.RemoveOwned(ctx, bin, config, data); err != nil {
			return err
		}
		if err := a.clearStop(); err != nil {
			return diagnostic.New("diagnostic.removal.stopIntentCleanup", "service unit removed, but saved stop intent needs manual cleanup: %w", err)
		}
		// Once the owned unit is removed, finish local cleanup even if the UI
		// closes. Refuse to delete files changed outside the application lock.
		currentTree, err := installationTree(base)
		if err != nil || !maps.Equal(tree, currentTree) {
			return errors.Join(diagnostic.New("diagnostic.removal.filesChanged", "service unit removed, but installation files changed; files preserved for manual cleanup"), err)
		}
		runtimePath := filepath.Join(a.Home, "runtime-state.json")
		if runtimeRaw != nil {
			current, err := store.Read(runtimePath)
			if err != nil || !bytes.Equal(current, runtimeRaw) {
				return errors.Join(diagnostic.New("diagnostic.removal.runtimeChanged", "service unit removed, but runtime state changed; installation preserved for manual cleanup"), err)
			}
			if err := os.Remove(runtimePath); err != nil {
				return diagnostic.New("diagnostic.removal.runtimeCleanup", "service unit removed, but runtime state needs manual cleanup: %w", err)
			}
		} else if _, err := os.Lstat(runtimePath); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(diagnostic.New("diagnostic.removal.newRuntime", "service unit removed, but a new runtime state appeared; installation preserved for manual cleanup"), err)
		}
		if err := os.RemoveAll(base); err != nil {
			return diagnostic.New("diagnostic.removal.installationCleanup", "service unit removed, but installation files need manual cleanup: %w", err)
		}
		if err := store.SyncDir(a.Home); err != nil {
			return diagnostic.New("diagnostic.removal.sync", "core removed, but syncing the data directory failed: %w", err)
		}
		return nil
	})
}

func (a *App) removalTree(owner manifest) (map[string]string, error) {
	base := filepath.Join(a.Home, "installation")
	tree, err := installationTree(base)
	if err != nil {
		return nil, err
	}
	for path, kind := range tree {
		switch {
		case path == "." || path == "mihomo" || path == "current" || path == "runtime" || path == "generations" || strings.HasPrefix(path, "runtime/"):
		case a.systemService() != nil && path == "lazyclash" && owner.LifecycleSHA != "" && kind == "file:"+owner.LifecycleSHA:
		case a.systemService() != nil && path == "instance-id" && kind == "file:"+digest([]byte(owner.ID)):
		case generationName.MatchString(path) && kind == "directory":
			raw, err := store.Read(filepath.Join(base, path, "manifest.json"))
			if err != nil {
				return nil, err
			}
			var m manifest
			if json.Unmarshal(raw, &m) != nil || m.ID != owner.ID || m.Service != owner.Service || tree[path+"/config.yaml"] != "file:"+m.SHA {
				return nil, diagnostic.New("diagnostic.removal.foreignGeneration", "installation contains a changed or foreign configuration generation; files preserved")
			}
		case generationName.MatchString(filepath.Dir(path)) && (filepath.Base(path) == "manifest.json" || filepath.Base(path) == "config.yaml") && strings.HasPrefix(kind, "file:"):
		default:
			return nil, diagnostic.New("diagnostic.removal.unexpectedFiles", "installation contains unexpected files; preserve them before uninstalling")
		}
	}
	return tree, nil
}
