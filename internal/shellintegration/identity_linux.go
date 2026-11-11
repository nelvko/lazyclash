//go:build linux

package shellintegration

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

func identity(pid int) (Identity, int, error) {
	var result Identity
	if pid <= 1 {
		return result, 0, fmt.Errorf("cannot identify an interactive shell")
	}
	base := "/proc/" + strconv.Itoa(pid)
	info, err := os.Stat(base)
	if err != nil {
		return result, 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return result, 0, fmt.Errorf("shell belongs to another user")
	}
	raw, err := os.ReadFile(base + "/stat")
	if err != nil {
		return result, 0, err
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return result, 0, fmt.Errorf("invalid shell process identity")
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 20 {
		return result, 0, fmt.Errorf("incomplete shell process identity")
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return result, 0, err
	}
	tty, err := strconv.ParseInt(fields[4], 10, 64)
	if err != nil || tty == 0 {
		return result, parent, fmt.Errorf("shell has no controlling terminal")
	}
	executable, err := os.Readlink(base + "/exe")
	if err != nil {
		return result, parent, err
	}
	shell := filepath.Base(strings.TrimSuffix(executable, " (deleted)"))
	if shell != "bash" && shell != "zsh" && shell != "fish" {
		return result, parent, fmt.Errorf("unsupported calling shell: %s", shell)
	}
	command, err := os.ReadFile(base + "/cmdline")
	if err != nil {
		return result, parent, err
	}
	if !interactiveInvocation(strings.Split(strings.TrimSuffix(string(command), "\x00"), "\x00")) {
		return result, parent, fmt.Errorf("calling shell is not an interactive session")
	}
	input, err := unix.Open(base+"/fd/0", unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return result, parent, fmt.Errorf("cannot inspect calling shell terminal: %w", err)
	}
	isTerminal := term.IsTerminal(uintptr(input))
	_ = unix.Close(input)
	if !isTerminal {
		return result, parent, fmt.Errorf("calling shell standard input is not a terminal")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return result, parent, err
	}
	namespace, err := os.Readlink(base + "/ns/pid")
	if err != nil {
		return result, parent, err
	}
	result = Identity{UID: os.Getuid(), PID: pid, Start: fields[19], BootID: strings.TrimSpace(string(boot)), TTY: uint64(tty), Shell: shell, PIDNamespace: namespace}
	return result, parent, nil
}

func interactiveInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	// Without -i these shells become noninteractive when given a command or a
	// script. A normal login/non-login invocation with terminal stdin is allowed.
	interactive := false
	forceNoninteractive := false
	skip := false
	for index, arg := range args[1:] {
		if skip {
			skip = false
			continue
		}
		if arg == "--interactive" {
			interactive = true
			continue
		}
		if arg == "--rcfile" || arg == "--init-file" || arg == "--init-command" || arg == "-C" || arg == "-o" || arg == "-O" {
			skip = true
			continue
		}
		if arg == "--command" || strings.HasPrefix(arg, "--command=") {
			return interactive
		}
		if strings.HasPrefix(arg, "-") {
			if !strings.HasPrefix(arg, "--") {
				if strings.Contains(strings.TrimPrefix(arg, "-"), "i") {
					interactive = true
				}
				if strings.Contains(strings.TrimPrefix(arg, "-"), "c") {
					return interactive
				}
			}
			if arg == "--" {
				return interactive || index == len(args)-2 && !forceNoninteractive
			}
			continue
		}
		if strings.HasPrefix(arg, "+") {
			if strings.Contains(strings.TrimPrefix(arg, "+"), "i") {
				interactive = false
				forceNoninteractive = true
			}
			continue
		}
		return interactive
	}
	return interactive || !forceNoninteractive
}

func callerIdentity() (Identity, error) { value, _, err := identity(os.Getppid()); return value, err }

func attachIdentity(shell string, pid int) (Identity, error) {
	wanted, _, err := identity(pid)
	if err != nil {
		return Identity{}, err
	}
	if wanted.Shell != shell {
		return Identity{}, fmt.Errorf("requested shell does not match running shell")
	}
	// A source command may use a command-substitution child. Only its actual
	// ancestor, on the same controlling terminal, may claim the pending request.
	ancestor := os.Getppid()
	for depth := 0; depth < 8 && ancestor > 1; depth++ {
		if ancestor == pid {
			return wanted, nil
		}
		raw, e := os.ReadFile("/proc/" + strconv.Itoa(ancestor) + "/stat")
		if e != nil {
			break
		}
		end := strings.LastIndexByte(string(raw), ')')
		if end < 0 {
			break
		}
		fields := strings.Fields(string(raw[end+1:]))
		if len(fields) < 5 {
			break
		}
		next, e := strconv.Atoi(fields[1])
		if e != nil {
			break
		}
		ancestor = next
	}
	return Identity{}, fmt.Errorf("shell is not an ancestor of this integration process")
}

func ownerAlive(owner Identity) bool {
	current, _, err := identity(owner.PID)
	return err == nil && current == owner
}

func owned(info os.FileInfo) bool {
	value, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(value.Uid) == os.Getuid()
}
