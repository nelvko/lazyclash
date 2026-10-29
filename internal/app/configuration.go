package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// buildRecord pins the exact recipe and immutable source revisions used by a
// generation. Later source/override edits cannot change its restart behavior.
type buildRecord struct {
	Profile string                   `json:"profile"`
	Recipe  *configuration.Profile   `json:"recipe,omitempty"`
	Sources []configuration.Revision `json:"sources,omitempty"`
}

func (r buildRecord) fingerprint() string { raw, _ := json.Marshal(r); return digest(raw) }

func (a *App) library() configuration.Repository {
	return configuration.Repository{Root: filepath.Join(a.Home, "configuration")}
}

func (a *App) buildContext() (manifest, bool, error) {
	m, _, err := a.load()
	if errors.Is(err, os.ErrNotExist) {
		return manifest{Controller: "127.0.0.1:9097", Secret: "structure-validation-only", Port: 7897}, false, nil
	}
	return m, err == nil, err
}

func recipeRecord(lib configuration.Library, name string) (buildRecord, error) {
	record := buildRecord{Profile: name}
	if name == "default" {
		return record, nil
	}
	entry, ok := lib.Profiles[name]
	if !ok {
		return record, errors.New("profile does not exist")
	}
	p := entry.Spec
	if p.Core != "mihomo" {
		return record, errors.New("this profile requires an unsupported core")
	}
	record.Recipe = &p
	for _, name := range p.SourceNames() {
		source, ok := lib.Subscriptions[name]
		if !ok || source.Format != "mihomo" {
			return record, errors.New("profile references a missing or incompatible subscription")
		}
		record.Sources = append(record.Sources, configuration.Revision{Name: name, SHA256: source.Revision})
	}
	return record, nil
}

func (a *App) recipeInputs(lib configuration.Library, name string) (mihomo.BuildOptions, buildRecord, error) {
	record, err := recipeRecord(lib, name)
	if err != nil || record.Recipe == nil {
		return mihomo.BuildOptions{}, record, err
	}
	p := record.Recipe
	opts := mihomo.BuildOptions{Overrides: []byte(p.Overrides)}
	if p.Mode == "managed" {
		opts.Base = []byte(p.Template)
		opts.NodeSources = []mihomo.NodeSource{}
	}
	for _, source := range record.Sources {
		raw, err := a.library().Read(source.SHA256)
		if err != nil {
			return opts, record, err
		}
		if p.Mode == "full" {
			opts.Base = raw
		} else {
			opts.NodeSources = append(opts.NodeSources, mihomo.NodeSource{Name: source.Name, Raw: raw})
		}
	}
	return opts, record, nil
}

func (a *App) buildProfile(ctx context.Context, lib configuration.Library, name string, m manifest, engine bool) ([]byte, buildRecord, error) {
	opts, record, err := a.recipeInputs(lib, name)
	if err != nil {
		return nil, record, err
	}
	if name == "default" {
		opts.Base, err = mihomo.Default(m.Controller, m.Secret, m.Port)
		if err != nil {
			return nil, record, err
		}
	}
	opts.Controller, opts.Secret, opts.MixedPort, opts.Mode = m.Controller, m.Secret, m.Port, m.Mode
	cfg, err := mihomo.Build(opts)
	if err != nil {
		return nil, record, fmt.Errorf("profile %s could not be built: %w", name, err)
	}
	if engine {
		if err := a.validateConfig(ctx, m, cfg); err != nil {
			return nil, record, fmt.Errorf("profile %s failed core validation: %w", name, err)
		}
	}
	return cfg, record, nil
}

type ProfileInfo struct {
	Name      string   `json:"name"`
	Core      string   `json:"core"`
	Mode      string   `json:"mode"`
	Sources   []string `json:"sources"`
	Pending   bool     `json:"pending"`
	Validated bool     `json:"validated"`
}

func (a *App) profileInfo(lib configuration.Library, name string, m manifest, installed bool) (ProfileInfo, error) {
	record, err := recipeRecord(lib, name)
	if err != nil {
		return ProfileInfo{}, err
	}
	info := ProfileInfo{Name: name, Core: "mihomo", Mode: "full", Sources: []string{}, Validated: installed && name == "default"}
	if record.Recipe != nil {
		info.Core, info.Mode, info.Sources = record.Recipe.Core, record.Recipe.Mode, record.Recipe.SourceNames()
		info.Validated = lib.Profiles[name].Validated
	}
	info.Pending = installed && m.Applied == name && (m.Build == nil || m.Build.fingerprint() != record.fingerprint())
	return info, nil
}

func (a *App) ListProfiles(ctx context.Context) ([]ProfileInfo, error) {
	out := []ProfileInfo{}
	err := a.locked(ctx, func() error {
		lib, _, err := a.library().Load()
		if err != nil {
			return err
		}
		m, installed, err := a.buildContext()
		if err != nil {
			return err
		}
		names := make([]string, 0, len(lib.Profiles))
		for name := range lib.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range append([]string{"default"}, names...) {
			info, err := a.profileInfo(lib, name, m, installed)
			if err != nil {
				return err
			}
			out = append(out, info)
		}
		return nil
	})
	return out, err
}

func (a *App) ProfileDetails(ctx context.Context, name string) (ProfileInfo, error) {
	var info ProfileInfo
	err := a.locked(ctx, func() error {
		lib, _, err := a.library().Load()
		if err != nil {
			return err
		}
		m, installed, err := a.buildContext()
		if err != nil {
			return err
		}
		info, err = a.profileInfo(lib, name, m, installed)
		return err
	})
	return info, err
}

// CreateProfile saves a validated recipe without selecting or starting it.
func (a *App) CreateProfile(ctx context.Context, name string, spec configuration.Profile) error {
	if !configuration.ValidName(name) {
		return errors.New("invalid or reserved profile name")
	}
	if spec.Core == "" {
		spec.Core = "mihomo"
	}
	if spec.Mode == "" {
		spec.Mode = "full"
	}
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		if _, ok := lib.Profiles[name]; ok {
			return errors.New("profile already exists")
		}
		lib.Profiles[name] = configuration.ProfileRecord{Spec: spec}
		m, installed, err := a.buildContext()
		if err != nil {
			return err
		}
		if _, _, err = a.buildProfile(ctx, lib, name, m, installed); err != nil {
			return err
		}
		lib.Profiles[name] = configuration.ProfileRecord{Spec: spec, Validated: installed}
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.library().Save(lib, old)
	})
}

// SetProfileOverrides edits a draft. The currently applied generation is a
// pinned input snapshot until the user explicitly applies this profile again.
func (a *App) SetProfileOverrides(ctx context.Context, name string, raw []byte) error {
	if len(raw) > configuration.MaxDocumentSize {
		return errors.New("overrides exceed 8 MiB")
	}
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		entry, ok := lib.Profiles[name]
		if !ok {
			return errors.New("choose a saved profile to edit overrides")
		}
		entry.Spec.Overrides = string(raw)
		lib.Profiles[name] = entry
		m, installed, err := a.buildContext()
		if err != nil {
			return err
		}
		if _, _, err = a.buildProfile(ctx, lib, name, m, installed); err != nil {
			return err
		}
		entry.Validated = installed
		lib.Profiles[name] = entry
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.library().Save(lib, old)
	})
}

func (a *App) DeleteProfile(ctx context.Context, name string) error {
	if !configuration.ValidName(name) {
		return errors.New("invalid or reserved profile name")
	}
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		if _, ok := lib.Profiles[name]; !ok {
			return errors.New("profile does not exist")
		}
		m, installed, err := a.buildContext()
		if err != nil {
			return err
		}
		if installed && m.Applied == name {
			return errors.New("apply another profile before deleting the selected profile")
		}
		delete(lib.Profiles, name)
		return a.library().Save(lib, old)
	})
}
