package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
	"golang.org/x/sys/unix"
)

type InstallPlan struct {
	Release         mihomo.Release
	Local           string
	ActualPath      string
	ExistingVersion string
	RunningVersion  string
	ServiceActive   bool
	Operation       string
	FreeBytes       uint64
	DiskKnown       bool
	Warnings        []string
	BlockedReason   string
	guard, localSHA string
	seal            string
	reinstall       bool
}

type InstallResult struct {
	InstalledVersion string
	RunningVersion   string
	Operation        string
	ActualPath       string
	RestartRequired  bool
}

func (a *App) ResolveRelease(ctx context.Context, selection string, cfg downloadnet.Config) (mihomo.Release, error) {
	if selection == "" {
		selection = "latest"
	}
	if a.Resolve != nil {
		return a.Resolve(ctx, selection, cfg)
	}
	return mihomo.Resolve(ctx, selection, cfg)
}

func (a *App) TestReleaseFile(ctx context.Context, release mihomo.Release, cfg downloadnet.Config) error {
	if a.TestFile != nil {
		return a.TestFile(ctx, release, cfg)
	}
	return mihomo.TestFile(ctx, release, cfg)
}

func (a *App) PrepareInstall(ctx context.Context, release mihomo.Release, local string, reinstall bool) (InstallPlan, error) {
	var plan InstallPlan
	err := a.locked(ctx, func() error {
		var err error
		plan, err = a.prepareInstallLocked(ctx, release, local, reinstall)
		return err
	})
	return plan, err
}

func (a *App) prepareInstallLocked(ctx context.Context, release mihomo.Release, local string, reinstall bool) (InstallPlan, error) {
	bin, config, data := a.paths()
	p := InstallPlan{Release: release, Local: local, ActualPath: bin, Operation: "install", reinstall: reinstall}
	if local != "" {
		if !filepath.IsAbs(local) {
			return p, errors.New("local Mihomo path must be absolute")
		}
		info, err := os.Stat(local)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 256<<20 {
			return p, errors.New("local Mihomo must be a readable regular file at most 256 MiB")
		}
		version, err := a.Version(ctx, local)
		if err != nil {
			return p, err
		}
		p.Release = mihomo.Release{Version: version, Asset: filepath.Base(local), ChecksumSource: "local binary", Size: info.Size(), Platform: runtime.GOOS + "/" + runtime.GOARCH}
		p.localSHA, err = fileDigest(local)
		if err != nil {
			return p, err
		}
	}
	if p.Release.Version == "" {
		return p, errors.New("resolve a Mihomo version before preparing installation")
	}
	if free, err := a.freeSpace(a.Home); err == nil {
		p.FreeBytes, p.DiskKnown = free, true
	} else {
		p.Warnings = append(p.Warnings, "Free disk space could not be determined")
	}
	block := func(err error) {
		if err != nil && p.BlockedReason == "" {
			p.BlockedReason = err.Error()
		}
	}
	block(a.requireService())
	if a.Service != nil {
		block(a.Service.Check(ctx))
	}
	final := filepath.Join(a.Home, "installation")
	if _, err := os.Lstat(final); errors.Is(err, os.ErrNotExist) {
		p.guard = "absent"
		if a.Service != nil {
			block(a.Service.Available(ctx))
		}
		block(a.checkPorts(manifest{Controller: "127.0.0.1:9097", Port: 7897}))
	} else if err != nil {
		block(err)
	} else {
		m, target, err := a.load()
		if err != nil {
			block(err)
		} else {
			p.ExistingVersion = m.Version
			p.Operation = "update"
			if m.Version == p.Release.Version {
				p.Operation = "use existing"
				if reinstall {
					p.Operation = "reinstall"
				}
			}
			state := "unsupported"
			if a.Service != nil {
				block(a.Service.Verify(ctx, bin, config, data))
				status, err := a.Service.Inspect(ctx)
				block(err)
				state = status.State
				p.ServiceActive = status.Active
				if err == nil && status.Active && p.BlockedReason == "" {
					p.RunningVersion, err = a.client(m).Version(ctx)
					if err != nil {
						p.Warnings = append(p.Warnings, "Running service version is unavailable; the running process will be left untouched")
					}
				}
			}
			raw, _ := json.Marshal([]string{target, m.ID, m.BinarySHA, m.SHA, m.Version, state, p.RunningVersion})
			p.guard = digest(raw)
			if p.Operation != "use existing" && state == "active" {
				p.Warnings = append(p.Warnings, "The running Mihomo process will keep its current binary until an explicit restart")
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	if p.DiskKnown && p.Operation != "use existing" {
		need := uint64(128 << 20)
		if p.Operation == "install" {
			need *= 2 // downloaded staging plus the complete first-install tree
		}
		if p.Release.Size > 0 {
			need += uint64(p.Release.Size)
		}
		if p.FreeBytes < need {
			block(errors.New("insufficient free disk space for staging and recovery"))
		}
	}
	probeDirs := []string{a.Home}
	if info, err := os.Lstat(filepath.Dir(bin)); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		probeDirs = append(probeDirs, filepath.Dir(bin))
	}
	for _, dir := range probeDirs {
		f, err := os.CreateTemp(dir, ".lazyclash-preflight-*")
		if err != nil {
			block(errors.New("installation destination is not writable"))
			continue
		}
		if err := errors.Join(f.Close(), os.Remove(f.Name())); err != nil {
			block(errors.New("installation destination write probe could not be cleaned up"))
		}
	}
	p.seal = planSeal(p)
	return p, nil
}

func (a *App) freeSpace(path string) (uint64, error) {
	if a.DiskSpace != nil {
		return a.DiskSpace(path)
	}
	var disk unix.Statfs_t
	if err := unix.Statfs(path, &disk); err != nil {
		return 0, err
	}
	return uint64(disk.Bavail) * uint64(disk.Bsize), nil
}

func planSeal(p InstallPlan) string {
	raw, _ := json.Marshal(p)
	return digest(append(raw, []byte(p.guard+"\x00"+p.localSHA+fmt.Sprint(p.reinstall))...))
}

// InstallCore never stops or restarts a running service. A plan is an immutable
// review result; it is checked again under the same lock used for publication.
func (a *App) InstallCore(ctx context.Context, plan InstallPlan, cfg downloadnet.Config, progress func(mihomo.Progress)) (InstallResult, error) {
	var result InstallResult
	err := a.locked(ctx, func() error {
		if plan.seal == "" || plan.seal != planSeal(plan) {
			return errors.New("installation plan was changed; prepare it again")
		}
		fresh, err := a.prepareInstallLocked(ctx, plan.Release, plan.Local, plan.reinstall)
		if err != nil {
			return err
		}
		if fresh.guard != plan.guard || fresh.localSHA != plan.localSHA || fresh.Operation != plan.Operation {
			return errors.New("installation changed since preflight; prepare installation again")
		}
		if fresh.BlockedReason != "" {
			return errors.New(fresh.BlockedReason)
		}
		result = InstallResult{InstalledVersion: fresh.Release.Version, RunningVersion: fresh.RunningVersion, Operation: fresh.Operation, ActualPath: fresh.ActualPath, RestartRequired: fresh.ServiceActive && fresh.RunningVersion != fresh.Release.Version}
		if fresh.Operation == "use existing" {
			return nil
		}
		report := func(stage, detail string) {
			if progress != nil {
				progress(mihomo.Progress{Stage: stage, Detail: detail})
			}
		}
		stage, err := os.MkdirTemp(a.Home, ".core-stage-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		newBin := filepath.Join(stage, "mihomo")
		if fresh.Local != "" {
			report("download", "Copying the selected local Mihomo binary")
			err = copyBinary(ctx, fresh.Local, newBin)
		} else if a.DownloadRelease != nil {
			err = a.DownloadRelease(ctx, newBin, fresh.Release, cfg, progress)
		} else {
			err = mihomo.DownloadRelease(ctx, newBin, fresh.Release, cfg, progress)
		}
		if err != nil {
			return err
		}
		if fresh.Local != "" {
			copiedSHA, err := fileDigest(newBin)
			if err != nil || copiedSHA != fresh.localSHA {
				return errors.New("local Mihomo changed during staging; prepare installation again")
			}
		}
		report("verify", "Verifying the staged Mihomo executable")
		version, err := a.Version(ctx, newBin)
		if err != nil {
			return err
		}
		if version != fresh.Release.Version {
			return errors.New("staged Mihomo version differs from the reviewed version")
		}
		report("deploy", "Validating configuration and committing the staged installation")
		if fresh.Operation == "install" {
			err = a.installFreshLocked(ctx, newBin, func(detail string) { report("deploy", detail) })
		} else {
			err = a.replaceCoreLocked(ctx, newBin, version)
		}
		if err != nil {
			return err
		}
		result.RestartRequired = fresh.ServiceActive
		report("done", "Mihomo binary is installed; service state was preserved")
		return nil
	})
	return result, err
}

type coreJournal struct {
	Old, New, Backup                          string
	OldBinarySHA, NewBinarySHA                string
	OldManifestSHA, NewManifestSHA, ConfigSHA string
	Service, ID                               string
	Committed                                 bool
}

func (a *App) replaceCoreLocked(ctx context.Context, newBin, version string) (result error) {
	m, old, err := a.load()
	if err != nil {
		return err
	}
	bin, config, data := a.paths()
	if err := a.Service.Verify(ctx, bin, config, data); err != nil {
		return err
	}
	base := filepath.Join(a.Home, "installation")
	cfg, err := store.Read(config)
	if err != nil {
		return err
	}
	validateDir, err := os.MkdirTemp(a.Home, ".core-validate-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(validateDir)
	if err := a.Validate(ctx, newBin, config, validateDir); err != nil {
		return err
	}
	oldSHA := m.BinarySHA
	m.BinarySHA, err = fileDigest(newBin)
	if err != nil {
		return err
	}
	m.Version = version
	next, err := a.writeGeneration(base, m, cfg)
	if err != nil {
		return err
	}
	oldManifest, err := store.Read(filepath.Join(base, old, "manifest.json"))
	if err != nil {
		return err
	}
	newManifest, err := store.Read(filepath.Join(base, next, "manifest.json"))
	if err != nil {
		return err
	}
	j := coreJournal{Old: old, New: next, Backup: "core-backup-" + randomID(), OldBinarySHA: oldSHA, NewBinarySHA: m.BinarySHA, OldManifestSHA: digest(oldManifest), NewManifestSHA: digest(newManifest), ConfigSHA: digest(cfg), Service: a.Name, ID: m.ID}
	backup := filepath.Join(base, j.Backup)
	if err := os.Link(bin, backup); err != nil {
		return err
	}
	journal := filepath.Join(a.Home, "core-pending.json")
	raw, _ := json.Marshal(j)
	if err := store.SyncDir(base); err != nil {
		return errors.Join(err, os.Remove(backup))
	}
	if err := store.Replace(journal, raw, nil, 0600); err != nil {
		return errors.Join(err, os.Remove(backup))
	}
	committed := false
	defer func() {
		if !committed {
			c, cancel := cleanupContext()
			defer cancel()
			result = errors.Join(result, a.recoverCore(c))
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(newBin, bin); err != nil {
		return err
	}
	if err := store.SyncDir(base); err != nil {
		return err
	}
	if err := store.Switch(filepath.Join(base, "current"), next, old); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	j.Committed = true
	complete, _ := json.Marshal(j)
	if err := store.Replace(journal, complete, raw, 0600); err != nil {
		return err
	}
	committed = true
	if err := a.recoverCore(ctx); err != nil {
		return fmt.Errorf("new binary installed; recovery cleanup remains pending: %w", err)
	}
	return nil
}

func (a *App) recoverCore(ctx context.Context) error {
	journal := filepath.Join(a.Home, "core-pending.json")
	raw, err := store.Read(journal)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var j coreJournal
	if json.Unmarshal(raw, &j) != nil || !generationName.MatchString(j.Old) || !generationName.MatchString(j.New) || j.Old == j.New || !regexpCoreBackup(j.Backup) || j.Service != a.Name || j.ID == "" {
		return errors.New("invalid core recovery journal; preserving installation")
	}
	base := filepath.Join(a.Home, "installation")
	if err := existingDir(base); err != nil {
		return err
	}
	for _, dir := range []string{"generations", "runtime"} {
		if err := existingDir(filepath.Join(base, dir)); err != nil {
			return err
		}
	}
	for _, gen := range []struct{ target, manifestSHA, binarySHA string }{{j.Old, j.OldManifestSHA, j.OldBinarySHA}, {j.New, j.NewManifestSHA, j.NewBinarySHA}} {
		if err := existingDir(filepath.Join(base, gen.target)); err != nil {
			return err
		}
		manifestBytes, err := store.Read(filepath.Join(base, gen.target, "manifest.json"))
		if err != nil || digest(manifestBytes) != gen.manifestSHA {
			return errors.New("core recovery metadata changed; preserving installation")
		}
		var m manifest
		if json.Unmarshal(manifestBytes, &m) != nil || m.ID != j.ID || m.Service != j.Service || m.BinarySHA != gen.binarySHA || m.SHA != j.ConfigSHA || m.Version == "" {
			return errors.New("core recovery metadata does not match its transaction; preserving installation")
		}
		cfg, err := store.Read(filepath.Join(base, gen.target, "config.yaml"))
		if err != nil || digest(cfg) != j.ConfigSHA {
			return errors.New("core recovery configuration changed; preserving installation")
		}
	}
	bin, config, data := a.paths()
	if err := a.requireService(); err != nil {
		return err
	}
	if err := a.Service.Verify(ctx, bin, config, data); err != nil {
		return err
	}
	info, err := os.Lstat(bin)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("core recovery binary changed; preserving installation")
	}
	actualSHA, err := fileDigest(bin)
	if err != nil || (actualSHA != j.OldBinarySHA && actualSHA != j.NewBinarySHA) {
		return errors.New("core recovery binary was modified; preserving it")
	}
	current := filepath.Join(base, "current")
	target, err := os.Readlink(current)
	if err != nil || (target != j.Old && target != j.New) {
		return errors.New("core recovery current generation changed; preserving it")
	}
	backup := filepath.Join(base, j.Backup)
	backupExists := false
	var backupInfo os.FileInfo
	if candidate, err := os.Lstat(backup); err == nil {
		if !candidate.Mode().IsRegular() {
			return errors.New("core recovery backup is not a regular file")
		}
		backupSHA, err := fileDigest(backup)
		if err != nil || backupSHA != j.OldBinarySHA {
			return errors.New("core recovery backup was modified; preserving it")
		}
		backupExists = true
		backupInfo = candidate
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if j.Committed {
		if target != j.New || actualSHA != j.NewBinarySHA {
			return errors.New("committed core transaction was modified; preserving installation")
		}
	} else {
		if !backupExists && (actualSHA != j.OldBinarySHA || target != j.Old) {
			return errors.New("core rollback backup is unavailable; preserving installation")
		}
		if backupExists && !os.SameFile(info, backupInfo) {
			restore := filepath.Join(base, ".core-restore-"+randomID())
			if err := os.Link(backup, restore); err != nil {
				return err
			}
			defer os.Remove(restore)
			if err := os.Rename(restore, bin); err != nil {
				return err
			}
		}
		if target != j.Old {
			if err := store.Switch(current, j.Old, j.New); err != nil {
				return err
			}
		}
	}
	if err := store.SyncDir(base); err != nil {
		return err
	}
	if backupExists {
		if err := os.Remove(backup); err != nil {
			return err
		}
		if err := store.SyncDir(base); err != nil {
			return err
		}
	}
	if err := os.Remove(journal); err != nil {
		return err
	}
	if err := store.SyncDir(a.Home); err != nil {
		return err
	}
	if !j.Committed {
		return removeStagedGeneration(filepath.Join(base, j.New), j.NewManifestSHA, j.ConfigSHA)
	}
	return nil
}

func removeStagedGeneration(path, manifestSHA, configSHA string) error {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) != 2 {
		return errors.New("unused core generation contains user files; preserving it")
	}
	for name, hash := range map[string]string{"manifest.json": manifestSHA, "config.yaml": configSHA} {
		raw, err := store.Read(filepath.Join(path, name))
		if err != nil || digest(raw) != hash {
			return errors.New("unused core generation changed; preserving it")
		}
	}
	return os.RemoveAll(path)
}

func regexpCoreBackup(name string) bool {
	return strings.HasPrefix(name, "core-backup-") && len(name) == len("core-backup-")+24 && strings.Trim(name[len("core-backup-"):], "0123456789abcdef") == ""
}
