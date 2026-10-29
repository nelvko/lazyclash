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
func TestInitOnlyPrintsHelpers(t *testing.T) {
	home := t.TempDir()
	root := New()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--home", home, "init", "--shell", "zsh"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "lazyclash_on()") || !strings.Contains(out.String(), Unset) {
		t.Fatal(out.String())
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatal("init wrote files", err)
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

func TestInitPreservesNamespaceAndQuotesShellInput(t *testing.T) {
	home := t.TempDir() + "/a'b $(touch should-not-run)"
	root := New()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--home", home, "--service-name", "lazyclash-isolated", "init"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "--home "+shellQuote(home)) || !strings.Contains(out.String(), "--service-name 'lazyclash-isolated'") {
		t.Fatal(out.String())
	}
}

func TestTUIRequiresNormalLinuxUser(t *testing.T) {
	if err := requireUnprivilegedTUI("linux", 0); err == nil || !strings.Contains(err.Error(), "without sudo") {
		t.Fatal("root TUI did not explain the unprivileged service boundary")
	}
	if err := requireUnprivilegedTUI("linux", 1000); err != nil {
		t.Fatal(err)
	}
}
