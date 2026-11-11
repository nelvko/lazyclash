//go:build linux

package shellintegration

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type identityPTYReport struct {
	Identity    Identity
	Error       string
	AttachError string
}

// This uses a real controlling PTY and the actual shell executable. It covers
// the Linux /proc association that unit-test identities intentionally replace.
func TestLinuxIdentityPTY(t *testing.T) {
	if os.Getenv("LAZYCLASH_IDENTITY_TEST_CHILD") == "1" {
		owner, err := callerIdentity()
		report := identityPTYReport{Identity: owner}
		if err != nil {
			report.Error = err.Error()
		}
		pid, _ := strconv.Atoi(os.Getenv("LAZYCLASH_IDENTITY_TEST_PID"))
		if _, err := attachIdentity(os.Getenv("LAZYCLASH_IDENTITY_TEST_SHELL"), pid); err != nil {
			report.AttachError = err.Error()
		}
		data, _ := json.Marshal(report)
		if err := os.WriteFile(os.Getenv("LAZYCLASH_IDENTITY_TEST_REPORT"), data, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, name := range []string{"bash", "zsh", "fish"} {
		t.Run(name, func(t *testing.T) {
			shell, err := exec.LookPath(name)
			if err != nil {
				t.Skipf("%s not installed", name)
			}
			masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			master := os.NewFile(uintptr(masterFD), "identity PTY")
			defer master.Close()
			if err := unix.IoctlSetPointerInt(masterFD, unix.TIOCSPTLCK, 0); err != nil {
				t.Fatal(err)
			}
			number, err := unix.IoctlGetInt(masterFD, unix.TIOCGPTN)
			if err != nil {
				t.Fatal(err)
			}
			slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer slave.Close()
			args := []string{"--noprofile", "--norc", "-i"}
			if name == "zsh" {
				args = []string{"-f", "-i"}
			}
			if name == "fish" {
				args = []string{"--no-config", "--interactive"}
			}
			cmd := exec.Command(shell, args...)
			cmd.Stdin = slave
			cmd.Stdout = slave
			cmd.Stderr = slave
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
			root := t.TempDir()
			cmd.Env = append(os.Environ(), "HOME="+root, "XDG_CONFIG_HOME="+filepath.Join(root, "config"), "XDG_CACHE_HOME="+filepath.Join(root, "cache"), "TERM=xterm-256color", "PS1=identity> ")
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			defer func() {
				_ = cmd.Process.Kill()
				select {
				case <-done:
				case <-time.After(time.Second):
				}
			}()
			go func() { _, _ = io.Copy(io.Discard, master) }()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			for _, substitution := range []bool{false, true} {
				reportPath := filepath.Join(root, fmt.Sprintf("report-%v.json", substitution))
				quote := func(s string) string {
					if name == "fish" {
						return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(s) + "'"
					}
					return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
				}
				command := fmt.Sprintf("env LAZYCLASH_IDENTITY_TEST_CHILD=1 LAZYCLASH_IDENTITY_TEST_PID=%d LAZYCLASH_IDENTITY_TEST_SHELL=%s LAZYCLASH_IDENTITY_TEST_REPORT=%s %s -test.run '^TestLinuxIdentityPTY$'", cmd.Process.Pid, name, quote(reportPath), quote(executable))
				if substitution {
					if name == "fish" {
						command = "set -l captured (" + command + ")"
					} else {
						command = "captured=$(" + command + ")"
					}
				}
				if _, err := fmt.Fprintln(master, command); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(10 * time.Second)
				var data []byte
				for {
					data, err = os.ReadFile(reportPath)
					if err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("shell did not run identity child: %v", err)
					}
					time.Sleep(20 * time.Millisecond)
				}
				var report identityPTYReport
				if err = json.Unmarshal(data, &report); err != nil {
					t.Fatal(err)
				}
				if report.AttachError != "" {
					t.Fatalf("source substitution=%v failed ancestor association: %s", substitution, report.AttachError)
				}
				if !substitution && (report.Error != "" || report.Identity.PID != cmd.Process.Pid || report.Identity.Shell != name || report.Identity.TTY == 0 || report.Identity.Start == "" || report.Identity.BootID == "") {
					t.Fatalf("direct identity %+v", report)
				}
			}
		})
	}
}
