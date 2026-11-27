package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/oschwald/maxminddb-golang"
	"golang.org/x/sys/unix"
)

type geoCandidate struct {
	Data          string
	Before, After map[string]string // Runtime filenames, including case and MMDB aliases, to content hashes.
	Sources       map[string]string // Logical database type to source identity.
	stage         string
}

func (c *geoCandidate) Close() {
	if c != nil {
		_ = os.RemoveAll(c.stage)
	}
}

func geoFileKind(name string) string {
	switch {
	case strings.EqualFold(name, "Country.mmdb"), strings.EqualFold(name, "geoip.db"), strings.EqualFold(name, "geoip.metadb"):
		return "mmdb"
	case strings.EqualFold(name, "GeoIP.dat"):
		return "geoip"
	case strings.EqualFold(name, "GeoSite.dat"):
		return "geosite"
	case strings.EqualFold(name, "ASN.mmdb"):
		return "asn"
	}
	return ""
}

const geoStagePrefix = ".validate-geo-"
const geoStageOwner = "lazyclash isolated validation v1\n"

// The management lock excludes ongoing candidate preparation. An interrupted
// validator's data never participates in recovery of a published generation.
func (a *App) cleanGeoCandidates() error {
	items, err := os.ReadDir(a.Home)
	if err != nil {
		return err
	}
	for _, item := range items {
		if !item.IsDir() || !strings.HasPrefix(item.Name(), geoStagePrefix) {
			continue
		}
		dir := filepath.Join(a.Home, item.Name())
		f, err := os.OpenFile(filepath.Join(dir, "owner"), os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(geoStageOwner)) {
			f.Close()
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(f, int64(len(geoStageOwner))+1))
		f.Close()
		if err == nil && string(raw) == geoStageOwner {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
		}
	}
	return nil
}

// prepareGeoCandidate validates once against private bytes. Its caller either
// discards them (for a preview) or publishes precisely the files -t used.
func (a *App) prepareGeoCandidate(ctx context.Context, bin string, m manifest, cfg []byte) (*geoCandidate, error) {
	sources, err := mihomo.GeoSources(cfg)
	if err != nil {
		return nil, err
	}
	_, _, live := a.paths()
	before, err := hashGeoFiles(ctx, live)
	if err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(a.Home, geoStagePrefix)
	if err != nil {
		return nil, err
	}
	c := &geoCandidate{stage: stage, Data: filepath.Join(stage, "data"), Before: before, Sources: sources}
	ready := false
	defer func() {
		if !ready {
			c.Close()
		}
	}()
	if err := os.WriteFile(filepath.Join(stage, "owner"), []byte(geoStageOwner), 0600); err != nil {
		return nil, err
	}
	config := filepath.Join(stage, "config.yaml")
	if err := os.WriteFile(config, cfg, 0600); err != nil {
		return nil, err
	}
	if err := os.Mkdir(c.Data, 0700); err != nil {
		return nil, err
	}
	if err := a.systemFile(config, 0640); err != nil {
		return nil, err
	}
	if err := a.systemRuntime(c.Data); err != nil {
		return nil, err
	}
	if err := a.systemDirectory(stage); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(live)
	if err != nil {
		return nil, err
	}
	var paths []string
	inherited := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if kind := geoFileKind(name); kind != "" {
			if m.GeoSources[kind] != sources[kind] {
				continue
			}
			inherited[name] = before[name]
		}
		// The profile cache is process-owned and may be mid-write. Sockets and
		// other device nodes are not inputs to a configuration parse.
		if name == "cache.db" || name == ".cache" || !(entry.Type().IsRegular() || entry.IsDir() || entry.Type() == os.ModeSymlink) {
			continue
		}
		paths = append(paths, filepath.Join(live, name))
	}
	probe, err := a.executionContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := mihomo.CopyValidationAssets(probe, paths, c.Data); err != nil {
		return nil, err
	}
	if err := checkValidationLinks(c.Data); err != nil {
		return nil, err
	}
	copied, err := hashGeoFiles(ctx, c.Data)
	if err != nil {
		return nil, err
	}
	if !maps.Equal(copied, inherited) {
		return nil, diagnostic.New("diagnostic.geo.changed", "GEO data changed during validation; retry the operation")
	}
	// A corrupt unused database must not block MATCH-only profiles. Let -t
	// reacquire a corrupt inherited MMDB only when the candidate needs it.
	for name := range copied {
		if kind := geoFileKind(name); kind == "mmdb" || kind == "asn" {
			_, err := inspectGeoFile(ctx, c.Data, name, true)
			if errors.Is(err, errInvalidGeoMMDB) {
				err = os.Remove(filepath.Join(c.Data, name))
			}
			if err != nil {
				return nil, err
			}
		}
	}
	if err := a.Validate(probe, bin, config, c.Data); err != nil {
		return nil, err
	}
	c.After, err = inspectGeoFiles(ctx, c.Data, true)
	if err != nil {
		return nil, err
	}
	ready = true
	return c, nil
}

// cp -P preserves relative local assets. An absolute link could point back to
// the running instance. Use Root.Stat to check the complete chain, including
// linked parent directories, before allowing the core to follow it.
func checkValidationLinks(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type() == os.ModeSymlink {
			if _, err := root.Stat(path); err != nil {
				return diagnostic.New("diagnostic.geo.assetLink", "a runtime asset link cannot be resolved inside the isolated validation directory")
			}
		}
		return nil
	})
}

func hashGeoFiles(ctx context.Context, data string) (map[string]string, error) {
	return inspectGeoFiles(ctx, data, false)
}

func inspectGeoFiles(ctx context.Context, data string, verify bool) (map[string]string, error) {
	root, err := os.OpenRoot(data)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	hashes := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		kind := geoFileKind(name)
		if kind == "" {
			continue
		}
		hash, err := inspectGeoFile(ctx, data, name, verify && (kind == "mmdb" || kind == "asn"))
		if err != nil {
			return nil, err
		}
		hashes[name] = hash
	}
	return hashes, ctx.Err()
}

var errInvalidGeoMMDB = diagnostic.New("diagnostic.geo.mmdb", "invalid or oversized GEO MaxMind database")

// GEO files are direct children of a directory anchored by the private
// installation or stage parent. Reject the final symlink at open time.
func inspectGeoFile(ctx context.Context, dir, name string, verifyMMDB bool) (string, error) {
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", diagnostic.New("diagnostic.geo.file", "GEO assets must be regular files without links")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 {
		return "", diagnostic.New("diagnostic.geo.file", "GEO assets must be regular files without links")
	}
	h := sha256.New()
	reader := geoContextReader{ctx, f}
	if verifyMMDB {
		// The core may report success after downloading an invalid MMDB on
		// its second initialization attempt. Open these bytes independently.
		const limit = 256 << 20
		if info.Size() > limit {
			return "", errInvalidGeoMMDB
		}
		raw, err := io.ReadAll(io.LimitReader(io.TeeReader(reader, h), limit+1))
		if err != nil {
			return "", err
		}
		if len(raw) > limit {
			return "", errInvalidGeoMMDB
		}
		db, err := maxminddb.FromBytes(raw)
		if err != nil {
			return "", errInvalidGeoMMDB
		}
		db.Close()
	} else if _, err := io.Copy(h, reader); err != nil {
		return "", err
	}
	now, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !now.ModTime().Equal(info.ModTime()) || now.Size() != info.Size() {
		return "", diagnostic.New("diagnostic.geo.changed", "GEO data changed during validation; retry the operation")
	}
	return hex.EncodeToString(h.Sum(nil)), ctx.Err()
}

type geoContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r geoContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
