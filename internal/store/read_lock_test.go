package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Run boundary checks with a deadline and an independent watchdog: a blocking
// FIFO open cannot observe context cancellation, which is the regression here.
func rejectUnsafeLockPromptly(t *testing.T, acquire func(context.Context, string) (func(), error), root, fifo string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		unlock, err := acquire(ctx, root)
		if unlock != nil {
			unlock()
		}
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unsafe lock boundary was accepted or waited for cancellation: %v", err)
		}
	case <-time.After(time.Second):
		// Unblock the old faulty O_RDONLY FIFO implementation before failing,
		// so the regression test does not strand its observation goroutine.
		if fifo != "" {
			if fd, err := unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0); err == nil {
				_ = unix.Close(fd)
			}
		}
		t.Fatal("unsafe lock open blocked despite context cancellation")
	}
}

func TestLockBoundariesRejectFIFOAndDirectoryWithoutWriting(t *testing.T) {
	for name, acquire := range map[string]func(context.Context, string) (func(), error){"read": ReadLock, "write": Lock} {
		for _, kind := range []string{"fifo", "directory", "symlink"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				root := t.TempDir()
				lockPath := filepath.Join(root, ".lock")
				protected := filepath.Join(root, "protected")
				if err := os.WriteFile(protected, []byte("original bytes"), 0600); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "fifo":
					if err := unix.Mkfifo(lockPath, 0600); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(lockPath, 0700); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					if err := os.Symlink(protected, lockPath); err != nil {
						t.Fatal(err)
					}
				}
				before, err := os.Lstat(lockPath)
				if err != nil {
					t.Fatal(err)
				}
				protectedBefore, err := os.Stat(protected)
				if err != nil {
					t.Fatal(err)
				}
				entriesBefore, err := os.ReadDir(root)
				if err != nil {
					t.Fatal(err)
				}
				fifo := ""
				if kind == "fifo" {
					fifo = lockPath
				}
				rejectUnsafeLockPromptly(t, acquire, root, fifo)
				after, err := os.Lstat(lockPath)
				if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
					t.Fatalf("unsafe lock path was modified: %v", err)
				}
				protectedAfter, err := os.Stat(protected)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := os.ReadFile(protected)
				if err != nil || string(raw) != "original bytes" || !os.SameFile(protectedBefore, protectedAfter) || !protectedBefore.ModTime().Equal(protectedAfter.ModTime()) {
					t.Fatalf("protected file was modified: %v", err)
				}
				entriesAfter, err := os.ReadDir(root)
				if err != nil || !reflect.DeepEqual(entriesBefore, entriesAfter) {
					t.Fatalf("lock boundary check created files: %v", err)
				}
			})
		}
	}
}

func TestLockBoundariesRejectRootSymlinkWithoutCreatingLock(t *testing.T) {
	for name, acquire := range map[string]func(context.Context, string) (func(), error){"read": ReadLock, "write": Lock} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			realRoot := filepath.Join(parent, "actual")
			if err := os.Mkdir(realRoot, 0700); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(parent, "alias")
			if err := os.Symlink(realRoot, root); err != nil {
				t.Fatal(err)
			}
			rejectUnsafeLockPromptly(t, acquire, root, "")
			entries, err := os.ReadDir(realRoot)
			if err != nil || len(entries) != 0 {
				t.Fatalf("symlink target was modified: entries=%v error=%v", entries, err)
			}
			target, err := os.Readlink(root)
			if err != nil || target != realRoot {
				t.Fatalf("root symlink was changed: target=%q error=%v", target, err)
			}
		})
	}
}

func TestReadLockCancelledAbsentNamespaceDoesNotCreateStorage(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ReadLock(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("absent namespace lost cancellation: %v", err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled read lock created storage: %v", err)
	}
}
