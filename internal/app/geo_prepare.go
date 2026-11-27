package app

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type GeoPreparation struct {
	Prepared, Present, Skipped, Failed []string `json:",omitempty"`
	Cancelled                          bool     `json:",omitempty"`
	Warning                            string   `json:",omitempty"`
	Diagnostic                         error    `json:"-"`
}

func (p *GeoPreparation) setWarning(err error) {
	if errors.Is(err, context.Canceled) {
		p.Cancelled = true
	}
	if err != nil && !diagnostic.CancellationOnly(err) {
		// Transport errors or caller-supplied paths need not be public.
		p.Diagnostic = diagnostic.New("diagnostic.geo.prepareIncomplete", "default GEO data preparation is incomplete")
		var cause *diagnostic.Error
		if errors.As(err, &cause) {
			switch cause.Code {
			case "diagnostic.geo.requireStopped", "diagnostic.geo.changed", "diagnostic.profile.serviceChanged":
				p.Diagnostic = cause
			}
		}
		p.Warning = p.Diagnostic.Error()
	}
}

// PrepareDefaultGeo fills missing official assets only for a stopped instance.
// It uses the same transaction as profile changes, so a new service cannot
// observe a partial replacement or consume a file validated under another URL.
func (a *App) PrepareDefaultGeo(ctx context.Context, cfg downloadnet.Config, progress func(mihomo.Progress)) (GeoPreparation, error) {
	var result GeoPreparation
	err := a.locked(ctx, func() error {
		var err error
		result, err = a.prepareDefaultGeoLocked(ctx, cfg, progress)
		return err
	})
	result.setWarning(err)
	return result, err
}

var geoSeedFiles = [...]struct{ kind, name string }{{"mmdb", "geoip.metadb"}, {"geosite", "GeoSite.dat"}}

type geoSeedResult struct {
	name    string
	present bool
	skipped bool
	err     error
}

func (a *App) prepareDefaultGeoLocked(ctx context.Context, cfg downloadnet.Config, progress func(mihomo.Progress)) (GeoPreparation, error) {
	var result GeoPreparation
	if progress != nil {
		progress(mihomo.Progress{Stage: "geodata"})
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	m, current, err := a.load()
	if err != nil {
		return result, err
	}
	status, err := a.profileServiceStatus(ctx)
	if err != nil {
		return result, err
	}
	if status.Active || status.PID != 0 || (status.State != "inactive" && status.State != "failed") {
		return result, diagnostic.New("diagnostic.geo.requireStopped", "stop the service before preparing default GEO data")
	}
	config, err := a.readGenerationConfig(current, m.SHA)
	if err != nil {
		return result, err
	}
	sources, err := mihomo.GeoSources(config)
	if err != nil {
		return result, err
	}
	defaults, err := mihomo.GeoSources([]byte("{}"))
	if err != nil {
		return result, err
	}
	_, _, live := a.paths()
	before, err := hashGeoFiles(ctx, live)
	if err != nil {
		return result, err
	}
	stage, err := os.MkdirTemp(a.Home, geoStagePrefix)
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(stage)
	if err := os.WriteFile(filepath.Join(stage, "owner"), []byte(geoStageOwner), 0600); err != nil {
		return result, err
	}
	// Download workers never write into the live runtime. Keep the existing
	// caller progress contract (including CLI writers) single-threaded.
	var progressMu sync.Mutex
	report := func(p mihomo.Progress) {
		if progress != nil {
			progressMu.Lock()
			defer progressMu.Unlock()
			progress(p)
		}
	}
	var outcomes [len(geoSeedFiles)]geoSeedResult
	var workers sync.WaitGroup
	for i, asset := range geoSeedFiles {
		if sources[asset.kind] != defaults[asset.kind] {
			outcomes[i].skipped = true
			continue
		}
		workers.Go(func() {
			outcomes[i] = a.stageGeoSeed(ctx, asset.kind, asset.name, live, stage, before, m.GeoSources[asset.kind] == sources[asset.kind], cfg, report)
		})
	}
	workers.Wait()
	c := &geoCandidate{Data: stage, Before: before, After: maps.Clone(before), Sources: make(map[string]string)}
	maps.Copy(c.Sources, m.GeoSources)
	var completed []string
	var failures []error
	for i, outcome := range outcomes {
		kind := geoSeedFiles[i].kind
		switch {
		case outcome.skipped:
			result.Skipped = append(result.Skipped, geoSeedFiles[i].name)
		case errors.Is(outcome.err, context.Canceled):
			result.Cancelled = true
		case outcome.err != nil:
			result.Failed = append(result.Failed, geoSeedFiles[i].name)
			failures = append(failures, diagnostic.New("diagnostic.geo.assetFailed", "could not prepare default GEO data for %s", geoSeedFiles[i].name))
		case outcome.present:
			result.Present = append(result.Present, outcome.name)
		default:
			hash, err := fileDigest(filepath.Join(stage, outcome.name))
			if err != nil {
				return result, err
			}
			for name := range c.After {
				if geoFileKind(name) == kind {
					delete(c.After, name)
				}
			}
			c.After[outcome.name] = hash
			c.Sources[kind] = sources[kind]
			completed = append(completed, outcome.name)
		}
	}
	if len(completed) != 0 {
		// Cancellation stops transfer; an asset whose download and validation
		// already completed is independently published and reported.
		publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		if err := a.publishPreparedGeo(publishCtx, current, m, config, status, c); err != nil {
			result.Failed = append(result.Failed, completed...)
			slices.Sort(result.Failed)
			return result, errors.Join(err, ctx.Err())
		}
		result.Prepared = completed
	}
	return result, errors.Join(append(failures, ctx.Err())...)
}

// Mihomo selects the first recognized alias in directory order. If an earlier
// alias is broken, promote a later valid one and discard the obsolete aliases.
func (a *App) stageGeoSeed(ctx context.Context, kind, canonical, live, stage string, before map[string]string, trusted bool, cfg downloadnet.Config, progress func(mihomo.Progress)) geoSeedResult {
	if trusted {
		var names []string
		for name := range before {
			if geoFileKind(name) == kind {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		for i, name := range names {
			if err := mihomo.ValidateDefaultGeo(ctx, kind, filepath.Join(live, name)); err == nil {
				if i == 0 {
					return geoSeedResult{name: name, present: true}
				}
				return geoSeedResult{name: name, err: copyGeoForSwitch(ctx, live, stage, stage, name, before[name], -1, -1)}
			}
			if err := ctx.Err(); err != nil {
				return geoSeedResult{err: err}
			}
		}
	}
	download := a.DownloadGeo
	if download == nil {
		download = mihomo.DownloadDefaultGeo
	}
	return geoSeedResult{name: canonical, err: download(ctx, kind, filepath.Join(stage, canonical), cfg, progress)}
}
