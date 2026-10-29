// Package service manages a single unprivileged Linux systemd user service.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const managedHeader = "# Managed by lazyclash.\n"

var validName = regexp.MustCompile(`^lazyclash-[A-Za-z0-9-]+$`)

type Status struct {
	Active bool
	State  string
	Linger bool
	PID    int
}

// Name excludes the .service suffix. UnitDir is the systemd user unit directory.
type Systemd struct {
	Name             string
	UnitDir          string
	run              func(context.Context, string, ...string) ([]byte, error)
	installedContent string
}

func New(name string) (*Systemd, error) {
	if runtime.GOOS != "linux" {
		return nil, errors.New("service management requires Linux with systemd; this platform is unsupported")
	}
	if !validName.MatchString(name) {
		return nil, errors.New("service name must match lazyclash-[A-Za-z0-9-]+")
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("find user config directory: %w", err)
		}
		config = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(config) {
		return nil, errors.New("XDG_CONFIG_HOME must be an absolute path")
	}
	return &Systemd{Name: name, UnitDir: filepath.Join(config, "systemd", "user")}, nil
}

func (s *Systemd) command(ctx context.Context, command string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.run != nil {
		return s.run(ctx, command, args...)
	}
	// Never include arbitrary subprocess output in returned errors. A service's
	// output can contain subscription credentials or controller secrets.
	out, err := exec.CommandContext(ctx, command, args...).Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%s failed: %w", command, err)
	}
	return out, nil
}

func (s *Systemd) unit() (string, error) {
	if !validName.MatchString(s.Name) || !filepath.IsAbs(s.UnitDir) {
		return "", errors.New("invalid systemd service name or unit directory")
	}
	return s.Name + ".service", nil
}

func (s *Systemd) Check(ctx context.Context) error {
	if _, err := s.unit(); err != nil {
		return err
	}
	if _, err := s.command(ctx, "systemctl", "--user", "show", "--property=SystemState", "--value"); err != nil {
		return fmt.Errorf("systemd user manager is unavailable (check your user session and XDG_RUNTIME_DIR): %w", err)
	}
	return nil
}

// Available rejects units in any user manager search directory, including
// vendor-supplied user units, and local dangling symlinks.
func (s *Systemd) Available(ctx context.Context) error {
	name, err := s.unit()
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(s.UnitDir, name)); err == nil {
		return fmt.Errorf("service %s already exists; refusing to overwrite it", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect unit path: %w", err)
	}
	out, err := s.command(ctx, "systemctl", "--user", "show", name, "--property=LoadState", "--value")
	if err != nil {
		return fmt.Errorf("check service availability: %w", err)
	}
	if strings.TrimSpace(string(out)) != "not-found" {
		return fmt.Errorf("service %s already exists or its availability is unknown", name)
	}
	return nil
}

func (s *Systemd) Inspect(ctx context.Context) (Status, error) {
	name, err := s.unit()
	if err != nil {
		return Status{}, err
	}
	out, err := s.command(ctx, "systemctl", "--user", "show", name, "--property=LoadState,ActiveState,MainPID")
	if err != nil {
		return Status{}, fmt.Errorf("inspect service: %w", err)
	}
	status := Status{State: "unknown"}
	load := ""
	for line := range strings.SplitSeq(string(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			load = value
		case "ActiveState":
			status.State = value
		case "MainPID":
			pid, err := strconv.Atoi(value)
			if err != nil || pid < 0 {
				return Status{}, errors.New("systemd returned an invalid process ID")
			}
			status.PID = pid
		}
	}
	if load == "not-found" {
		status.State = "not-found"
	}
	status.Active = status.State == "active"
	// Linger is diagnostic: lack of loginctl must not prevent service control.
	if out, err := s.command(ctx, "loginctl", "show-user", strconv.Itoa(os.Getuid()), "--property=Linger", "--value"); err == nil {
		status.Linger = strings.TrimSpace(string(out)) == "yes"
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	return status, nil
}

// quoteArg follows systemd.syntax(7), not shell quoting. Specifier expansion
// also happens inside quotes, so literal percent signs must be doubled.
func quoteArg(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\n\r") {
		return "", errors.New("service paths must be absolute and contain no NUL or newline")
	}
	path = strings.ReplaceAll(path, "\\", "\\\\")
	path = strings.ReplaceAll(path, `"`, `\"`)
	path = strings.ReplaceAll(path, "%", "%%")
	// ExecStart expands environment variables unless the executable has the
	// ':' prefix. This unit uses ':' so dollar signs remain literal.
	return `"` + path + `"`, nil
}

func unitContent(bin, config, data string) (string, error) {
	paths := []string{bin, config, data}
	for i := range paths {
		var err error
		paths[i], err = quoteArg(paths[i])
		if err != nil {
			return "", err
		}
	}
	return managedHeader + "[Unit]\nDescription=lazyclash Mihomo user service\nAfter=network.target\n\n[Service]\nType=simple\nExecStart=:" + paths[0] + " -d " + paths[2] + " -f " + paths[1] + "\nRestart=on-failure\nRestartSec=2s\nStandardOutput=null\nStandardError=null\nUMask=0077\nNoNewPrivileges=true\n\n[Install]\nWantedBy=default.target\n", nil
}

// Install publishes a complete unit atomically without replacing any file.
// It deliberately does not enable or start the service.
func (s *Systemd) Install(ctx context.Context, bin, config, data string) error {
	content, err := unitContent(bin, config, data)
	if err != nil {
		return err
	}
	if err := s.Available(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.UnitDir, 0700); err != nil {
		return fmt.Errorf("create user unit directory: %w", err)
	}
	tmp, err := os.CreateTemp(s.UnitDir, ".lazyclash-unit-*")
	if err != nil {
		return fmt.Errorf("stage service unit: %w", err)
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.WriteString(content)
	if writeErr == nil {
		writeErr = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write service unit: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(s.UnitDir, s.Name+".service")
	if err := os.Link(tmp.Name(), path); err != nil {
		return fmt.Errorf("publish service unit without overwriting: %w", err)
	}
	s.installedContent = content
	if _, err := s.command(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		removeErr := os.Remove(path)
		if removeErr == nil {
			s.installedContent = ""
		}
		// A reload can fail after the manager read the unit. Invalidate that
		// cache using a fresh context even when the caller cancelled.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, reloadErr := s.command(cleanupCtx, "systemctl", "--user", "daemon-reload")
		return errors.Join(fmt.Errorf("reload user service definitions: %w", err), removeErr, reloadErr)
	}
	return nil
}

// Verify checks that this installation owns the exact unit definition currently
// known to the manager. Drop-ins are rejected because they can change ExecStart.
func (s *Systemd) Verify(ctx context.Context, bin, config, data string) error {
	name, err := s.unit()
	if err != nil {
		return err
	}
	expected, err := unitContent(bin, config, data)
	if err != nil {
		return err
	}
	path := filepath.Join(s.UnitDir, name)
	if err := verifyFile(path, expected); err != nil {
		return err
	}
	out, err := s.command(ctx, "systemctl", "--user", "show", name, "--property=FragmentPath,DropInPaths")
	if err != nil {
		return fmt.Errorf("verify loaded service ownership: %w", err)
	}
	fragment := ""
	for line := range strings.SplitSeq(string(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "FragmentPath":
			fragment = value
		case "DropInPaths":
			if value != "" {
				return errors.New("service has additional drop-ins; refusing to control a modified unit")
			}
		}
	}
	if fragment != path {
		return errors.New("loaded service belongs to a different installation or is not loaded")
	}
	return nil
}

func verifyFile(path, expected string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect service ownership: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("refusing to modify a service unit that is not a regular file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read service ownership: %w", err)
	}
	if string(content) != expected {
		return errors.New("service unit differs from this installation; refusing to modify it")
	}
	return nil
}

// RemoveOwned removes only this installation's unit. It is safe to call after
// an unsuccessful Install which left no unit. No process is stopped.
func (s *Systemd) RemoveOwned(ctx context.Context, bin, config, data string) error {
	if err := s.Verify(ctx, bin, config, data); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	expected, err := unitContent(bin, config, data)
	if err != nil {
		return err
	}
	return s.remove(ctx, expected)
}

// Remove rolls back only a unit created by Install on this Systemd instance.
// Use RemoveOwned when the installation was created by an earlier process.
func (s *Systemd) Remove(ctx context.Context) error {
	if s.installedContent == "" {
		return errors.New("no service unit was installed by this operation")
	}
	return s.remove(ctx, s.installedContent)
}

func (s *Systemd) remove(ctx context.Context, expected string) error {
	name, err := s.unit()
	if err != nil {
		return err
	}
	path := filepath.Join(s.UnitDir, name)
	if err := verifyFile(path, expected); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	status, err := s.Inspect(ctx)
	if err != nil {
		return fmt.Errorf("check service before removal: %w", err)
	}
	switch status.State {
	case "inactive", "failed", "not-found":
	default:
		return errors.New("service is running or changing state; refusing to remove its installation")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove service unit: %w", err)
	}
	s.installedContent = ""
	if _, err := s.command(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("reload after service removal: %w", err)
	}
	return nil
}

func (s *Systemd) Action(ctx context.Context, action string) error {
	name, err := s.unit()
	if err != nil {
		return err
	}
	switch action {
	case "start", "stop", "restart":
	default:
		return errors.New("service action must be start, stop, or restart")
	}
	if _, err := s.command(ctx, "systemctl", "--user", action, name); err != nil {
		return fmt.Errorf("%s service %s: %w", action, name, err)
	}
	return nil
}
