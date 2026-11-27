package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"syscall"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"
	"golang.org/x/sys/unix"
)

// Before records every original alias in a family being replaced. An omitted
// alias was absent, which is also part of the rollback state.
type geoTransition struct {
	Untracked map[string]bool   `json:"untracked,omitempty"` // Old resources with no source record.
	Kinds     map[string]bool   `json:"kinds"`
	Before    map[string]string `json:"before"`
	Ready     bool              `json:"ready,omitempty"`
	Committed bool              `json:"committed,omitempty"`
}

func planGeoTransition(oldSources map[string]string, c *geoCandidate) *geoTransition {
	kinds := make(map[string]bool)
	for kind, source := range c.Sources {
		if source != oldSources[kind] {
			kinds[kind] = true
		}
	}
	for name, hash := range c.Before {
		if hash != c.After[name] {
			kinds[geoFileKind(name)] = true
		}
	}
	for name, hash := range c.After {
		if hash != c.Before[name] {
			kinds[geoFileKind(name)] = true
		}
	}
	if len(kinds) == 0 {
		return nil
	}
	g := &geoTransition{Kinds: kinds, Before: selectedGeoFiles(c.Before, kinds)}
	for name := range g.Before {
		kind := geoFileKind(name)
		if oldSources[kind] == "" {
			if g.Untracked == nil {
				g.Untracked = make(map[string]bool)
			}
			g.Untracked[kind] = true
		}
	}
	return g
}

func selectedGeoFiles(files map[string]string, kinds map[string]bool) map[string]string {
	result := make(map[string]string)
	for name, hash := range files {
		if kinds[geoFileKind(name)] {
			result[name] = hash
		}
	}
	return result
}

func (g *geoTransition) valid() bool {
	if len(g.Kinds) == 0 || g.Committed && !g.Ready {
		return false
	}
	for kind, enabled := range g.Kinds {
		if !enabled || kind != "mmdb" && kind != "geoip" && kind != "geosite" && kind != "asn" {
			return false
		}
	}
	for kind, untracked := range g.Untracked {
		if !untracked || !g.Kinds[kind] {
			return false
		}
	}
	for name, hash := range g.Before {
		b, err := hex.DecodeString(hash)
		if err != nil || len(b) != sha256.Size || !g.Kinds[geoFileKind(name)] {
			return false
		}
	}
	return true
}

// If current points to a regular file, current/config.yaml resolves to ENOTDIR.
// This durably fences systemd restarts using the original, verified ExecStart
// on both scopes, even if power fails between two database renames.
func geoBarrier(old string) string { return filepath.Join(old, "config.yaml") }

func (a *App) geoUndoDirectory(p pending) string {
	prefix := ".geo-undo-"
	if len(p.Geo.Untracked) != 0 {
		prefix = "geodata-backup-"
	}
	return filepath.Join(a.Home, prefix+filepath.Base(p.New))
}

func (a *App) prepareProfileCandidate(ctx context.Context, profile string, m manifest, cfg []byte) (*geoCandidate, error) {
	bin, _, _ := a.paths()
	candidate, err := a.prepareGeoCandidate(ctx, bin, m, cfg)
	if err != nil {
		return nil, diagnostic.New("diagnostic.profile.validate", "profile %s failed core validation: %w", profile, err)
	}
	return candidate, nil
}

func (a *App) publishPreparedGeo(ctx context.Context, old string, m manifest, cfg []byte, status service.Status, candidate *geoCandidate) error {
	return a.publishProfileGeneration(ctx, old, m, cfg, status, false, candidate)
}

func (a *App) saveGeoJournal(p *pending, raw *[]byte) error {
	next, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := store.Replace(filepath.Join(a.Home, "apply-pending.json"), next, *raw, 0600); err != nil {
		return err
	}
	*raw = next
	return nil
}

func (a *App) finishGeoJournal(p pending, raw []byte) error {
	dir := a.geoUndoDirectory(p)
	if p.Geo.Committed && len(p.Geo.Untracked) != 0 {
		if err := existingDir(dir); err != nil {
			return err
		}
		if a.systemService() != nil {
			if err := trustedRootAncestors(dir); err != nil {
				return err
			}
		}
		// Keep manually placed databases whose provenance we could not establish.
		// The deterministic private archive is outside Mihomo's discovery path.
		for name := range p.Geo.Before {
			if !p.Geo.Untracked[geoFileKind(name)] {
				if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
		if err := store.SyncDir(dir); err != nil {
			return err
		}
	} else if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return store.Remove(filepath.Join(a.Home, "apply-pending.json"), raw)
}

func (a *App) installGeoCandidate(ctx context.Context, current string, p *pending, raw *[]byte, c *geoCandidate) error {
	if err := store.Switch(current, geoBarrier(p.Old), p.Old); err != nil {
		return err
	}
	// A stop job also drains a start/automatic restart already queued before
	// the barrier was published. It does not change the user's desired state.
	if err := a.stopGeoService(ctx); err != nil {
		return err
	}
	_, _, runtime := a.paths()
	snapshot, err := hashGeoFiles(ctx, runtime)
	if err != nil {
		return err
	}
	if !maps.Equal(snapshot, c.Before) {
		return errors.New("GEO data changed during configuration validation; retry with the updated files")
	}
	undo := a.geoUndoDirectory(*p)
	if err := os.Mkdir(undo, 0700); err != nil {
		return err
	}
	if err := store.SyncDir(a.Home); err != nil {
		return err
	}
	for name, hash := range p.Geo.Before {
		if err := copyGeoForSwitch(ctx, runtime, undo, undo, name, hash, -1, -1); err != nil {
			return err
		}
	}
	if err := store.SyncDir(undo); err != nil {
		return err
	}
	p.Geo.Ready = true
	if err := a.saveGeoJournal(p, raw); err != nil {
		return err
	}
	return a.placeGeoFiles(ctx, undo, c.Data, selectedGeoFiles(c.After, p.Geo.Kinds), p.Geo.Kinds)
}

// This runs with current fenced and the service stopped. Staging files live in
// the manager's private backup directory, outside the service-writable runtime.
func (a *App) placeGeoFiles(ctx context.Context, undo, source string, target map[string]string, kinds map[string]bool) error {
	_, _, runtime := a.paths()
	uid, gid := -1, -1
	if s := a.systemService(); s != nil {
		var err error
		uid, gid, err = s.Identity()
		if err != nil {
			return err
		}
	}
	files, err := os.ReadDir(runtime)
	if err != nil {
		return err
	}
	for _, entry := range files {
		name := entry.Name()
		if !kinds[geoFileKind(name)] || target[name] != "" {
			continue
		}
		if !entry.Type().IsRegular() {
			return errors.New("refusing to remove a non-regular GEO database")
		}
		if err := os.Remove(filepath.Join(runtime, name)); err != nil {
			return err
		}
	}
	for name, hash := range target {
		if err := copyGeoForSwitch(ctx, source, undo, runtime, name, hash, uid, gid); err != nil {
			return err
		}
	}
	return store.SyncDir(runtime)
}

func copyGeoForSwitch(ctx context.Context, srcDir, stageDir, dstDir, name, wantHash string, uid, gid int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if geoFileKind(name) == "" {
		return errors.New("unexpected GEO database filename")
	}
	src, err := os.OpenFile(filepath.Join(srcDir, name), os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	const limit = 256 << 20
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || st.Nlink != 1 || info.Size() > limit {
		return errors.New("unsafe GEO database file")
	}
	tmp, err := os.CreateTemp(stageDir, ".copy-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, sum), io.LimitReader(geoContextReader{ctx, src}, limit+1))
	if err != nil {
		return err
	}
	if n != info.Size() || hex.EncodeToString(sum.Sum(nil)) != wantHash {
		return errors.New("GEO database changed while copying; original data was preserved")
	}
	if uid >= 0 {
		if err := tmp.Chown(uid, gid); err != nil {
			return err
		}
	}
	if err := os.Chtimes(tmp.Name(), info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dstDir, name))
}

// Once the invalid -f pointer is durable, a future start cannot see the old
// configuration. Only an already running or transitioning unit needs a stop job.
func (a *App) stopGeoService(ctx context.Context) error {
	status, err := a.Service.Inspect(ctx)
	if err != nil {
		return err
	}
	if !status.Active && status.PID == 0 && (status.State == "inactive" || status.State == "failed") {
		return nil
	}
	return a.Service.Action(ctx, "stop")
}

// Called only after both configuration generations and the loaded unit have
// been verified. Rollback remains repeatable until the original pointer is
// durable; ready=false means the forward operation never touched a database.
func (a *App) restoreGeo(ctx context.Context, p pending, current, target string) error {
	if target == p.Old {
		return nil
	}
	barrier := geoBarrier(p.Old)
	if target == p.New {
		if !p.Geo.Ready {
			return errors.New("GEO rollback backup was not committed")
		}
		if err := store.Switch(current, barrier, target); err != nil {
			return err
		}
	}
	if err := a.stopGeoService(ctx); err != nil {
		return err
	}
	if p.Geo.Ready {
		undo := a.geoUndoDirectory(p)
		if err := existingDir(undo); err != nil {
			return err
		}
		if a.systemService() != nil {
			if err := trustedRootAncestors(undo); err != nil {
				return err
			}
		}
		before, err := hashGeoFiles(ctx, undo)
		if err != nil {
			return err
		}
		if !maps.Equal(before, p.Geo.Before) {
			return errors.New("GEO rollback backup changed; preserving files for inspection")
		}
		if err := a.placeGeoFiles(ctx, undo, undo, p.Geo.Before, p.Geo.Kinds); err != nil {
			return err
		}
	}
	return store.Switch(current, p.Old, barrier)
}
