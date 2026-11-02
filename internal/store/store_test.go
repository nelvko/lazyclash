package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGuardAndSymlink(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "file")
	if err := Replace(p, []byte("old"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Replace(p, []byte("new"), nil, 0600); err == nil {
		t.Fatal("overwrite")
	}
	if err := Replace(p, []byte("new"), []byte("wrong"), 0600); err == nil {
		t.Fatal("modified file")
	}
	if err := Replace(p, []byte("new"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	os.Symlink(p, link)
	if _, err := Read(link); err == nil {
		t.Fatal("read symlink")
	}
	if err := Dir(link); err == nil {
		t.Fatal("directory symlink")
	}
}
func TestLockCancellationAndReuse(t *testing.T) {
	root := t.TempDir()
	unlock, err := Lock(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, err := Lock(ctx, root); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%v", err)
	}
	unlock()
	again, err := Lock(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	again()
	if _, err := os.Stat(filepath.Join(root, ".lock")); err != nil {
		t.Fatal(err)
	}
}

func TestReadLockDoesNotCreateStorageAndCoordinatesWithWriters(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	readUnlock, err := ReadLock(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	readUnlock()
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read lock created storage: %v", err)
	}
	writeUnlock, err := Lock(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancel()
	if _, err := ReadLock(ctx, root); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read lock bypassed writer: %v", err)
	}
	writeUnlock()
	readUnlock, err = ReadLock(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReadLock(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	second()
	ctx, cancelWriter := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancelWriter()
	if _, err := Lock(ctx, root); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("writer bypassed read lock: %v", err)
	}
	readUnlock()
	writeUnlock, err = Lock(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	writeUnlock()
}
