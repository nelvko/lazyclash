package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveRequiresUnchangedRegularContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata")
	if err := Replace(path, []byte("private-data"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, expected := range [][]byte{nil, []byte("older-data")} {
		if err := Remove(path, expected); err == nil {
			t.Fatal("unguarded deletion succeeded")
		}
	}
	if err := Remove(path, []byte("private-data")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unchanged file was not removed")
	}
}
