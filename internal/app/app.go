// Package app implements the use cases shared by the TUI and CLI.
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"
)

const DefaultService = "lazyclash-mihomo"

type Services interface {
	Check(context.Context) error
	Available(context.Context) error
	Inspect(context.Context) (service.Status, error)
	Install(context.Context, string, string, string) error
	Verify(context.Context, string, string, string) error
	RemoveOwned(context.Context, string, string, string) error
	Action(context.Context, string) error
}

type App struct {
	Home, Name    string
	Service       Services
	PlatformError error
	// Replacements are narrow seams for deterministic business/HTTP tests.
	Download   func(context.Context, string, func(string)) error
	Version    func(context.Context, string) (string, error)
	Validate   func(context.Context, string, string, string) error
	HTTP       *http.Client
	CheckPorts func(controller string, mixedPort int) error
}

type Snapshot struct {
	Installed    bool
	Version      string
	Service      service.Status
	ServiceError string
	Controller   string
	Applied      string
	Profiles     []string
	Groups       []mihomo.Group
	APIError     string
}

type manifest struct {
	ID         string `json:"id"`
	Service    string `json:"service"`
	Version    string `json:"version"`
	Controller string `json:"controller"`
	Secret     string `json:"secret"`
	Port       int    `json:"mixed_port"`
	Applied    string `json:"applied"`
	SHA        string `json:"config_sha256"`
	BinarySHA  string `json:"binary_sha256"`
}

type installJournal struct {
	manifest
	Tree map[string]string `json:"tree"`
}

type pending struct {
	Old, New string
	Active   bool
}

var profileName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,47}$`)
var generationName = regexp.MustCompile(`^generations/g-[a-f0-9]{24}$`)

func New(home, name string) (*App, error) {
	root, err := store.Home(home)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = os.Getenv("LAZYCLASH_SERVICE")
	}
	if name == "" {
		name = DefaultService
	}
	if !regexp.MustCompile(`^lazyclash-[A-Za-z0-9-]+$`).MatchString(name) {
		return nil, errors.New("invalid lazyclash service name")
	}
	svc, platformErr := service.New(name)
	a := &App{Home: root, Name: name, PlatformError: platformErr, Download: mihomo.Download, Version: mihomo.BinaryVersion, Validate: mihomo.Validate}
	if svc != nil {
		a.Service = svc
	}
	return a, nil
}
func (a *App) paths() (string, string, string) {
	return filepath.Join(a.Home, "installation", "mihomo"), filepath.Join(a.Home, "installation", "current", "config.yaml"), filepath.Join(a.Home, "installation", "runtime")
}
func randomID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), err
}
func (a *App) requireService() error {
	if a.Service == nil {
		if a.PlatformError != nil {
			return a.PlatformError
		}
		return errors.New("Linux systemd user service unavailable")
	}
	return nil
}
func cleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 20*time.Second)
}

func (a *App) locked(ctx context.Context, fn func() error) error {
	unlock, err := store.Lock(ctx, a.Home)
	if err != nil {
		return err
	}
	defer unlock()
	if err := a.recover(ctx); err != nil {
		return err
	}
	return fn()
}
func (a *App) load() (manifest, string, error) {
	dir := filepath.Join(a.Home, "installation")
	if _, err := os.Lstat(dir); err != nil {
		return manifest{}, "", err
	}
	for _, p := range []string{dir, filepath.Join(dir, "generations"), filepath.Join(dir, "runtime")} {
		if err := existingDir(p); err != nil {
			return manifest{}, "", fmt.Errorf("installation is incomplete or unsafe: %v", err)
		}
	}
	target, err := os.Readlink(filepath.Join(dir, "current"))
	if err != nil {
		return manifest{}, "", errors.New("installation has no valid current generation; refusing to modify")
	}
	if !generationName.MatchString(target) {
		return manifest{}, "", errors.New("invalid current generation target")
	}
	if err := existingDir(filepath.Join(dir, target)); err != nil {
		return manifest{}, "", fmt.Errorf("installation generation is incomplete or unsafe: %v", err)
	}
	raw, err := store.Read(filepath.Join(dir, target, "manifest.json"))
	if err != nil {
		return manifest{}, "", fmt.Errorf("installation manifest is unavailable: %v", err)
	}
	var m manifest
	if json.Unmarshal(raw, &m) != nil || m.ID == "" || m.Secret == "" || m.Service != a.Name {
		return m, "", errors.New("invalid installation manifest or service name mismatch")
	}
	cfg, err := store.Read(filepath.Join(dir, target, "config.yaml"))
	if err != nil {
		return m, "", fmt.Errorf("installation configuration is unavailable: %v", err)
	}
	if digest(cfg) != m.SHA {
		return m, "", errors.New("active configuration was modified; refusing to replace user changes")
	}
	bin, _, _ := a.paths()
	info, err := os.Lstat(bin)
	if err != nil {
		return m, "", fmt.Errorf("installation binary is unavailable: %v", err)
	}
	if !info.Mode().IsRegular() {
		return m, "", errors.New("installed binary is not a regular file")
	}
	hash, err := fileDigest(bin)
	if err != nil {
		return m, "", err
	}
	if hash != m.BinarySHA {
		return m, "", errors.New("installed binary was modified; refusing to operate")
	}
	// Validate stored connection settings without exposing credentials.
	if _, err := mihomo.Default(m.Controller, m.Secret, m.Port); err != nil {
		return m, "", errors.New("invalid managed controller settings")
	}
	return m, target, nil
}
func (a *App) writeGeneration(base string, m manifest, config []byte) (string, error) {
	target := "generations/g-" + randomID()
	dir := filepath.Join(base, target)
	if err := store.Dir(filepath.Join(base, "generations")); err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(dir)
		}
	}()
	m.SHA = digest(config)
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	if err := store.Replace(filepath.Join(dir, "config.yaml"), config, nil, 0600); err != nil {
		return "", err
	}
	if err := store.Replace(filepath.Join(dir, "manifest.json"), raw, nil, 0600); err != nil {
		return "", err
	}
	complete = true
	return target, nil
}

// existingDir is deliberately read-only. A damaged installation must not be
// repaired implicitly by a status query or mistaken for a fresh installation.
func existingDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("managed path is not a real directory")
	}
	return nil
}

// installationTree identifies the exact files produced before publishing an
// install. Recovery preserves the whole tree when later user changes appear.
func installationTree(base string) (map[string]string, error) {
	tree := make(map[string]string)
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			tree[rel] = "directory"
		case entry.Type()&os.ModeSymlink != 0:
			if rel != "current" {
				return errors.New("installation contains an unexpected symbolic link")
			}
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if !generationName.MatchString(target) {
				return errors.New("installation current target is invalid")
			}
			tree[rel] = "symlink:" + target
		case entry.Type().IsRegular():
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Size() > 256<<20 {
				return errors.New("installation file exceeds size limit")
			}
			hash, err := fileDigest(path)
			if err != nil {
				return err
			}
			tree[rel] = "file:" + hash
		default:
			return errors.New("installation contains a non-regular file")
		}
		return nil
	})
	return tree, err
}

func (a *App) recover(ctx context.Context) error {
	// A pending apply is always rolled back after interruption. The old complete
	// generation remains available until the new service has passed readiness.
	path := filepath.Join(a.Home, "apply-pending.json")
	raw, err := store.Read(path)
	if errors.Is(err, os.ErrNotExist) {
		return a.recoverInstall(ctx)
	}
	if err != nil {
		return err
	}
	var p pending
	if json.Unmarshal(raw, &p) != nil || !generationName.MatchString(p.Old) || !generationName.MatchString(p.New) {
		return errors.New("invalid recovery journal; manual inspection required")
	}
	if err := a.requireService(); err != nil {
		return err
	}
	current := filepath.Join(a.Home, "installation", "current")
	target, err := os.Readlink(current)
	if err != nil {
		return err
	}
	if target == p.New {
		if err := store.Switch(current, p.Old, p.New); err != nil {
			return err
		}
	} else if target != p.Old {
		return errors.New("recovery target changed; refusing to modify")
	}
	m, _, err := a.load()
	if err != nil {
		return err
	}
	if p.Active {
		if err := a.restartReady(ctx, m); err != nil {
			return fmt.Errorf("configuration restored but service recovery failed: %w", err)
		}
	}
	return os.Remove(path)
}
func (a *App) recoverInstall(ctx context.Context) error {
	path := filepath.Join(a.Home, "install-pending.json")
	raw, err := store.Read(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal installJournal
	if json.Unmarshal(raw, &journal) != nil || journal.ID == "" || journal.Service != a.Name || len(journal.Tree) == 0 {
		return errors.New("invalid installation recovery journal")
	}
	m := journal.manifest
	if err := a.requireService(); err != nil {
		return err
	}
	bin, config, data := a.paths()
	if _, err := os.Lstat(filepath.Join(a.Home, "installation")); err == nil {
		current, _, err := a.load()
		if err != nil || current.ID != m.ID {
			return errors.New("interrupted installation changed; refusing to remove files")
		}
		tree, err := installationTree(filepath.Join(a.Home, "installation"))
		if err != nil || !maps.Equal(tree, journal.Tree) {
			return errors.New("interrupted installation contains changed or additional files; preserving files for manual inspection")
		}
		if err := a.Service.RemoveOwned(ctx, bin, config, data); err != nil {
			return fmt.Errorf("installation rollback: %w", err)
		}
		if err := os.RemoveAll(filepath.Join(a.Home, "installation")); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Remove(path)
}
func (a *App) Snapshot(ctx context.Context) (Snapshot, error) {
	var out Snapshot
	err := a.locked(ctx, func() error {
		m, _, err := a.load()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			out.Installed = true
			out.Version = m.Version
			out.Controller = m.Controller
			out.Applied = m.Applied
		}
		if a.Service == nil {
			out.Service.State = "unsupported"
			if a.PlatformError != nil {
				out.ServiceError = a.PlatformError.Error()
			}
		} else {
			s, err := a.Service.Inspect(ctx)
			out.Service = s
			if err != nil {
				out.ServiceError = err.Error()
			}
		}
		profilesDir := filepath.Join(a.Home, "profiles")
		if err := existingDir(profilesDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		entries, err := os.ReadDir(profilesDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		out.Profiles = []string{"default"}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				out.Profiles = append(out.Profiles, strings.TrimSuffix(e.Name(), ".yaml"))
			}
		}
		sort.Strings(out.Profiles[1:])
		if out.Installed && out.Service.Active {
			out.Groups, err = a.client(m).Groups(ctx)
			if err != nil {
				out.APIError = err.Error()
			}
		} else if out.Installed {
			out.APIError = "Start the service to view proxy groups"
		}
		return ctx.Err()
	})
	return out, err
}
func (a *App) client(m manifest) mihomo.Client {
	return mihomo.Client{BaseURL: "http://" + m.Controller, Secret: m.Secret, HTTP: a.HTTP}
}

func copyBinary(ctx context.Context, src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return errors.New("cannot read local Mihomo binary")
	}
	if !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return errors.New("local binary must be a regular file at most 256 MiB")
	}
	in, err := os.Open(src)
	if err != nil {
		return errors.New("cannot open local Mihomo binary")
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	defer out.Close()
	buf := make([]byte, 128<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := in.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return errors.New("cannot copy local Mihomo binary")
		}
	}
	return out.Sync()
}
func (a *App) checkPorts(m manifest) error {
	if a.CheckPorts != nil {
		return a.CheckPorts(m.Controller, m.Port)
	}
	ports := []string{m.Controller, fmt.Sprintf("127.0.0.1:%d", m.Port)}
	for _, addr := range ports {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("local port %s is unavailable; no existing service was stopped", addr)
		}
		l.Close()
	}
	return nil
}
func (a *App) Install(ctx context.Context, local string, progress func(string)) error {
	return a.locked(ctx, func() (result error) {
		if err := a.requireService(); err != nil {
			return err
		}
		if err := a.Service.Check(ctx); err != nil {
			return err
		}
		if err := a.Service.Available(ctx); err != nil {
			return err
		}
		final := filepath.Join(a.Home, "installation")
		if _, err := os.Lstat(final); !errors.Is(err, os.ErrNotExist) {
			return errors.New("installation path already exists; refusing to overwrite")
		}
		m := manifest{ID: randomID(), Service: a.Name, Controller: "127.0.0.1:9097", Secret: randomID() + randomID(), Port: 7897, Applied: "default"}
		if err := a.checkPorts(m); err != nil {
			return err
		}
		stage, err := os.MkdirTemp(a.Home, ".install-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		report := func(s string) {
			if progress != nil {
				progress(s)
			}
		}
		bin := filepath.Join(stage, "mihomo")
		if local == "" {
			err = a.Download(ctx, bin, progress)
		} else {
			report("Copying and validating local Mihomo binary…")
			err = copyBinary(ctx, local, bin)
		}
		if err != nil {
			return err
		}
		m.Version, err = a.Version(ctx, bin)
		if err != nil {
			return err
		}
		m.BinarySHA, err = fileDigest(bin)
		if err != nil {
			return err
		}
		cfg, err := mihomo.Default(m.Controller, m.Secret, m.Port)
		if err != nil {
			return err
		}
		target, err := a.writeGeneration(stage, m, cfg)
		if err != nil {
			return err
		}
		if err := store.Dir(filepath.Join(stage, "runtime")); err != nil {
			return err
		}
		report("Validating default configuration…")
		if err := a.Validate(ctx, bin, filepath.Join(stage, target, "config.yaml"), filepath.Join(stage, "runtime")); err != nil {
			return err
		}
		if err := store.Switch(filepath.Join(stage, "current"), target, ""); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		tree, err := installationTree(stage)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(installJournal{manifest: m, Tree: tree})
		journal := filepath.Join(a.Home, "install-pending.json")
		if err := store.Replace(journal, raw, nil, 0600); err != nil {
			return err
		}
		if err := os.Rename(stage, final); err != nil {
			return errors.Join(err, os.Remove(journal))
		}
		installed := false
		defer func() {
			if !installed {
				c, cancel := cleanupContext()
				defer cancel()
				if err := a.recoverInstall(c); err != nil {
					result = errors.Join(result, fmt.Errorf("installation recovery remains pending: %w", err))
				}
			}
		}()
		if err := store.SyncDir(a.Home); err != nil {
			return err
		}
		actualBin, config, data := a.paths()
		report("Installing systemd user service…")
		if err := a.Service.Install(ctx, actualBin, config, data); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.Remove(journal); err != nil {
			return err
		}
		installed = true
		report("Mihomo installed. Start the service to view PROXY nodes.")
		return nil
	})
}
func (a *App) source(ctx context.Context, source string) ([]byte, error) {
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		req, err := http.NewRequestWithContext(ctx, "GET", source, nil)
		if err != nil {
			return nil, errors.New("invalid subscription URL")
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.Proxy = nil
		defer tr.CloseIdleConnections()
		client := &http.Client{Timeout: 45 * time.Second, Transport: tr}
		if a.HTTP != nil {
			client = a.HTTP
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("subscription download failed (URL withheld)")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("subscription download returned HTTP %d", resp.StatusCode)
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, mihomo.MaxConfigSize+1))
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("cannot read subscription response")
		}
		if len(raw) > mihomo.MaxConfigSize {
			return nil, errors.New("subscription exceeds 8 MiB")
		}
		return raw, nil
	}
	if !filepath.IsAbs(source) {
		return nil, errors.New("subscription source must be an HTTP(S) URL or absolute file path")
	}
	raw, err := store.Read(source)
	if err != nil {
		return nil, errors.New("cannot read subscription file")
	}
	return raw, nil
}
func (a *App) validateConfig(ctx context.Context, m manifest, config []byte) error {
	stage, err := os.MkdirTemp(a.Home, ".validate-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	path := filepath.Join(stage, "config.yaml")
	if err := store.Replace(path, config, nil, 0600); err != nil {
		return err
	}
	bin, _, _ := a.paths()
	return a.Validate(ctx, bin, path, stage)
}
func (a *App) Add(ctx context.Context, name, source string) error {
	if !profileName.MatchString(name) || name == "default" {
		return errors.New("profile name must use 1–48 letters, digits, _ or -; default is reserved")
	}
	return a.locked(ctx, func() error {
		m, _, err := a.load()
		if err != nil {
			return err
		}
		profiles := filepath.Join(a.Home, "profiles")
		if err := store.Dir(profiles); err != nil {
			return err
		}
		path := filepath.Join(profiles, name+".yaml")
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errors.New("profile already exists; choose a new name")
		}
		raw, err := a.source(ctx, source)
		if err != nil {
			return err
		}
		effective, err := mihomo.Effective(raw, m.Controller, m.Secret, m.Port)
		if err != nil {
			return err
		}
		if err := a.validateConfig(ctx, m, effective); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return store.Replace(path, raw, nil, 0600)
	})
}
func (a *App) Apply(ctx context.Context, name string) error {
	if !profileName.MatchString(name) {
		return errors.New("invalid profile name")
	}
	return a.locked(ctx, func() error {
		if err := a.requireService(); err != nil {
			return err
		}
		m, old, err := a.load()
		if err != nil {
			return err
		}
		bin, config, data := a.paths()
		if err := a.Service.Verify(ctx, bin, config, data); err != nil {
			return err
		}
		status, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		var cfg []byte
		if name == "default" {
			cfg, err = mihomo.Default(m.Controller, m.Secret, m.Port)
		} else {
			if err := store.Dir(filepath.Join(a.Home, "profiles")); err != nil {
				return err
			}
			raw, e := store.Read(filepath.Join(a.Home, "profiles", name+".yaml"))
			if e != nil {
				return e
			}
			cfg, err = mihomo.Effective(raw, m.Controller, m.Secret, m.Port)
		}
		if err != nil {
			return err
		}
		if err := a.validateConfig(ctx, m, cfg); err != nil {
			return err
		}
		m.Applied = name
		base := filepath.Join(a.Home, "installation")
		next, err := a.writeGeneration(base, m, cfg)
		if err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				_ = os.RemoveAll(filepath.Join(base, next))
			}
		}()
		if err := ctx.Err(); err != nil {
			return err
		}
		p := pending{Old: old, New: next, Active: status.Active}
		raw, _ := json.Marshal(p)
		journal := filepath.Join(a.Home, "apply-pending.json")
		if err := store.Replace(journal, raw, nil, 0600); err != nil {
			return err
		}
		rollback := func(cause error) error {
			c, cancel := cleanupContext()
			defer cancel()
			err := a.recover(c)
			if err != nil {
				committed = true
			}
			return errors.Join(cause, err)
		}
		if err := store.Switch(filepath.Join(base, "current"), next, old); err != nil {
			return rollback(err)
		}
		if status.Active {
			if err := a.restartReady(ctx, m); err != nil {
				return rollback(err)
			}
		}
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		if err := os.Remove(journal); err != nil {
			return rollback(err)
		}
		committed = true
		return nil
	})
}
func (a *App) waitReady(ctx context.Context, m manifest) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Mihomo API did not become ready: %w", err)
		}
		if a.client(m).Ping(ctx) == nil {
			status, err := a.Service.Inspect(ctx)
			if err == nil && status.Active {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Mihomo API did not become ready: %w", ctx.Err())
		case <-tick.C:
		}
	}
}
func (a *App) restartReady(ctx context.Context, m manifest) error {
	bin, config, data := a.paths()
	if err := a.Service.Verify(ctx, bin, config, data); err != nil {
		return err
	}
	if err := a.Service.Action(ctx, "restart"); err != nil {
		return err
	}
	return a.waitReady(ctx, m)
}
func (a *App) Control(ctx context.Context, action string) error {
	if action != "start" && action != "stop" && action != "restart" {
		return errors.New("unsupported service action")
	}
	return a.locked(ctx, func() error {
		if err := a.requireService(); err != nil {
			return err
		}
		m, _, err := a.load()
		if err != nil {
			return err
		}
		bin, config, data := a.paths()
		if err := a.Service.Verify(ctx, bin, config, data); err != nil {
			return err
		}
		status, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		if action != "stop" && !status.Active {
			if err := a.checkPorts(m); err != nil {
				return err
			}
		}
		if err := a.Service.Action(ctx, action); err != nil {
			// systemctl can be cancelled after its job was accepted. Restore the
			// stopped state with a fresh context rather than leaving a new daemon.
			if action != "stop" && !status.Active {
				c, cancel := cleanupContext()
				defer cancel()
				return errors.Join(err, a.Service.Action(c, "stop"))
			}
			return err
		}
		if action == "stop" {
			s, err := a.Service.Inspect(ctx)
			if err != nil {
				return err
			}
			if s.Active {
				return errors.New("service is still active")
			}
			return nil
		}
		if err := a.waitReady(ctx, m); err != nil {
			if !status.Active {
				c, cancel := cleanupContext()
				defer cancel()
				return errors.Join(err, a.Service.Action(c, "stop"))
			}
			return err
		}
		return nil
	})
}
func (a *App) Select(ctx context.Context, group, node string) error {
	return a.locked(ctx, func() error {
		if err := a.requireService(); err != nil {
			return err
		}
		m, _, err := a.load()
		if err != nil {
			return err
		}
		bin, config, data := a.paths()
		if err := a.Service.Verify(ctx, bin, config, data); err != nil {
			return err
		}
		return a.client(m).Select(ctx, group, node)
	})
}
func (a *App) Port(ctx context.Context) (int, error) {
	var port int
	err := a.locked(ctx, func() error { m, _, err := a.load(); port = m.Port; return err })
	return port, err
}
