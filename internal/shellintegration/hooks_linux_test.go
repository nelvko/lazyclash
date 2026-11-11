//go:build linux

package shellintegration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// The PTY driver requests state through the same Manager API used by the TUI.
// Integration attachment/finalization are delegated to the production CLI.
func TestMain(m *testing.M) {
	if os.Getenv("LAZYCLASH_SHELL_PTY_DRIVER") == "1" && len(os.Args) > 1 && os.Args[1] == "shell" {
		cmd := exec.Command(os.Getenv("LAZYCLASH_TEST_SHELL_BINARY"), os.Args[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestShellPTYDriver(t *testing.T) {
	action := os.Getenv("LAZYCLASH_SHELL_PTY_ACTION")
	if action == "" {
		t.Skip("PTY subprocess helper")
	}
	m, err := New()
	if err != nil {
		t.Fatal(err)
	}
	status, err := m.Current()
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(os.Getenv("LAZYCLASH_SHELL_PTY_PORT"))
	if port == 0 {
		port = 17891
	}
	endpoint := Endpoint{Namespace: "shell-pty", Host: "127.0.0.1", Port: port}
	switch action {
	case "install":
		err = m.Install(status.Shell)
	case "enable":
		err = m.Set(true, endpoint)
	case "disable":
		err = m.Set(false, endpoint)
	case "port":
		err = m.PublishEndpoint(endpoint)
	default:
		t.Fatalf("unknown PTY helper action %q", action)
	}
	if err != nil {
		t.Fatal(err)
	}
}
func TestShellIntegrationPTY(t *testing.T) {
	binary := os.Getenv("LAZYCLASH_TEST_SHELL_BINARY")
	if !filepath.IsAbs(binary) {
		t.Skip("set LAZYCLASH_TEST_SHELL_BINARY to a Linux product binary")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, "../../scripts/shell-integration-pty.py", "--binary", binary, "--helper", helper)
	command.Env = append(os.Environ(), "LAZYCLASH_SHELL_PTY_DRIVER=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("PTY acceptance failed: %v\n%s", err, output)
	} else {
		t.Logf("%s", output)
	}
}
