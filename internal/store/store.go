// Package store contains private, atomic filesystem operations for lazyclash.
package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"golang.org/x/sys/unix"
)

func Home(override string) (string, error) {
	return HomeForService(override, "lazyclash-mihomo")
}

// HomeForService separates root system instances from user data and inherited
// user environment. An explicit path remains available for isolated instances.
func HomeForService(override, name string) (string, error) {
	return homeForService(override, name, runtime.GOOS, os.Geteuid())
}

func homeForService(override, name, goos string, euid int) (string, error) {
	if override == "" && goos == "linux" && euid == 0 {
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
			return "", diagnostic.New("diagnostic.store.serviceName", "system data directory requires a valid service name")
		}
		override = filepath.Join("/var/lib/lazyclash", name)
	}
	if override == "" {
		override = os.Getenv("LAZYCLASH_HOME")
	}
	if override == "" {
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
			h, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(h, ".local", "share")
		}
		override = filepath.Join(base, "lazyclash")
	}
	if !filepath.IsAbs(override) {
		return "", diagnostic.New("diagnostic.store.homeAbsolute", "lazyclash home must be an absolute path")
	}
	return filepath.Clean(override), nil
}

// Dir rejects a symlink at a managed directory boundary. Ancestor directories
// can include OS aliases (macOS /var). Call it at each managed directory level.
// We do not claim protection against another process with the same UID.
func Dir(path string) error {
	if !filepath.IsAbs(path) {
		return diagnostic.New("diagnostic.store.directoryAbsolute", "private directory must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(path, 0700)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return diagnostic.New("diagnostic.store.directoryBoundary", "refusing non-directory or symlink: %s", path)
	}
	return nil
}

func Read(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, diagnostic.New("diagnostic.store.readRegular", "refusing to read a non-regular managed file")
	}
	if info.Size() > 16<<20 {
		return nil, diagnostic.New("diagnostic.store.fileSize", "managed file exceeds size limit")
	}
	return os.ReadFile(path)
}

// Replace uses expected bytes as a compare-and-swap guard. nil means the path
// must not exist; an existing user file is never silently overwritten.
func Replace(path string, data, expected []byte, mode os.FileMode) error {
	if expected == nil {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return err
			}
			return diagnostic.New("diagnostic.store.noOverwrite", "file already exists; refusing to overwrite")
		}
	} else {
		old, err := Read(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(old, expected) {
			return diagnostic.New("diagnostic.store.modifiedOverwrite", "managed file was modified; refusing to overwrite")
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".lazyclash-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if expected == nil {
		err = os.Link(f.Name(), path)
	} else {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return err
	}
	return SyncDir(filepath.Dir(path))
}

// Remove only deletes the unchanged regular file observed by the caller.
// Like Replace, this is guarded against accidental edits, not a hostile process
// running under the same UID.
func Remove(path string, expected []byte) error {
	if expected == nil {
		return diagnostic.New("diagnostic.store.expectedContents", "refusing to remove a file without expected contents")
	}
	actual, err := Read(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, expected) {
		return diagnostic.New("diagnostic.store.modifiedRemove", "managed file was modified; refusing to remove")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return SyncDir(filepath.Dir(path))
}

func SyncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// Lock files remain in place so waiters always contend on the same inode.
func Lock(ctx context.Context, root string) (func(), error) {
	if err := Dir(root); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(root, ".lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "lazyclash lock")
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, diagnostic.New("diagnostic.store.lockFile", "application lock must be a regular file")
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = f.Close() }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// ReadLock synchronizes observations with managed mutations without creating a
// directory or lock file. An absent namespace has nothing to observe yet.
func ReadLock(ctx context.Context, root string) (func(), error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return func() {}, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, diagnostic.New("diagnostic.store.realDirectory", "application data path must be a real directory")
	}
	fd, err := unix.Open(filepath.Join(root, ".lock"), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return func() {}, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "lazyclash read lock")
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, diagnostic.New("diagnostic.store.lockFile", "application lock must be a regular file")
	}
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		err := unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB)
		if err == nil {
			return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = f.Close() }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-tick.C:
		}
	}
}

func Switch(path, target, expected string) error {
	old, err := os.Readlink(path)
	if expected == "" {
		if !errors.Is(err, os.ErrNotExist) {
			return diagnostic.New("diagnostic.store.generationExists", "current generation already exists")
		}
	} else if err != nil || old != expected {
		return diagnostic.New("diagnostic.store.generationChanged", "current generation changed; refusing to replace")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".lazyclash-link-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	defer os.Remove(name)
	if err := os.Symlink(target, name); err != nil {
		return err
	}
	if expected == "" { // root is locked; avoid replacing foreign data even here.
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return diagnostic.New("diagnostic.store.generationExists", "current generation already exists")
		}
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return SyncDir(filepath.Dir(path))
}
