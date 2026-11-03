// Package service manages independent Linux systemd user and system services.
package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
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

type Scope string

const (
	User   Scope = "user"
	System Scope = "system"
)

func CurrentScope() Scope {
	if os.Geteuid() == 0 {
		return System
	}
	return User
}

type Status struct {
	Scope       Scope `json:"scope"`
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
	Scope            Scope
	TUN              bool
	Lifecycle        string
	Home             string
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
	if CurrentScope() == System {
		return &Systemd{Name: name, Scope: System, UnitDir: "/etc/systemd/system"}, nil
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
	return &Systemd{Name: name, Scope: User, UnitDir: filepath.Join(config, "systemd", "user")}, nil
}

func (s *Systemd) command(ctx context.Context, command string, args ...string) ([]byte, error) {
	if command == "systemctl" && s.Scope == System && len(args) > 0 && args[0] == "--user" {
		args = args[1:]
	}
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
	out, err := s.command(ctx, "systemctl", "--user", "show", "--property=SystemState")
	if err == nil && systemdProperty(out, "SystemState") == "" {
		err = diagnostic.New("diagnostic.service.managerState", "systemd did not return a nonempty SystemState property")
	}
	if err != nil {
		if s.Scope == System {
			return diagnostic.New("diagnostic.service.systemManager", "systemd system manager is unavailable: %w", err)
		}
		return diagnostic.New("diagnostic.service.manager", "systemd user manager is unavailable (check your user session and XDG_RUNTIME_DIR): %w", err)
	}
	return nil
}

// Read the named property rather than relying on --value, which older systemd
// tools do not support. Missing or empty values cannot confirm availability.
func systemdProperty(out []byte, name string) string {
	for line := range strings.SplitSeq(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && key == name {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// CheckPersistent refuses to promise logout independence until logind confirms
// linger. Enabling linger is an explicit administrator/user action, never a
// hidden side effect of opening lazyclash or starting a proxy.
func (s *Systemd) CheckPersistent(ctx context.Context) error {
	if err := s.Check(ctx); err != nil {
		return err
	}
	if s.Scope == System {
		return nil
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
	out, err := s.command(ctx, "loginctl", "show-user", strconv.Itoa(os.Getuid()), "--property=Linger")
	if err != nil {
		return false, false
	}
	switch systemdProperty(out, "Linger") {
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
	out, err := s.command(ctx, "systemctl", "--user", "show", name, "--property=LoadState")
	if err != nil {
		return diagnostic.New("diagnostic.service.checkAvailability", "check service availability: %w", err)
	}
	if systemdProperty(out, "LoadState") != "not-found" {
		return diagnostic.New("diagnostic.service.unknownAvailability", "service %s already exists or its availability is unknown", name)
	}
	return nil
}

func (s *Systemd) Inspect(ctx context.Context) (Status, error) {
	status, err := s.inspect(ctx)
	if err != nil {
		return status, err
	}
	// Linger applies only to a user manager.
	if s.Scope != System {
		status.LingerKnown, status.Linger = s.linger(ctx)
	}
	status.Scope = s.scope()
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

func unitContent(bin, config, data string) (string, error) {
	return (&Systemd{}).content(bin, config, data)
}

func (s *Systemd) scope() Scope {
	if s.Scope == System {
		return System
	}
	return User
}

func (s *Systemd) ServiceScope() Scope { return s.scope() }

func (s *Systemd) Account() string {
	sum := sha256.Sum256([]byte(s.Name))
	return fmt.Sprintf("lc-%x", sum[:6])
}

// Identity resolves the dedicated account. Creation happens only during install.
func (s *Systemd) Identity() (int, int, error) {
	u, err := user.Lookup(s.Account())
	if err != nil {
		return 0, 0, err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil || uid == 0 {
		return 0, 0, fmt.Errorf("invalid service uid")
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil || gid == 0 {
		return 0, 0, fmt.Errorf("invalid service gid")
	}
	group, err := user.LookupGroupId(u.Gid)
	if err != nil || group.Name != s.Account() || u.HomeDir != "/nonexistent" || u.Name != "lazyclash managed "+s.Name {
		return 0, 0, fmt.Errorf("existing service account is not a dedicated lazyclash identity")
	}
	groups, err := u.GroupIds()
	if err != nil {
		return 0, 0, err
	}
	for _, group := range groups {
		if group != u.Gid {
			return 0, 0, fmt.Errorf("service account has supplementary groups")
		}
	}
	passwd, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return 0, 0, err
	}
	nologin := false
	for line := range strings.SplitSeq(string(passwd), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) == 7 && fields[0] == s.Account() {
			nologin = fields[6] == "/usr/sbin/nologin"
		}
	}
	if !nologin {
		return 0, 0, fmt.Errorf("service account must have a nologin shell")
	}
	return uid, gid, nil
}

func (s *Systemd) PrepareIdentity(ctx context.Context) error {
	if s.Scope != System {
		return nil
	}
	if _, err := user.Lookup(s.Account()); err == nil {
		_, _, err := s.Identity()
		return err
	}
	if _, err := s.command(ctx, "/usr/sbin/useradd", "--system", "--user-group", "--no-create-home", "--comment", "lazyclash managed "+s.Name, "--home-dir", "/nonexistent", "--shell", "/usr/sbin/nologin", s.Account()); err != nil {
		return err
	}
	_, _, err := s.Identity()
	return err
}

func (s *Systemd) content(bin, config, data string) (string, error) {
	paths := []string{bin, config, data}
	for i := range paths {
		var err error
		paths[i], err = quoteArg(paths[i])
		if err != nil {
			return "", err
		}
	}
	scope := s.scope()
	content := managedHeader + "[Unit]\nDescription=lazyclash Mihomo " + string(scope) + " service\nAfter=network.target\nStartLimitIntervalSec=60s\nStartLimitBurst=5\n\n[Service]\nType=exec\nExecStart=:" + paths[0] + " -d " + paths[2] + " -f " + paths[1] + "\nRestart=on-failure\nRestartSec=2s\nTimeoutStopSec=15s\nKillMode=control-group\nStandardOutput=null\nStandardError=null\nUMask=0077\nNoNewPrivileges=true\n"
	target := "default.target"
	if scope == System {
		target = "multi-user.target"
		content += "User=" + s.Account() + "\nGroup=" + s.Account() + "\nProtectSystem=strict\nProtectHome=true\nPrivateTmp=true\nReadWritePaths=" + paths[2] + "\nCapabilityBoundingSet=\nAmbientCapabilities=\n"
		if s.TUN {
			content += "CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW\nAmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW\n"
		}
		if s.Lifecycle != "" {
			lifecycle, err := quoteArg(s.Lifecycle)
			if err != nil {
				return "", err
			}
			home, err := quoteArg(s.Home)
			if err != nil {
				return "", err
			}
			content += "ExecStartPre=+:" + lifecycle + " __service prepare --home " + home + " --service-name " + s.Name + "\nExecStartPost=+:" + lifecycle + " __service ready --home " + home + " --service-name " + s.Name + "\nExecStopPost=+:" + lifecycle + " __service cleanup --home " + home + " --service-name " + s.Name + "\n"
		}
	}
	return content + "\n[Install]\nWantedBy=" + target + "\n", nil
}

// Install publishes a complete unit atomically without replacing any file.
// It deliberately does not enable or start the service.
func (s *Systemd) Install(ctx context.Context, bin, config, data string) error {
	content, err := s.content(bin, config, data)
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
	expected, err := s.content(bin, config, data)
	if err != nil {
		return err
	}
	path := filepath.Join(s.UnitDir, name)
	if err := verifyFile(path, expected); err != nil {
		return err
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
	expected, err := s.content(bin, config, data)
	if err != nil {
		return err
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

// ResetFailedOwned lets rollback recover from a crashing new binary exhausting
// systemd's start limit. It applies only to the exact managed unit.
func (s *Systemd) ResetFailedOwned(ctx context.Context, bin, config, data string) error {
	if err := s.Verify(ctx, bin, config, data); err != nil {
		return err
	}
	name, err := s.unit()
	if err != nil {
		return err
	}
	if _, err := s.command(ctx, "systemctl", "--user", "reset-failed", name); err != nil {
		return diagnostic.New("diagnostic.service.resetFailed", "reset failed state before service recovery: %w", err)
	}
	return nil
}

// SetTUNPolicy updates only an exactly owned unit and never starts it.
func (s *Systemd) SetTUNPolicy(ctx context.Context, enabled bool, bin, config, data string) error {
	if s.Scope != System {
		if enabled {
			return fmt.Errorf("TUN requires root's system service")
		}
		return nil
	}
	if s.TUN == enabled {
		return nil
	}
	if err := s.Verify(ctx, bin, config, data); err != nil {
		return err
	}
	old, err := s.content(bin, config, data)
	if err != nil {
		return err
	}
	previous := s.TUN
	s.TUN = enabled
	next, err := s.content(bin, config, data)
	if err != nil {
		s.TUN = previous
		return err
	}
	path := filepath.Join(s.UnitDir, s.Name+".service")
	if err := store.Replace(path, []byte(next), []byte(old), 0600); err != nil {
		s.TUN = previous
		return err
	}
	if _, err := s.command(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		s.TUN = previous
		undo := store.Replace(path, []byte(old), []byte(next), 0600)
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, reload := s.command(cleanup, "systemctl", "--user", "daemon-reload")
		return errors.Join(err, undo, reload)
	}
	return nil
}

// VerifyForStop identifies either complete managed policy without trusting a
// possibly damaged configuration. Stop must remain possible during recovery.
func (s *Systemd) VerifyForStop(ctx context.Context, bin, config, data string) error {
	if s.Scope != System {
		return s.Verify(ctx, bin, config, data)
	}
	original := s.TUN
	for _, enabled := range []bool{original, !original} {
		s.TUN = enabled
		if err := s.Verify(ctx, bin, config, data); err == nil {
			return nil
		}
	}
	s.TUN = original
	return s.Verify(ctx, bin, config, data)
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
