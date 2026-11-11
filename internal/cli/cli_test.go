package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnsetDoesNotReadDamagedInstallation(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "installation"), 0700); err != nil {
		t.Fatal(err)
	}
	root := New()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--home", home, "env", "--unset"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != Unset {
		t.Fatal(out.String())
	}
}
func TestUnsetIgnoresInvalidNamespace(t *testing.T) {
	t.Setenv("LAZYCLASH_HOME", "relative-invalid-home")
	t.Setenv("LAZYCLASH_SERVICE", "invalid/service")
	root := New()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"env", "--unset"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != Unset {
		t.Fatal(out.String())
	}
}

func TestLegacyInitRemoved(t *testing.T) {
	root := New()
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err == nil {
		t.Fatal("legacy init command still accepted")
	}
	for _, cmd := range root.Commands() {
		if cmd.Name() == "init" {
			t.Fatal("legacy init still registered")
		}
	}
}
