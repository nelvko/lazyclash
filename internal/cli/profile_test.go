package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
)

func TestProfileRememberSourceAndExplicitRefreshPreserveStoppedCore(t *testing.T) {
	a, service := installBackend(t)
	source := filepath.Join(t.TempDir(), "subscription.yaml")
	initial := []byte("mode: rule\nproxies: []\nproxy-groups: []\nrules: ['MATCH,DIRECT']\n")
	if err := os.WriteFile(source, initial, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) error {
		cmd := newProfileCommand(func() *app.App { return a })
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		return cmd.Execute()
	}
	if err := run("add", "saved", source, "--remember-source"); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(a.Home, "profiles", "saved.source.json")
	if info, err := os.Stat(metadataPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("remembered source was not private", err)
	}
	updated := []byte("mode: direct\nproxies: []\nproxy-groups: []\nrules: ['MATCH,DIRECT']\n")
	if err := os.WriteFile(source, updated, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run("refresh", "saved"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(a.Home, "profiles", "saved.yaml"))
	if err != nil || !bytes.Equal(raw, updated) {
		t.Fatal("explicit refresh did not update the raw source profile", err)
	}
	if service.installs != 0 || service.actions != 0 {
		t.Fatal("subscription command installed or started a core")
	}
}
