package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestUnsetDoesNotReadDamagedInstallation(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "installation"), 0700); err != nil {
		t.Fatal(err)
	}
	root := New()
	var out bytes.Buffer
	root.SetOut(&out)
	t.Setenv("LAZYCLASH_HOME", home)
	root.SetArgs([]string{"shell", "env", "--unset"})
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
	root.SetArgs([]string{"shell", "env", "--unset"})
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

func TestPublicCommandTreeAndUsageExitCode(t *testing.T) {
	root := New()
	public := func(command *cobra.Command) []string {
		var names []string
		for _, child := range command.Commands() {
			if !child.Hidden {
				names = append(names, child.Name())
			}
		}
		return names
	}
	if got := strings.Join(public(root), " "); got != "completion doctor help install restart shell start status stop uninstall upgrade" {
		t.Fatal("unexpected release commands:", got)
	}
	shell, _, _ := root.Find([]string{"shell"})
	if got := strings.Join(public(shell), " "); got != "env install status uninstall" {
		t.Fatal("unexpected shell commands:", got)
	}
	unit, _, _ := root.Find([]string{"__service"})
	for _, flag := range []string{"home", "service-name"} {
		if unit.Flags().Lookup(flag) == nil || !unit.Hidden {
			t.Fatal("missing private systemd flag", flag)
		}
	}
	for _, args := range [][]string{
		{}, {"service", "start"}, {"env", "--unset"}, {"install", "check"}, {"install", "upgrade"},
		{"shell", "instal"}, {"completion", "unknown"}, {"help", "profile"},
		{"install", "--remember-download-settings"}, {"--home", "/example", "status"},
		{"start", "--service-name", "lazyclash-test"}, {"upgrade"}, {"upgrade", "--check", "--yes"},
	} {
		cmd := New(args...)
		cmd.SetArgs(args)
		cmd.SetIn(strings.NewReader("yes\n"))
		var out bytes.Buffer
		cmd.SetOut(&out)
		if code := ExitCode(cmd.Execute()); code != 2 || out.Len() != 0 {
			t.Errorf("%q: code=%d stdout=%q", args, code, out.String())
		}
	}
	for _, args := range [][]string{{"shell"}, {"completion"}, {"help"}, {"--version"}} {
		cmd := New(args...)
		var out bytes.Buffer
		cmd.SetOut(&out)
		if err := cmd.Execute(); err != nil || out.Len() == 0 {
			t.Errorf("%q: output=%q err=%v", args, out.String(), err)
		}
	}
}
