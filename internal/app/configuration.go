package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// buildRecord pins a profile's content and overrides for one generation.
// Later downloads, renames and address edits cannot change its restart inputs.
type buildRecord struct {
	Profile string                 `json:"profile"`
	Recipe  *configuration.Profile `json:"recipe,omitempty"`
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
		return record, diagnostic.New("diagnostic.profile.missing", "profile does not exist")
	}
	p := entry.Spec
	if p.Core != "mihomo" {
		return record, diagnostic.New("diagnostic.profile.unsupportedCore", "this profile requires an unsupported core")
	}
	record.Recipe = &p
	return record, nil
}

func (a *App) recipeInputs(lib configuration.Library, name string) (mihomo.BuildOptions, buildRecord, error) {
	record, err := recipeRecord(lib, name)
	if err != nil || record.Recipe == nil {
		return mihomo.BuildOptions{}, record, err
	}
	p := record.Recipe
	raw, err := a.library().Read(p.Revision)
	return mihomo.BuildOptions{Base: raw, Overrides: []byte(p.Overrides)}, record, err
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
		return nil, record, diagnostic.New("diagnostic.profile.build", "profile %s could not be built: %w", name, err)
	}
	if engine {
		if err := a.validateConfig(ctx, m, cfg); err != nil {
			return nil, record, diagnostic.New("diagnostic.profile.validate", "profile %s failed core validation: %w", name, err)
		}
	}
	return cfg, record, nil
}

type ProfileInfo struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	NameSource   string    `json:"name_source"`
	NameBase     string    `json:"name_base"`
	Core         string    `json:"core"`
	Refreshable  bool      `json:"refreshable"`
	Kind         string    `json:"kind"`
	Origin       string    `json:"origin"`
	UpdatedAt    time.Time `json:"updated_at"`
	Pending      bool      `json:"pending"`
	Validated    bool      `json:"validated"`
	HasOverrides bool      `json:"has_overrides"`
}

func (a *App) profileInfo(lib configuration.Library, name string, m manifest, installed bool) (ProfileInfo, error) {
	record, err := recipeRecord(lib, name)
	if err != nil {
		return ProfileInfo{}, err
	}
	info := ProfileInfo{ID: name, Name: name, NameSource: configuration.NameCustom, NameBase: name, Core: "mihomo", Kind: "builtin", Origin: "Built-in", Validated: installed && name == "default"}
	if record.Recipe != nil {
		entry := lib.Profiles[name]
		info.Name, info.Description, info.NameSource, info.NameBase = entry.Name, entry.Description, entry.NameSource, entry.NameBase
		info.Core, info.Refreshable, info.UpdatedAt = entry.Spec.Core, entry.Address != "", entry.UpdatedAt
		info.Kind, info.Origin = "imported", "Imported file"
		if entry.Address != "" {
			info.Kind, info.Origin = "local", "Local file"
			if address, err := url.Parse(entry.Address); err == nil && (address.Scheme == "http" || address.Scheme == "https") {
				info.Kind, info.Origin = "remote", address.Hostname()
			}
		}
		info.Validated = entry.Validated
		info.HasOverrides = record.Recipe.Overrides != ""
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
		id, err := resolveProfileID(lib, name)
		if err != nil {
			return err
		}
		info, err = a.profileInfo(lib, id, m, installed)
		return err
	})
	return info, err
}

// SetProfileOverrides edits a draft. The currently applied generation is a
// pinned input snapshot until the user explicitly applies this profile again.
func (a *App) SetProfileOverrides(ctx context.Context, name string, raw []byte) error {
	if len(raw) > configuration.MaxDocumentSize {
		return diagnostic.New("diagnostic.profile.overridesSize", "overrides exceed 8 MiB")
	}
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		name, err = resolveProfileID(lib, name)
		if err != nil {
			return err
		}
		entry, ok := lib.Profiles[name]
		if !ok {
			return diagnostic.New("diagnostic.profile.chooseOverrides", "choose a saved profile to edit overrides")
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
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		name, err = resolveProfileID(lib, name)
		if err != nil {
			return err
		}
		if _, ok := lib.Profiles[name]; !ok {
			return diagnostic.New("diagnostic.profile.missing", "profile does not exist")
		}
		m, installed, err := a.buildContext()
		if err != nil {
			return err
		}
		if installed && m.Applied == name {
			return diagnostic.New("diagnostic.profile.deleteSelected", "apply another profile before deleting the selected profile")
		}
		delete(lib.Profiles, name)
		return a.library().Save(lib, old)
	})
}
