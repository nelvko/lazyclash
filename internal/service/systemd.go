// Package service manages a single unprivileged Linux systemd user service.
package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/store"
)

const managedHeader = "# Managed by lazyclash.\n"

var validName = regexp.MustCompile(`^lazyclash-[A-Za-z0-9-]+$`)

type Status struct {
	Active      bool
	State       string
	SubState    string
	Result      string
	ExitCode    int
	Restarts    uint64
	Enabled     bool
	EnableState string
	Linger      bool
	LingerKnown bool
	PID         int
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
		return nil, diagnostic.New("diagnostic.service.platform", "service management requires Linux with systemd; this platform is unsupported")
	}
	if !validName.MatchString(name) {
		return nil, diagnostic.New("diagnostic.service.name", "service name must match lazyclash-[A-Za-z0-9-]+")
	}
	if os.Geteuid() == 0 {
		return nil, diagnostic.New("diagnostic.service.normalUser", "lazyclash manages an unprivileged user service; run it as a normal user, without sudo")
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, diagnostic.New("diagnostic.service.userConfigDirectory", "find user config directory: %w", err)
		}
		config = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(config) {
		return nil, diagnostic.New("diagnostic.service.configPath", "XDG_CONFIG_HOME must be an absolute path")
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
		return nil, diagnostic.New("diagnostic.service.commandFailed", "%s failed: %w", command, err)
	}
	return out, nil
}

func (s *Systemd) unit() (string, error) {
	if !validName.MatchString(s.Name) || !filepath.IsAbs(s.UnitDir) {
		return "", diagnostic.New("diagnostic.service.invalidUnitDirectory", "invalid systemd service name or unit directory")
	}
	return s.Name + ".service", nil
}

func (s *Systemd) Check(ctx context.Context) error {
	if _, err := s.unit(); err != nil {
		return err
	}
	if _, err := s.command(ctx, "systemctl", "--user", "show", "--property=SystemState", "--value"); err != nil {
		return diagnostic.New("diagnostic.service.manager", "systemd user manager is unavailable (check your user session and XDG_RUNTIME_DIR): %w", err)
	}
	return nil
}

// CheckPersistent refuses to promise logout independence until logind confirms
// linger. Enabling linger is an explicit administrator/user action, never a
// hidden side effect of opening lazyclash or starting a proxy.
func (s *Systemd) CheckPersistent(ctx context.Context) error {
	if err := s.Check(ctx); err != nil {
		return err
	}
	known, enabled := s.linger(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if !known || !enabled {
		return diagnostic.New("diagnostic.service.linger", "persistent user service requires linger; run loginctl enable-linger %d (administrator authorization may be required), then retry", os.Getuid())
	}
	return nil
}

func (s *Systemd) linger(ctx context.Context) (known, enabled bool) {
	out, err := s.command(ctx, "loginctl", "show-user", strconv.Itoa(os.Getuid()), "--property=Linger", "--value")
	if err != nil {
		return false, false
	}
	switch strings.TrimSpace(string(out)) {
	case "yes":
		return true, true
	case "no":
		return true, false
	default:
		return false, false
	}
}

// Available rejects units in any user manager search directory, including
// vendor-supplied user units, and local dangling symlinks.
func (s *Systemd) Available(ctx context.Context) error {
	name, err := s.unit()
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(s.UnitDir, name)); err == nil {
		return diagnostic.New("diagnostic.service.exists", "service %s already exists; refusing to overwrite it", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return diagnostic.New("diagnostic.service.inspectUnit", "inspect unit path: %w", err)
	}
	out, err := s.command(ctx, "systemctl", "--user", "show", name, "--property=LoadState", "--value")
	if err != nil {
		return diagnostic.New("diagnostic.service.checkAvailability", "check service availability: %w", err)
	}
	if strings.TrimSpace(string(out)) != "not-found" {
		return diagnostic.New("diagnostic.service.unknownAvailability", "service %s already exists or its availability is unknown", name)
	}
	return nil
}

func (s *Systemd) Inspect(ctx context.Context) (Status, error) {
	status, err := s.inspect(ctx)
	if err != nil {
		return status, err
	}
	// Linger is diagnostic here: lack of loginctl must not prevent a stop.
	status.LingerKnown, status.Linger = s.linger(ctx)
	return status, ctx.Err()
}

func (s *Systemd) inspect(ctx context.Context) (Status, error) {
	name, err := s.unit()
	if err != nil {
		return Status{}, err
	}
	out, err := s.command(ctx, "systemctl", "--user", "show", name, "--property=LoadState,ActiveState,SubState,MainPID,Result,ExecMainStatus,NRestarts,UnitFileState")
	if err != nil {
		return Status{}, diagnostic.New("diagnostic.service.inspect", "inspect service: %w", err)
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
		case "SubState":
			status.SubState = value
		case "Result":
			status.Result = value
		case "ExecMainStatus":
			exit, err := strconv.Atoi(value)
			if err != nil || exit < 0 {
				return Status{}, diagnostic.New("diagnostic.service.invalidExit", "systemd returned an invalid exit status")
			}
			status.ExitCode = exit
		case "NRestarts":
			restarts, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return Status{}, diagnostic.New("diagnostic.service.invalidRestarts", "systemd returned an invalid restart count")
			}
			status.Restarts = restarts
		case "UnitFileState":
			status.EnableState = value
			status.Enabled = value == "enabled"
		case "MainPID":
			pid, err := strconv.Atoi(value)
			if err != nil || pid < 0 {
				return Status{}, diagnostic.New("diagnostic.service.invalidPID", "systemd returned an invalid process ID")
			}
			status.PID = pid
		}
	}
	if load == "not-found" {
		status.State = "not-found"
	}
	status.Active = status.State == "active"
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	return status, nil
}

// quoteArg follows systemd.syntax(7), not shell quoting. Specifier expansion
// also happens inside quotes, so literal percent signs must be doubled.
func quoteArg(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\n\r") {
		return "", diagnostic.New("diagnostic.service.absolutePaths", "service paths must be absolute and contain no NUL or newline")
	}
	path = strings.ReplaceAll(path, "\\", "\\\\")
	path = strings.ReplaceAll(path, `"`, `\"`)
	path = strings.ReplaceAll(path, "%", "%%")
	// ExecStart expands environment variables unless the executable has the
	// ':' prefix. This unit uses ':' so dollar signs remain literal.
	return `"` + path + `"`, nil
}

func legacyUnitContent(bin, config, data string) (string, error) {
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

// unitContent adds explicit recovery bounds. Raw core output remains discarded
// because it may contain provider credentials; status retains exit diagnostics.
func unitContent(bin, config, data string) (string, error) {
	content, err := legacyUnitContent(bin, config, data)
	if err != nil {
		return "", err
	}
	content = strings.Replace(content, "After=network.target\n", "After=network.target\nStartLimitIntervalSec=60s\nStartLimitBurst=5\n", 1)
	content = strings.Replace(content, "RestartSec=2s\nStandardOutput=null\nStandardError=null\n", "RestartSec=2s\nTimeoutStopSec=15s\nKillMode=control-group\nStandardOutput=null\nStandardError=null\n", 1)
	return content, nil
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
		return diagnostic.New("diagnostic.service.createDirectory", "create user unit directory: %w", err)
	}
	tmp, err := os.CreateTemp(s.UnitDir, ".lazyclash-unit-*")
	if err != nil {
		return diagnostic.New("diagnostic.service.stageUnit", "stage service unit: %w", err)
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.WriteString(content)
	if writeErr == nil {
		writeErr = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return diagnostic.New("diagnostic.service.writeUnit", "write service unit: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(s.UnitDir, s.Name+".service")
	if err := os.Link(tmp.Name(), path); err != nil {
		return diagnostic.New("diagnostic.service.publishUnit", "publish service unit without overwriting: %w", err)
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
		return errors.Join(diagnostic.New("diagnostic.service.reloadDefinitions", "reload user service definitions: %w", err), removeErr, reloadErr)
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
		legacy, legacyErr := legacyUnitContent(bin, config, data)
		if legacyErr != nil || verifyFile(path, legacy) != nil {
			return err
		}
	}
	out, err := s.command(ctx, "systemctl", "--user", "show", name, "--property=FragmentPath,DropInPaths,NeedDaemonReload")
	if err != nil {
		return diagnostic.New("diagnostic.service.verifyLoaded", "verify loaded service ownership: %w", err)
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
		case "NeedDaemonReload":
			if value == "yes" {
				return diagnostic.New("diagnostic.service.changedDefinition", "service definition changed without daemon-reload; refusing to control an unverified loaded unit")
			}
		case "DropInPaths":
			if value != "" {
				return diagnostic.New("diagnostic.service.dropins", "service has additional drop-ins; refusing to control a modified unit")
			}
		}
	}
	if fragment != path {
		return diagnostic.New("diagnostic.service.foreignLoaded", "loaded service belongs to a different installation or is not loaded")
	}
	return nil
}

func verifyFile(path, expected string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return diagnostic.New("diagnostic.service.inspectOwnership", "inspect service ownership: %w", err)
	}
	if !info.Mode().IsRegular() {
		return diagnostic.New("diagnostic.service.regularUnit", "refusing to modify a service unit that is not a regular file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return diagnostic.New("diagnostic.service.readOwnership", "read service ownership: %w", err)
	}
	if string(content) != expected {
		return diagnostic.New("diagnostic.service.modifiedUnit", "service unit differs from this installation; refusing to modify it")
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
	path := filepath.Join(s.UnitDir, s.Name+".service")
	if verifyFile(path, expected) != nil {
		expected, err = legacyUnitContent(bin, config, data)
		if err != nil {
			return err
		}
	}
	return s.remove(ctx, expected)
}

// Remove rolls back only a unit created by Install on this Systemd instance.
// Use RemoveOwned when the installation was created by an earlier process.
func (s *Systemd) Remove(ctx context.Context) error {
	if s.installedContent == "" {
		return diagnostic.New("diagnostic.service.noInstalledUnit", "no service unit was installed by this operation")
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
		return diagnostic.New("diagnostic.service.checkRemoval", "check service before removal: %w", err)
	}
	if status.PID != 0 {
		return diagnostic.New("diagnostic.service.ownsProcess", "service still owns a process; refusing to remove its installation")
	}
	switch status.State {
	case "inactive", "failed", "not-found":
	default:
		return diagnostic.New("diagnostic.service.activeRemoval", "service is running or changing state; refusing to remove its installation")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Remove activation links before deleting the owned definition. Leaving a
	// dangling wants/ link could silently enable a later installation of the
	// same name. disable never stops or starts the already verified service.
	if _, err := s.command(ctx, "systemctl", "--user", "disable", name); err != nil {
		return diagnostic.New("diagnostic.service.disableRemoval", "disable activation before removing service: %w", err)
	}
	if err := verifyFile(path, expected); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return diagnostic.New("diagnostic.service.removeUnit", "remove service unit: %w", err)
	}
	s.installedContent = ""
	if _, err := s.command(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return diagnostic.New("diagnostic.service.reloadRemoval", "reload after service removal: %w", err)
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
		return diagnostic.New("diagnostic.service.action", "service action must be start, stop, or restart")
	}
	if _, err := s.command(ctx, "systemctl", "--user", action, name); err != nil {
		return diagnostic.New("diagnostic.service.control", "%s service %s: %w", action, name, err)
	}
	if action == "stop" {
		waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		for {
			status, err := s.inspect(waitCtx)
			if err != nil {
				return diagnostic.New("diagnostic.service.confirmStop", "confirm service stop: %w", err)
			}
			if status.PID == 0 && (status.State == "inactive" || status.State == "failed" || status.State == "not-found") {
				return nil
			}
			select {
			case <-waitCtx.Done():
				return diagnostic.New("diagnostic.service.notStopped", "service did not stop: %w", waitCtx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	return nil
}

// UpgradeOwned migrates only the exact previously generated unit, after checking
// loaded ownership. Read-only status calls never rewrite a service definition.
func (s *Systemd) UpgradeOwned(ctx context.Context, bin, config, data string) error {
	if err := s.Verify(ctx, bin, config, data); err != nil {
		return err
	}
	content, err := unitContent(bin, config, data)
	if err != nil {
		return err
	}
	path := filepath.Join(s.UnitDir, s.Name+".service")
	if verifyFile(path, content) == nil {
		return nil
	}
	legacy, err := legacyUnitContent(bin, config, data)
	if err != nil {
		return err
	}
	if err := verifyFile(path, legacy); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.Replace(path, []byte(content), []byte(legacy), 0600); err != nil {
		return err
	}
	if _, err := s.command(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		// A cancelled reload may still have reached the manager. Roll back only
		// our exact new bytes and reload using a fresh bounded context.
		rollbackErr := store.Replace(path, []byte(legacy), []byte(content), 0600)
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, reloadErr := s.command(cleanupCtx, "systemctl", "--user", "daemon-reload")
		return errors.Join(diagnostic.New("diagnostic.service.upgradeOwned", "upgrade owned service: %w", err), rollbackErr, reloadErr)
	}
	return nil
}

// SetEnabled changes only boot/login activation; it never starts or stops the
// process. The application verifies installation ownership before calling it.
func (s *Systemd) SetEnabled(ctx context.Context, enabled bool) error {
	name, err := s.unit()
	if err != nil {
		return err
	}
	action := "disable"
	if enabled {
		if err := s.CheckPersistent(ctx); err != nil {
			return err
		}
		action = "enable"
	}
	if _, err := s.command(ctx, "systemctl", "--user", action, name); err != nil {
		return diagnostic.New("diagnostic.service.activation", "%s service activation: %w", action, err)
	}
	status, err := s.inspect(ctx)
	if err != nil {
		return err
	}
	if status.Enabled != enabled || status.EnableState == "" {
		return diagnostic.New("diagnostic.service.enableConfirm", "systemd did not confirm the requested service activation setting")
	}
	return nil
}
