package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"
)

func (a *App) systemService() *service.Systemd {
	s, ok := a.Service.(*service.Systemd)
	if ok && s.Scope == service.System {
		return s
	}
	return nil
}

func (a *App) executionContext(ctx context.Context) (context.Context, error) {
	s := a.systemService()
	if s == nil {
		return ctx, nil
	}
	uid, gid, err := s.Identity()
	if err != nil {
		return nil, err
	}
	return mihomo.WithIdentity(ctx, uid, gid), nil
}

// System data is readable by the service account, but only runtime is writable.
// Root lifecycle hooks and binaries must stay outside that writable boundary.
func (a *App) prepareSystemHome(ctx context.Context) error {
	s := a.systemService()
	if s == nil {
		return nil
	}
	if err := systemHomeVisible(a.Home); err != nil {
		return err
	}
	if err := trustedRootAncestors(a.Home); err != nil {
		return err
	}
	if err := s.PrepareIdentity(ctx); err != nil {
		return err
	}
	_, gid, err := s.Identity()
	if err != nil {
		return err
	}
	if err := store.Dir(a.Home); err != nil {
		return err
	}
	if filepath.Dir(a.Home) == "/var/lib/lazyclash" {
		if err := setOwnership(filepath.Dir(a.Home), 0, 0, 0711, true); err != nil {
			return err
		}
	}
	if err := serviceCanTraverse(a.Home, gid); err != nil {
		return err
	}
	return setOwnership(a.Home, 0, gid, 0750, true)
}

func trustedRootAncestors(path string) error {
	home := filepath.Clean(path)
	for path = filepath.Clean(path); ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || stat.Uid != 0 {
			return diagnostic.New("diagnostic.install.systemHomeOwner", "system service data and its ancestors must be real directories owned by root")
		}
		// Standard sticky temporary directories cannot be used to replace an
		// existing root-owned child by another account.
		temporaryBoundary := path != home && (path == "/tmp" || path == "/var/tmp") && info.Mode()&os.ModeSticky != 0
		if info.Mode().Perm()&0022 != 0 && !temporaryBoundary {
			return diagnostic.New("diagnostic.install.systemHomeWritable", "system service data ancestors must not be writable by other accounts")
		}
		if path == filepath.Dir(path) {
			return nil
		}
	}
}

func serviceCanTraverse(home string, gid int) error {
	for path := filepath.Dir(home); ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (info.Mode().Perm()&0001 == 0 && !(int(stat.Gid) == gid && info.Mode().Perm()&0010 != 0)) {
			return diagnostic.New("diagnostic.install.systemHomeTraverse", "the dedicated service account cannot traverse a system data ancestor; use a protected path under /var/lib")
		}
		if path == filepath.Dir(path) {
			return nil
		}
	}
}

func setOwnership(path string, uid, gid int, mode os.FileMode, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return diagnostic.New("diagnostic.install.systemPath", "system service data contains an unsafe file or directory")
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func trustedRootFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return diagnostic.New("diagnostic.install.systemFileOwner", "system service managed files must be ordinary files owned by root and not writable by other accounts")
	}
	return nil
}

func (a *App) verifyLifecycle(m manifest) error {
	if a.systemService() == nil {
		return nil
	}
	id, err := a.lifecycleIdentity()
	if err != nil {
		return err
	}
	if id != m.ID {
		return errors.New("system installation identity changed")
	}
	path := filepath.Join(a.Home, "installation", "lazyclash")
	if err := trustedRootFile(path); err != nil {
		return err
	}
	hash, err := fileDigest(path)
	if err != nil {
		return err
	}
	if m.LifecycleSHA == "" || hash != m.LifecycleSHA {
		return diagnostic.New("diagnostic.install.lifecycleModified", "the protected service lifecycle executable was modified; refusing to operate")
	}
	return nil
}

func (a *App) systemDirectory(path string) error {
	s := a.systemService()
	if s == nil {
		return nil
	}
	_, gid, err := s.Identity()
	if err != nil {
		return err
	}
	return setOwnership(path, 0, gid, 0750, true)
}

func (a *App) systemFile(path string, mode os.FileMode) error {
	s := a.systemService()
	if s == nil {
		return nil
	}
	_, gid, err := s.Identity()
	if err != nil {
		return err
	}
	return setOwnership(path, 0, gid, mode, false)
}

func (a *App) systemRuntime(path string) error {
	s := a.systemService()
	if s == nil {
		return nil
	}
	uid, gid, err := s.Identity()
	if err != nil {
		return err
	}
	return setOwnership(path, uid, gid, 0700, true)
}

func (a *App) binaryVersion(ctx context.Context, bin string) (string, error) {
	if a.systemService() == nil {
		return a.Version(ctx, bin)
	}
	if err := trustedRootAncestors(a.Home); err != nil {
		return "", err
	}
	if err := mihomo.SafeExecutable(bin); err != nil {
		return "", err
	}
	// A local file can be below /root or a private user home. Probe a protected
	// copy so the unprivileged account never needs access to those ancestors.
	stage, err := os.MkdirTemp(a.Home, ".version-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	copy := filepath.Join(stage, "mihomo")
	if err := copyBinary(ctx, bin, copy); err != nil {
		return "", err
	}
	if err := a.systemDirectory(stage); err != nil {
		return "", err
	}
	if err := a.systemFile(copy, 0750); err != nil {
		return "", err
	}
	ctx, err = a.executionContext(ctx)
	if err != nil {
		return "", err
	}
	return a.Version(ctx, copy)
}

func (a *App) validateBinaryConfig(ctx context.Context, bin, config, data string) error {
	ctx, err := a.executionContext(ctx)
	if err != nil {
		return err
	}
	return a.Validate(ctx, bin, config, data)
}

// The stable identity survives interrupted generation or core publication, so
// ExecStopPost can clean up even when the next-start configuration is damaged.
func (a *App) lifecycleIdentity() (string, error) {
	path := filepath.Join(a.Home, "installation", "instance-id")
	if err := trustedRootAncestors(filepath.Dir(path)); err != nil {
		return "", err
	}
	if err := trustedRootFile(path); err != nil {
		return "", err
	}
	raw, err := store.Read(path)
	if err != nil {
		return "", err
	}
	if len(raw) != 24 {
		return "", errors.New("invalid stable installation identity")
	}
	for _, b := range raw {
		if !(b >= 'a' && b <= 'f' || b >= '0' && b <= '9') {
			return "", errors.New("invalid stable installation identity")
		}
	}
	return string(raw), nil
}

func systemHomeVisible(path string) error {
	for _, hidden := range []string{"/tmp", "/var/tmp", "/home", "/root", "/run/user"} {
		if path == hidden || strings.HasPrefix(path, hidden+"/") {
			return fmt.Errorf("system service data cannot be under %s because the service filesystem sandbox hides it; use /var/lib/lazyclash", hidden)
		}
	}
	return nil
}
