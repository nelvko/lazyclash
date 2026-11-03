package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

func copyBinary(ctx context.Context, src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return diagnostic.New("diagnostic.install.localRead", "cannot read local Mihomo binary")
	}
	if !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return diagnostic.New("diagnostic.install.localRegular", "local binary must be a regular file at most 256 MiB")
	}
	in, err := os.Open(src)
	if err != nil {
		return diagnostic.New("diagnostic.install.localOpen", "cannot open local Mihomo binary")
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	defer out.Close()
	buf := make([]byte, 128<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := in.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return diagnostic.New("diagnostic.install.localCopy", "cannot copy local Mihomo binary")
		}
	}
	return out.Sync()
}

// Install composes a complete first installation under the application lock.
// Staging files and registering the service are separate operations, committed
// together only after both have succeeded.
func (a *App) Install(ctx context.Context, local string, progress func(string)) error {
	return a.locked(ctx, func() error { return a.installFreshLocked(ctx, local, progress) })
}

func (a *App) installFreshLocked(ctx context.Context, local string, progress func(string)) (result error) {
	if err := a.requireService(); err != nil {
		return err
	}
	if err := a.Service.Check(ctx); err != nil {
		return err
	}
	if err := a.Service.Available(ctx); err != nil {
		return err
	}
	final := filepath.Join(a.Home, "installation")
	if _, err := os.Lstat(final); !errors.Is(err, os.ErrNotExist) {
		return diagnostic.New("diagnostic.install.exists", "installation path already exists; refusing to overwrite")
	}
	m := manifest{ID: randomID(), Service: a.Name, Controller: "127.0.0.1:9097", Secret: randomID() + randomID(), Port: 7897, Applied: "default"}
	// Installation writes and validates files but does not open runtime ports.
	// Occupied defaults can be changed in Settings before the first start.
	stage, err := a.stageFreshInstallation(ctx, m, local, progress)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage.path)
	if err := a.publishFreshInstallationLocked(ctx, stage); err != nil {
		return err
	}
	installed := false
	defer func() {
		if !installed {
			c, cancel := cleanupContext()
			defer cancel()
			if err := a.recoverInstall(c); err != nil {
				result = errors.Join(result, diagnostic.New("diagnostic.install.recoveryPending", "installation recovery remains pending: %w", err))
			}
		}
	}()
	if err := store.SyncDir(a.Home); err != nil {
		return err
	}
	if progress != nil {
		progress("Installing systemd user service…")
	}
	if err := a.registerService(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(a.Home, "install-pending.json")); err != nil {
		return err
	}
	installed = true
	if progress != nil {
		progress("Mihomo installed. Start the service to view PROXY nodes.")
	}
	return nil
}

type stagedInstallation struct {
	path     string
	manifest manifest
	tree     map[string]string
}

// stageFreshInstallation prepares and validates files without registering or
// controlling a service. The caller removes the returned private staging tree.
func (a *App) stageFreshInstallation(ctx context.Context, m manifest, local string, progress func(string)) (stagedInstallation, error) {
	if err := a.prepareSystemHome(ctx); err != nil {
		return stagedInstallation{}, err
	}
	stage, err := os.MkdirTemp(a.Home, ".install-*")
	if err != nil {
		return stagedInstallation{}, err
	}
	prepared := false
	defer func() {
		if !prepared {
			_ = os.RemoveAll(stage)
		}
	}()
	if err := a.systemDirectory(stage); err != nil {
		return stagedInstallation{}, err
	}
	bin := filepath.Join(stage, "mihomo")
	if local == "" {
		err = a.Download(ctx, bin, progress)
	} else {
		if progress != nil {
			progress("Copying and validating local Mihomo binary…")
		}
		err = copyBinary(ctx, local, bin)
	}
	if err != nil {
		return stagedInstallation{}, err
	}
	m.Version, err = a.binaryVersion(ctx, bin)
	if err != nil {
		return stagedInstallation{}, err
	}
	if err := a.systemFile(bin, 0750); err != nil {
		return stagedInstallation{}, err
	}
	if a.systemService() != nil {
		if err := store.Replace(filepath.Join(stage, "instance-id"), []byte(m.ID), nil, 0600); err != nil {
			return stagedInstallation{}, err
		}
		executable, err := os.Executable()
		if err != nil {
			return stagedInstallation{}, err
		}
		if runtime.GOOS == "linux" {
			// Copy the running image even if its original, user-owned pathname
			// has been replaced since sudo started this process.
			executable = "/proc/self/exe"
		}
		lifecycle := filepath.Join(stage, "lazyclash")
		if err := copyBinary(ctx, executable, lifecycle); err != nil {
			return stagedInstallation{}, err
		}
		m.LifecycleSHA, err = fileDigest(lifecycle)
		if err != nil {
			return stagedInstallation{}, err
		}
	}
	m.BinarySHA, err = fileDigest(bin)
	if err != nil {
		return stagedInstallation{}, err
	}
	m.Build = &buildRecord{Profile: "default"}
	cfg, err := mihomo.Default(m.Controller, m.Secret, m.Port)
	if err != nil {
		return stagedInstallation{}, err
	}
	target, err := a.writeGeneration(stage, m, cfg)
	if err != nil {
		return stagedInstallation{}, err
	}
	if err := store.Dir(filepath.Join(stage, "runtime")); err != nil {
		return stagedInstallation{}, err
	}
	if err := a.systemRuntime(filepath.Join(stage, "runtime")); err != nil {
		return stagedInstallation{}, err
	}
	if progress != nil {
		progress("Validating default configuration…")
	}
	if err := a.validateBinaryConfig(ctx, bin, filepath.Join(stage, target, "config.yaml"), filepath.Join(stage, "runtime")); err != nil {
		return stagedInstallation{}, err
	}
	if err := store.Switch(filepath.Join(stage, "current"), target, ""); err != nil {
		return stagedInstallation{}, err
	}
	if err := ctx.Err(); err != nil {
		return stagedInstallation{}, err
	}
	tree, err := installationTree(stage)
	if err != nil {
		return stagedInstallation{}, err
	}
	prepared = true
	return stagedInstallation{path: stage, manifest: m, tree: tree}, nil
}

// publishFreshInstallationLocked records recovery information before moving
// the complete tree into place. After success, the caller must recover failures
// until service registration succeeds and the journal is removed.
func (a *App) publishFreshInstallationLocked(ctx context.Context, stage stagedInstallation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, _ := json.Marshal(installJournal{manifest: stage.manifest, Tree: stage.tree})
	journal := filepath.Join(a.Home, "install-pending.json")
	if err := store.Replace(journal, raw, nil, 0600); err != nil {
		return err
	}
	if err := os.Rename(stage.path, filepath.Join(a.Home, "installation")); err != nil {
		return errors.Join(err, os.Remove(journal))
	}
	return nil
}
