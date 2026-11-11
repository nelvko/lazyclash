package shellintegration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nelvko/lazyclash/internal/store"
)

//go:embed scripts/bash.sh scripts/zsh.sh scripts/fish.fish
var integrationScripts embed.FS

const installReceiptVersion = 1

var ErrUninstallPending = errors.New("uninstall pending: another live terminal still needs its first source; initialization files have been preserved")
var ErrInstallPending = errors.New("Shell Integration has an unfinished transaction; retry installation or uninstall")

type installReference struct {
	Path    string `json:"path"`
	Block   string `json:"block"`
	Created bool   `json:"created,omitempty"`
}

type installReceipt struct {
	Version    int                `json:"version"`
	Shell      string             `json:"shell"`
	Script     string             `json:"script"`
	ScriptHash string             `json:"script_hash"`
	References []installReference `json:"references"`
}

type installChange struct {
	Path   string `json:"path"`
	Before []byte `json:"before"`
	After  []byte `json:"after"`
	Exists bool   `json:"exists"`
	Remove bool   `json:"remove,omitempty"`
	Mode   uint32 `json:"mode"`
}

// The private journal records the exact guarded edits, so interruption never
// requires restoring an entire old rc file over subsequent user edits.
type installJournal struct {
	Version   int             `json:"version"`
	Shell     string          `json:"shell"`
	Uninstall bool            `json:"uninstall,omitempty"`
	Receipt   installReceipt  `json:"receipt"`
	Changes   []installChange `json:"changes"`
}

func installerShell(shell string) bool {
	return shell == "bash" || shell == "zsh" || shell == "fish"
}

func (m *Manager) IntegrationPath(shell string) string {
	if !installerShell(shell) {
		return ""
	}
	name := shell + ".sh"
	if shell == "fish" {
		name = "fish.fish"
	}
	return filepath.Join(m.ConfigDir, name)
}

func (m *Manager) ActivationCommand(shell string) string {
	path := m.IntegrationPath(shell)
	if path == "" {
		return ""
	}
	return "source " + integrationQuote(path, shell)
}

func integrationQuote(value, shell string) string {
	if shell == "fish" {
		return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(value) + "'"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func installerHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func (m *Manager) installMetadata() string { return filepath.Join(m.ConfigDir, "metadata") }
func (m *Manager) installReceiptPath(shell string) string {
	return filepath.Join(m.installMetadata(), "installed-"+shell+".json")
}
func (m *Manager) installJournalPath(shell string) string {
	return filepath.Join(m.installMetadata(), "pending-"+shell+".json")
}

// Installation checks the published receipt and the actual script/reference
// bytes. An rc reference by itself does not mean that the current shell loaded
// the integration; Current separately validates its runtime session marker.
func (m *Manager) Installation(shell string) (bool, error) {
	if !installerShell(shell) {
		return false, nil
	}
	if _, err := os.Lstat(m.installJournalPath(shell)); err == nil {
		return false, ErrInstallPending
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	receipt, _, err := m.readInstallReceipt(shell)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return m.verifyInstallReceipt(receipt)
}

// Install edits shell startup files only after the caller has obtained user
// confirmation. It does not load the integration into an already-running shell.
func (m *Manager) Install(shell string) error {
	if !installerShell(shell) {
		return fmt.Errorf("unsupported shell %q", shell)
	}
	if err := m.prepareInstallDirs(); err != nil {
		return err
	}
	unlock, err := m.lockInstaller()
	if err != nil {
		return err
	}
	defer unlock()
	if err := m.recoverInstall(shell); err != nil {
		return err
	}
	if receipt, _, err := m.readInstallReceipt(shell); err == nil {
		ok, err := m.verifyInstallReceipt(receipt)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		return errors.New("installed Shell Integration was modified; refusing to overwrite")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return err
	}
	journal, err := m.planInstall(shell, executable)
	if err != nil {
		return err
	}
	return m.commitInstall(journal)
}

func (m *Manager) lockInstaller() (func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.Lock(ctx, m.installMetadata())
}

func (m *Manager) prepareInstallDirs() error {
	if err := secureDir(m.ConfigDir); err != nil {
		return err
	}
	return secureDir(m.installMetadata())
}

func (m *Manager) planInstall(shell, executable string) (installJournal, error) {
	journal := installJournal{Version: installReceiptVersion, Shell: shell}
	path := m.IntegrationPath(shell)
	script, err := integrationScripts.ReadFile("scripts/" + filepath.Base(path))
	if err != nil {
		return journal, err
	}
	script = bytes.ReplaceAll(script, []byte("@LAZYCLASH_EXECUTABLE@"), []byte(integrationQuote(executable, shell)))
	journal.Receipt = installReceipt{Version: installReceiptVersion, Shell: shell, Script: path, ScriptHash: installerHash(script)}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return journal, err
		}
		return journal, errors.New("unmanaged integration script already exists; refusing to overwrite")
	}
	journal.Changes = append(journal.Changes, installChange{Path: path, After: script, Mode: 0600})
	paths, err := m.startupPaths(shell)
	if err != nil {
		return journal, err
	}
	for _, rc := range paths {
		before, mode, exists, err := readStartup(rc)
		if err != nil {
			return journal, err
		}
		if bytes.Contains(before, []byte("# >>> lazyclash Shell Integration")) || bytes.Contains(before, []byte("# <<< lazyclash Shell Integration")) {
			return journal, fmt.Errorf("unmanaged lazyclash reference in %s; refusing to duplicate or overwrite", rc)
		}
		block := m.integrationReference(shell)
		if len(before) > 0 {
			block = "\n" + block
		}
		after := append(append([]byte{}, before...), []byte(block)...)
		journal.Receipt.References = append(journal.Receipt.References, installReference{Path: rc, Block: block, Created: !exists})
		journal.Changes = append(journal.Changes, installChange{Path: rc, Before: before, After: after, Exists: exists, Mode: uint32(mode.Perm())})
	}
	return journal, nil
}

func (m *Manager) startupPaths(shell string) ([]string, error) {
	if !filepath.IsAbs(m.HomeDir) {
		return nil, errors.New("shell home directory must be absolute")
	}
	switch shell {
	case "bash":
		paths := []string{filepath.Join(m.HomeDir, ".bashrc")}
		// Bash login shells read only the first readable file in this order.
		// Do not create .bash_profile when that would shadow an existing .profile.
		for _, name := range []string{".bash_profile", ".bash_login", ".profile"} {
			path := filepath.Join(m.HomeDir, name)
			if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return nil, err
			}
			f, err := os.Open(path)
			if errors.Is(err, os.ErrPermission) {
				continue
			}
			if err != nil {
				return nil, err
			}
			f.Close()
			return append(paths, path), nil
		}
		return append(paths, filepath.Join(m.HomeDir, ".bash_profile")), nil
	case "zsh":
		dir := os.Getenv("ZDOTDIR")
		if dir == "" {
			dir = m.HomeDir
		}
		if !filepath.IsAbs(dir) {
			return nil, errors.New("ZDOTDIR must be absolute for automatic installation")
		}
		return []string{filepath.Join(dir, ".zshrc")}, nil
	case "fish":
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(m.HomeDir, ".config")
		}
		if !filepath.IsAbs(base) {
			return nil, errors.New("XDG_CONFIG_HOME must be absolute")
		}
		return []string{filepath.Join(base, "fish", "conf.d", "lazyclash.fish")}, nil
	}
	return nil, fmt.Errorf("unsupported shell %q", shell)
}

func (m *Manager) integrationReference(shell string) string {
	path := integrationQuote(m.IntegrationPath(shell), shell)
	start := "# >>> lazyclash Shell Integration (" + shell + ") >>>\n"
	end := "# <<< lazyclash Shell Integration (" + shell + ") <<<\n"
	switch shell {
	case "bash":
		return start + "if [ -n \"${BASH_VERSION-}\" ] && [ -r " + path + " ]; then\n  . " + path + "\nfi\n" + end
	case "zsh":
		return start + "if [[ -o interactive ]] && [[ -r " + path + " ]]; then\n  source " + path + "\nfi\n" + end
	case "fish":
		return start + "if status is-interactive; and test -r " + path + "\n    source " + path + "\nend\n" + end
	}
	return ""
}

func readStartup(path string) ([]byte, os.FileMode, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0600, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, false, fmt.Errorf("refusing non-regular or symlink shell file: %s", path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() {
		return nil, 0, false, fmt.Errorf("shell file is not owned by current user: %s", path)
	}
	if info.Mode().Perm()&0022 != 0 {
		return nil, 0, false, fmt.Errorf("shell file is writable by another user: %s", path)
	}
	data, err := store.Read(path)
	return data, info.Mode(), true, err
}

func (m *Manager) readInstallReceipt(shell string) (installReceipt, []byte, error) {
	var receipt installReceipt
	raw, err := readInstallMetadata(m.installReceiptPath(shell))
	if err != nil {
		return receipt, nil, err
	}
	if json.Unmarshal(raw, &receipt) != nil || receipt.Version != installReceiptVersion || receipt.Shell != shell || receipt.Script != m.IntegrationPath(shell) || len(receipt.References) == 0 {
		return receipt, nil, errors.New("invalid Shell Integration installation receipt; preserving files")
	}
	for _, ref := range receipt.References {
		if !filepath.IsAbs(ref.Path) || ref.Block == "" || !strings.Contains(ref.Block, "# >>> lazyclash Shell Integration ("+shell+") >>>") {
			return receipt, nil, errors.New("invalid Shell Integration reference receipt; preserving files")
		}
	}
	return receipt, raw, nil
}

func (m *Manager) verifyInstallReceipt(receipt installReceipt) (bool, error) {
	script, err := readInstallMetadata(receipt.Script)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if installerHash(script) != receipt.ScriptHash {
		return false, nil
	}
	for _, ref := range receipt.References {
		raw, _, exists, err := readStartup(ref.Path)
		if err != nil {
			return false, err
		}
		if !exists || bytes.Count(raw, []byte(ref.Block)) != 1 {
			return false, nil
		}
	}
	return true, nil
}

func (m *Manager) commitInstall(journal installJournal) error {
	raw, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	if err := store.Replace(m.installJournalPath(journal.Shell), raw, nil, 0600); err != nil {
		return err
	}
	return m.finishInstall(journal, raw)
}

func (m *Manager) recoverInstall(shell string) error {
	raw, err := readInstallMetadata(m.installJournalPath(shell))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal installJournal
	if json.Unmarshal(raw, &journal) != nil || journal.Version != installReceiptVersion || journal.Shell != shell || journal.Receipt.Shell != shell || journal.Receipt.Script != m.IntegrationPath(shell) {
		return errors.New("invalid Shell Integration journal; preserving files")
	}
	return m.finishInstall(journal, raw)
}

// Startup backups/journals can be larger than hook state, but remain private.
// Keep a finite limit while allowing two base64-encoded 16 MiB startup versions.
func readInstallMetadata(path string) ([]byte, error) {
	if err := managedDir(filepath.Dir(path), false); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !owned(info) || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("unsafe private Shell Integration metadata: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, after) {
		return nil, errors.New("Shell Integration metadata changed while reading")
	}
	data, err := io.ReadAll(io.LimitReader(f, 48<<20+1))
	if err == nil && len(data) > 48<<20 {
		err = errors.New("oversized Shell Integration installation metadata")
	}
	return data, err
}

func (m *Manager) finishInstall(journal installJournal, raw []byte) error {
	for i, change := range journal.Changes {
		if !filepath.IsAbs(change.Path) || change.Path == m.installJournalPath(journal.Shell) || change.Path == m.installReceiptPath(journal.Shell) {
			return errors.New("invalid shell startup transaction path")
		}
		actual, actualMode, exists, err := readStartup(change.Path)
		if err != nil {
			return err
		}
		if change.Remove && !exists || !change.Remove && exists && bytes.Equal(actual, change.After) {
			continue
		}
		if exists != change.Exists || !bytes.Equal(actual, change.Before) {
			return fmt.Errorf("shell file changed during transaction: %s; preserving journal", change.Path)
		}
		if exists && !change.Remove && actualMode.Perm() != os.FileMode(change.Mode).Perm() {
			return fmt.Errorf("shell file permissions changed during transaction: %s; preserving journal", change.Path)
		}
		if change.Exists {
			backup := filepath.Join(m.installMetadata(), fmt.Sprintf("backup-%s-%d-%s", journal.Shell, i, installerHash(change.Before)))
			if old, _, present, err := readStartup(backup); err != nil {
				return err
			} else if present && !bytes.Equal(old, change.Before) {
				return errors.New("Shell Integration backup was modified")
			} else if !present {
				if err := store.Replace(backup, change.Before, nil, 0600); err != nil {
					return err
				}
			}
		}
		if err := ensureStartupParent(change.Path); err != nil {
			return err
		}
		if change.Remove {
			err = store.Remove(change.Path, change.Before)
		} else {
			expected := change.Before
			if !change.Exists {
				expected = nil
			}
			err = store.Replace(change.Path, change.After, expected, os.FileMode(change.Mode))
		}
		if err != nil {
			return err
		}
	}
	path := m.installReceiptPath(journal.Shell)
	_, old, err := m.readInstallReceipt(journal.Shell)
	if !errors.Is(err, os.ErrNotExist) && err != nil {
		return err
	}
	if journal.Uninstall {
		if old != nil {
			if err := store.Remove(path, old); err != nil {
				return err
			}
		}
	} else {
		receipt, err := json.MarshalIndent(journal.Receipt, "", "  ")
		if err != nil {
			return err
		}
		if old == nil || !bytes.Equal(receipt, old) {
			if err := store.Replace(path, receipt, old, 0600); err != nil {
				return err
			}
		}
	}
	return store.Remove(m.installJournalPath(journal.Shell), raw)
}

// Existing user configuration directories need not be private, but must not be
// symlinks or writable by another user. Newly created parents are private.
func ensureStartupParent(path string) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("unsafe shell configuration directory: %s", parent)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("shell configuration directory is not owned by current user: %s", parent)
	}
	return nil
}

// FinalizeUninstall removes installation files only. The caller must first wait
// for its shell's restoration ACK; live sessions in other terminals retain their
// state and in-memory hooks. Never restore a backup over the user's current rc.
func (m *Manager) FinalizeUninstall() error {
	if _, err := os.Lstat(m.ConfigDir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := m.prepareInstallDirs(); err != nil {
		return err
	}
	unlock, err := m.lockInstaller()
	if err != nil {
		return err
	}
	defer unlock()
	unlockState, err := m.stateLock()
	if err != nil {
		return err
	}
	defer unlockState()
	if err := m.preservePendingActivation(); err != nil {
		return err
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		if err := m.recoverInstall(shell); err != nil {
			return err
		}
		receipt, _, err := m.readInstallReceipt(shell)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		journal, err := m.planUninstall(receipt)
		if err != nil {
			return err
		}
		if err := m.commitInstall(journal); err != nil {
			return err
		}
	}
	return nil
}

func pendingNeedsIntegration(pending pendingRecord, now time.Time, alive bool) bool {
	return pending.Enabled && alive && !pending.UpdatedAt.IsZero() && !pending.UpdatedAt.After(now.Add(time.Minute)) && now.Sub(pending.UpdatedAt) <= pendingTTL
}

func (m *Manager) preservePendingActivation() error {
	entries, err := os.ReadDir(filepath.Join(m.StateDir, "pending"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var pending pendingRecord
		if err := readJSON(filepath.Join(m.StateDir, "pending", entry.Name()), &pending); err != nil {
			return fmt.Errorf("cannot verify another terminal's pending activation; preserving initialization files: %w", err)
		}
		if pendingNeedsIntegration(pending, now, m.isOwnerAlive(pending.Owner)) {
			return ErrUninstallPending
		}
	}
	return nil
}

func (m *Manager) planUninstall(receipt installReceipt) (installJournal, error) {
	journal := installJournal{Version: installReceiptVersion, Shell: receipt.Shell, Uninstall: true, Receipt: receipt}
	for _, ref := range receipt.References {
		before, mode, exists, err := readStartup(ref.Path)
		if err != nil {
			return journal, err
		}
		if !exists {
			continue
		}
		if bytes.Count(before, []byte(ref.Block)) != 1 {
			return journal, fmt.Errorf("owned Shell Integration reference was modified: %s; preserving file", ref.Path)
		}
		after := bytes.Replace(before, []byte(ref.Block), nil, 1)
		journal.Changes = append(journal.Changes, installChange{Path: ref.Path, Before: before, After: after, Exists: true, Remove: ref.Created && len(after) == 0, Mode: uint32(mode.Perm())})
	}
	script, err := readInstallMetadata(receipt.Script)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return journal, err
	}
	if err == nil {
		if installerHash(script) != receipt.ScriptHash {
			return journal, errors.New("owned Shell Integration script was modified; preserving it")
		}
		journal.Changes = append(journal.Changes, installChange{Path: receipt.Script, Before: script, Exists: true, Remove: true})
	}
	return journal, nil
}
