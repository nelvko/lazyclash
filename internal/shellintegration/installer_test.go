package shellintegration

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/store"
)

func installerManager(t *testing.T) *Manager {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZDOTDIR", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return &Manager{HomeDir: home, ConfigDir: filepath.Join(home, ".config", "lazyclash", "shell"), StateDir: filepath.Join(home, ".cache", "lazyclash", "shell")}
}

func writeStartup(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func startupValue(t *testing.T, path string) string {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(value)
}

func TestInstallBashUsesActualLoginPriority(t *testing.T) {
	for _, files := range [][]string{{}, {".profile"}, {".bash_login", ".profile"}, {".bash_profile", ".bash_login", ".profile"}} {
		t.Run(strings.Join(files, "+"), func(t *testing.T) {
			m := installerManager(t)
			for _, file := range files {
				writeStartup(t, filepath.Join(m.HomeDir, file), "# user "+file+"\n")
			}
			if err := m.Install("bash"); err != nil {
				t.Fatal(err)
			}
			selected := ".bash_profile"
			if len(files) > 0 {
				selected = files[0]
			}
			for _, file := range []string{".bash_profile", ".bash_login", ".profile"} {
				path := filepath.Join(m.HomeDir, file)
				content, err := os.ReadFile(path)
				if file == selected {
					if err != nil || !bytes.Contains(content, []byte("# >>> lazyclash Shell Integration (bash) >>>")) {
						t.Fatalf("selected startup %s: %s, %v", file, content, err)
					}
				} else if err == nil && bytes.Contains(content, []byte("lazyclash")) {
					t.Fatalf("modified lower-priority startup %s", file)
				} else if errors.Is(err, os.ErrNotExist) && file == ".bash_profile" && selected != file {
					// In particular, never shadow an existing .profile/.bash_login.
					continue
				}
			}
			if !strings.Contains(startupValue(t, filepath.Join(m.HomeDir, ".bashrc")), ". '"+m.IntegrationPath("bash")+"'") {
				t.Fatal("non-login interactive Bash has no initialization reference")
			}
		})
	}
}

func TestInstallIdempotentPreservesConfigAndPermissions(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			m := installerManager(t)
			paths, err := m.startupPaths(shell)
			if err != nil {
				t.Fatal(err)
			}
			const original = "# custom configuration without final newline"
			for _, path := range paths {
				writeStartup(t, path, original)
				if err := os.Chmod(path, 0640); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.Install(shell); err != nil {
				t.Fatal(err)
			}
			first := startupValue(t, paths[0])
			if err := m.Install(shell); err != nil {
				t.Fatal(err)
			}
			if got := startupValue(t, paths[0]); got != first || strings.Count(got, "# >>> lazyclash") != 1 || !strings.HasPrefix(got, original) {
				t.Fatalf("repeat installation changed user configuration: %q", got)
			}
			if ok, err := m.Installation(shell); !ok || err != nil {
				t.Fatalf("installation not verified: %t %v", ok, err)
			}
			for _, path := range paths {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0640 {
					t.Fatalf("startup permissions changed: %v %v", info, err)
				}
			}
			for _, path := range []string{m.ConfigDir, m.installMetadata()} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0700 {
					t.Fatalf("private directory mode: %v %v", info, err)
				}
			}
			for _, path := range []string{m.IntegrationPath(shell), m.installReceiptPath(shell)} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("private integration mode: %v %v", info, err)
				}
			}
			backups, _ := filepath.Glob(filepath.Join(m.installMetadata(), "backup-*"))
			if len(backups) != len(paths) {
				t.Fatalf("backup count %d, want %d", len(backups), len(paths))
			}
			for _, backup := range backups {
				if got := startupValue(t, backup); got != original {
					t.Fatalf("backup differs: %q", got)
				}
			}
		})
	}
}

func TestRepairMissingBashReferencePreservesUserFiles(t *testing.T) {
	m := stateManager(t, testOwner(100))
	rc := filepath.Join(m.HomeDir, ".bashrc")
	profile := filepath.Join(m.HomeDir, ".profile")
	writeStartup(t, profile, "# login\n")
	if err := m.InstallCurrent(); err != nil {
		t.Fatal(err)
	}
	originalScript := startupValue(t, m.IntegrationPath("bash"))
	originalProfile := startupValue(t, profile)
	const userRC = "export MY_OPTION=yes" // no final newline
	writeStartup(t, rc, userRC)
	if err := os.Chmod(rc, 0640); err != nil {
		t.Fatal(err)
	}
	status, err := m.Current()
	if err != nil || status.State != "incomplete" || status.ActivationCommand != "" {
		t.Fatalf("expected a repairable installation, got %+v, %v", status, err)
	}
	for range 2 {
		if err := m.InstallCurrent(); err != nil {
			t.Fatal(err)
		}
	}
	if got := startupValue(t, rc); got != userRC+"\n"+m.integrationReference("bash") {
		t.Fatalf("unexpected repaired rc: %q", got)
	}
	if startupValue(t, profile) != originalProfile || startupValue(t, m.IntegrationPath("bash")) != originalScript {
		t.Fatal("repair changed an intact file")
	}
	if err := m.FinalizeUninstall(); err != nil {
		t.Fatal(err)
	}
	if got := startupValue(t, rc); got != userRC {
		t.Fatalf("uninstall did not restore user's exact contents: %q", got)
	}
	info, err := os.Stat(rc)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("startup file lost original permissions: %v %v", info, err)
	}
}

func TestRepairPreservesConflictingStartupFilesAndScripts(t *testing.T) {
	for _, issue := range []string{"partial reference", "duplicate reference", "unmarked source", "edited script"} {
		t.Run(issue, func(t *testing.T) {
			m := stateManager(t, testOwner(100))
			if err := m.InstallCurrent(); err != nil {
				t.Fatal(err)
			}
			rc := filepath.Join(m.HomeDir, ".bashrc")
			script := m.IntegrationPath("bash")
			path := rc
			switch issue {
			case "partial reference":
				writeStartup(t, rc, "# >>> lazyclash Shell Integration (bash) >>>\n# customized\n")
			case "duplicate reference":
				writeStartup(t, rc, strings.Repeat(m.integrationReference("bash"), 2))
			case "unmarked source":
				writeStartup(t, rc, "source "+script+"\n")
			case "edited script":
				path = script
				writeStartup(t, script, "# custom hook\n")
			}
			before := startupValue(t, path)
			status, err := m.Current()
			if err != nil || status.State != "conflict" || !strings.Contains(status.Detail, path) || status.ActivationCommand != "" {
				t.Fatalf("expected a conflict naming %s, got %+v, %v", path, status, err)
			}
			if err := m.InstallCurrent(); err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("installation should explain the conflict at %s: %v", path, err)
			}
			if startupValue(t, path) != before {
				t.Fatal("changed user file")
			}
		})
	}
}

func TestRepairRecreatesMissingHookAndNeverShadowsBashLoginProfile(t *testing.T) {
	m := stateManager(t, testOwner(100))
	if err := m.InstallCurrent(); err != nil {
		t.Fatal(err)
	}
	hook := m.IntegrationPath("bash")
	expected := startupValue(t, hook)
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if err := m.InstallCurrent(); err != nil || startupValue(t, hook) != expected {
		t.Fatalf("failed to restore the missing hook: %v", err)
	}
	oldLogin := filepath.Join(m.HomeDir, ".bash_profile")
	if err := os.Remove(oldLogin); err != nil {
		t.Fatal(err)
	}
	writeStartup(t, filepath.Join(m.HomeDir, ".profile"), "# new login configuration\n")
	if err := m.InstallCurrent(); err == nil {
		t.Fatal("repair accepted a stale Bash login path")
	}
	if _, err := os.Lstat(oldLogin); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repair shadowed the user's .profile: %v", err)
	}
}

func TestInstallZshRespectsZDOTDIR(t *testing.T) {
	m := installerManager(t)
	dot := filepath.Join(m.HomeDir, "custom-zsh")
	t.Setenv("ZDOTDIR", dot)
	if err := m.Install("zsh"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(startupValue(t, filepath.Join(dot, ".zshrc")), "source ") {
		t.Fatal("custom ZDOTDIR not initialized")
	}
	if _, err := os.Lstat(filepath.Join(m.HomeDir, ".zshrc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default zshrc changed: %v", err)
	}
}

func TestInstallRejectsUnsafeOrUnmanagedFiles(t *testing.T) {
	for _, scenario := range []string{"symlink-rc", "writable-rc", "unmanaged-marker", "unmanaged-script", "symlink-config"} {
		t.Run(scenario, func(t *testing.T) {
			m := installerManager(t)
			rc := filepath.Join(m.HomeDir, ".bashrc")
			writeStartup(t, rc, "# original\n")
			switch scenario {
			case "symlink-rc":
				target := filepath.Join(m.HomeDir, "realrc")
				writeStartup(t, target, "# target\n")
				os.Remove(rc)
				if err := os.Symlink(target, rc); err != nil {
					t.Fatal(err)
				}
			case "writable-rc":
				if err := os.Chmod(rc, 0666); err != nil {
					t.Fatal(err)
				}
			case "unmanaged-marker":
				writeStartup(t, rc, "# >>> lazyclash Shell Integration (bash) >>>\n# unknown\n")
			case "unmanaged-script":
				writeStartup(t, m.IntegrationPath("bash"), "# unknown\n")
			case "symlink-config":
				target := filepath.Join(m.HomeDir, "actual-shell")
				if err := os.MkdirAll(filepath.Dir(m.ConfigDir), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, m.ConfigDir); err != nil {
					t.Fatal(err)
				}
			}
			before := startupValue(t, rc)
			if err := m.Install("bash"); err == nil {
				t.Fatal("unsafe installation succeeded")
			}
			if after := startupValue(t, rc); after != before {
				t.Fatal("failed installation modified rc")
			}
			if ok, _ := m.Installation("bash"); ok {
				t.Fatal("failed installation reports installed")
			}
		})
	}
}

func TestInstallRecoveryCompletesInterruptedExactEdits(t *testing.T) {
	m := installerManager(t)
	writeStartup(t, filepath.Join(m.HomeDir, ".bashrc"), "# original\n")
	if err := m.prepareInstallDirs(); err != nil {
		t.Fatal(err)
	}
	journal, err := m.planInstall("bash", "/bin/lazyclash")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(journal)
	if err := store.Replace(m.installJournalPath("bash"), raw, nil, 0600); err != nil {
		t.Fatal(err)
	}
	change := journal.Changes[0]
	if err := store.Replace(change.Path, change.After, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if ok, err := m.Installation("bash"); ok || err == nil {
		t.Fatal("unfinished installation was reported as complete")
	}
	if err := m.Install("bash"); err != nil {
		t.Fatal(err)
	}
	if ok, err := m.Installation("bash"); !ok || err != nil {
		t.Fatalf("recovered installation not verified: %t %v", ok, err)
	}
	if _, err := os.Lstat(m.installJournalPath("bash")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed journal was retained")
	}
}

func TestInstallRecoveryPreservesConcurrentUserEdits(t *testing.T) {
	m := installerManager(t)
	rc := filepath.Join(m.HomeDir, ".bashrc")
	writeStartup(t, rc, "# original\n")
	if err := m.prepareInstallDirs(); err != nil {
		t.Fatal(err)
	}
	journal, err := m.planInstall("bash", "/bin/lazyclash")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(journal)
	if err := store.Replace(m.installJournalPath("bash"), raw, nil, 0600); err != nil {
		t.Fatal(err)
	}
	const edited = "# user edited after interrupted installation\n"
	writeStartup(t, rc, edited)
	if err := m.Install("bash"); err == nil {
		t.Fatal("changed startup was overwritten")
	}
	if got := startupValue(t, rc); got != edited {
		t.Fatal("user edits lost")
	}
	if _, err := os.Lstat(m.installJournalPath("bash")); err != nil {
		t.Fatal("conflicting journal not retained")
	}
}

func TestInstallRecoveryPreservesConcurrentPermissionEdits(t *testing.T) {
	m := installerManager(t)
	rc := filepath.Join(m.HomeDir, ".bashrc")
	writeStartup(t, rc, "# original\n")
	if err := m.prepareInstallDirs(); err != nil {
		t.Fatal(err)
	}
	journal, err := m.planInstall("bash", "/bin/lazyclash")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(journal)
	if err := store.Replace(m.installJournalPath("bash"), raw, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(rc, 0640); err != nil {
		t.Fatal(err)
	}
	if err := m.Install("bash"); err == nil {
		t.Fatal("concurrent chmod was overwritten")
	}
	info, err := os.Stat(rc)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("concurrent chmod lost: %v %v", info, err)
	}
}

func TestInstallMetadataRequiresPrivateFiles(t *testing.T) {
	for _, target := range []string{"receipt", "script"} {
		t.Run(target, func(t *testing.T) {
			m := installerManager(t)
			if err := m.Install("bash"); err != nil {
				t.Fatal(err)
			}
			path := m.installReceiptPath("bash")
			if target == "script" {
				path = m.IntegrationPath("bash")
			}
			if err := os.Chmod(path, 0644); err != nil {
				t.Fatal(err)
			}
			if ok, err := m.Installation("bash"); ok || err == nil {
				t.Fatal("non-private metadata accepted")
			}
		})
	}
}

func TestInstallLargeStartupRecovery(t *testing.T) {
	m := installerManager(t)
	rc := filepath.Join(m.HomeDir, ".bashrc")
	original := strings.Repeat("# existing plugin configuration\n", 4000)
	writeStartup(t, rc, original)
	if err := m.prepareInstallDirs(); err != nil {
		t.Fatal(err)
	}
	journal, err := m.planInstall("bash", "/bin/lazyclash")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(journal)
	if len(raw) < 64*1024 {
		t.Fatal("fixture must exceed hook state limit")
	}
	if err := store.Replace(m.installJournalPath("bash"), raw, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Install("bash"); err != nil {
		t.Fatal(err)
	}
	if got := startupValue(t, rc); !strings.HasPrefix(got, original) {
		t.Fatal("large rc content changed")
	}
}

func TestUninstallPreservesUserEditsAndLiveState(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			m := installerManager(t)
			paths, err := m.startupPaths(shell)
			if err != nil {
				t.Fatal(err)
			}
			const original = "# original no final newline"
			writeStartup(t, paths[0], original)
			if err := m.Install(shell); err != nil {
				t.Fatal(err)
			}
			writeStartup(t, paths[0], startupValue(t, paths[0])+"# added after install\n")
			live := filepath.Join(m.StateDir, "sessions", "other-terminal", "request")
			writeStartup(t, live, "1 99 1 127.0.0.1 7890 0\n")
			if err := m.FinalizeUninstall(); err != nil {
				t.Fatal(err)
			}
			if got := startupValue(t, paths[0]); got != original+"# added after install\n" {
				// Removing precisely the appended block retains all user bytes.
				t.Fatalf("uninstall changed user bytes: %q", got)
			}
			if got := startupValue(t, live); got != "1 99 1 127.0.0.1 7890 0\n" {
				t.Fatal("uninstall touched another terminal")
			}
			if _, err := os.Lstat(m.IntegrationPath(shell)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("script retained: %v", err)
			}
			if len(paths) > 1 {
				if _, err := os.Lstat(paths[1]); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("installer-created empty login file retained")
				}
			}
			if err := m.FinalizeUninstall(); err != nil {
				t.Fatal("repeated uninstall:", err)
			}
		})
	}
}

func TestUninstallRejectsModifiedOwnedReference(t *testing.T) {
	m := installerManager(t)
	if err := m.Install("bash"); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(m.HomeDir, ".bashrc")
	edited := strings.Replace(startupValue(t, rc), "# >>> lazyclash", "# user changed lazyclash", 1)
	writeStartup(t, rc, edited)
	if err := m.FinalizeUninstall(); err == nil {
		t.Fatal("modified reference silently deleted")
	}
	if got := startupValue(t, rc); got != edited {
		t.Fatal("modified reference overwritten")
	}
}

func TestUninstallRecoveryCompletesInterruptedOwnedEdits(t *testing.T) {
	m := installerManager(t)
	rc := filepath.Join(m.HomeDir, ".bashrc")
	const original = "# user configuration\n"
	writeStartup(t, rc, original)
	if err := m.Install("bash"); err != nil {
		t.Fatal(err)
	}
	receipt, _, err := m.readInstallReceipt("bash")
	if err != nil {
		t.Fatal(err)
	}
	journal, err := m.planUninstall(receipt)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(journal)
	if err := store.Replace(m.installJournalPath("bash"), raw, nil, 0600); err != nil {
		t.Fatal(err)
	}
	first := journal.Changes[0]
	if err := store.Replace(first.Path, first.After, first.Before, os.FileMode(first.Mode)); err != nil {
		t.Fatal(err)
	}
	if err := m.FinalizeUninstall(); err != nil {
		t.Fatal(err)
	}
	if got := startupValue(t, rc); got != original {
		t.Fatalf("recovered uninstall lost configuration: %q", got)
	}
	if _, err := os.Lstat(m.IntegrationPath("bash")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("script not removed: %v", err)
	}
	if _, err := os.Lstat(m.installJournalPath("bash")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed uninstall journal retained: %v", err)
	}
	if installed, err := m.Installation("bash"); installed || err != nil {
		t.Fatalf("removed integration still installed: %t %v", installed, err)
	}
}

func TestUninstallPreservesRequiredPendingIntegration(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		name    string
		enabled bool
		alive   bool
		updated time.Time
		blocks  bool
	}{
		{"live-enabled", true, true, now.Add(-time.Hour), true},
		{"live-disabled", false, true, now.Add(-time.Hour), false},
		{"dead-enabled", true, false, now.Add(-time.Hour), false},
		{"expired-enabled", true, true, now.Add(-pendingTTL - time.Second), false},
		{"invalid-date", true, true, time.Time{}, false},
		{"future-date", true, true, now.Add(time.Hour), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			pending := pendingRecord{Enabled: test.enabled, UpdatedAt: test.updated}
			if got := pendingNeedsIntegration(pending, now, test.alive); got != test.blocks {
				t.Fatalf("blocks=%t, want %t", got, test.blocks)
			}
		})
	}
}

func TestUninstallWaitsForForeignPendingSourceThenRetries(t *testing.T) {
	m := installerManager(t)
	owner := Identity{UID: os.Getuid(), PID: 12345, Start: "321", BootID: "test-boot", TTY: 9, Shell: "bash", PIDNamespace: "test-ns"}
	m.caller = func() (Identity, error) { return owner, nil }
	m.attachCaller = func(string, int) (Identity, error) { return owner, nil }
	m.alive = func(Identity) bool { return true }
	t.Setenv(SessionVariable, "")
	t.Setenv(LoadedVariable, "")
	if err := m.Install("bash"); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(true, Endpoint{Namespace: "test-instance", Host: "127.0.0.1", Port: 7890}); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(m.HomeDir, ".bashrc")
	before := startupValue(t, rc)
	if err := m.FinalizeUninstall(); !errors.Is(err, ErrUninstallPending) {
		t.Fatalf("cleanup broke pending terminal or hid deferred reason: %v", err)
	}
	if got := startupValue(t, rc); got != before {
		t.Fatal("deferred uninstall changed initialization files")
	}
	if ok, err := m.Installation("bash"); !ok || err != nil {
		t.Fatalf("pending terminal cannot source installation: %t %v", ok, err)
	}
	attachment, err := m.Attach("bash", owner.PID)
	if err != nil {
		t.Fatal(err)
	}
	request, err := m.readRequest(attachment.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	ackState(t, m, attachment.SessionID, request, false)
	if err := m.FinalizeAttach(attachment.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := m.FinalizeUninstall(); err != nil {
		t.Fatal("retry after foreign activation failed:", err)
	}
	requestAfter, err := m.readRequest(attachment.SessionID)
	if err != nil || requestAfter != request || !requestAfter.Enabled {
		t.Fatalf("cleanup changed other terminal's active request: %+v %v", requestAfter, err)
	}
}

func TestIntegrationQuoteRoundTripSyntax(t *testing.T) {
	value := "/tmp/a b'c\\d"
	if got := integrationQuote(value, "bash"); got != "'/tmp/a b'\\''c\\d'" {
		t.Fatalf("POSIX quoting: %q", got)
	}
	if got := integrationQuote(value, "fish"); got != "'/tmp/a b\\'c\\\\d'" {
		t.Fatalf("Fish quoting: %q", got)
	}
}
