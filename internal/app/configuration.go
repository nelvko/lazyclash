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
	m, target, err := a.buildTarget()
	return m, target != "", err
}

func (a *App) buildTarget() (manifest, string, error) {
	m, target, err := a.load()
	if errors.Is(err, os.ErrNotExist) {
		return manifest{Controller: "127.0.0.1:9097", Secret: "structure-validation-only", Port: 7897}, "", nil
	}
	return m, target, err
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

func (a *App) buildProfile(ctx context.Context, lib configuration.Library, name string, m manifest, engine bool) ([]byte, buildRecord, error) {
	record, err := recipeRecord(lib, name)
	if err != nil {
		return nil, record, err
	}
	cfg, err := a.buildRecipe(record, m)
	if err == nil && engine {
		err = a.validateRecipe(ctx, name, m, cfg)
	}
	return cfg, record, err
}

func (a *App) buildRecipe(record buildRecord, m manifest) ([]byte, error) {
	opts := mihomo.BuildOptions{
		Controller: m.Controller, Secret: m.Secret, MixedPort: m.Port,
		General: m.CoreSettings, Mode: m.Mode, TUN: m.TUN, ServiceName: a.Name,
	}
	var err error
	if record.Profile == "default" {
		opts.Base, err = mihomo.Default(m.Controller, m.Secret, m.Port)
	} else {
		opts.Base, err = a.library().Read(record.Recipe.Revision)
		opts.Overrides = []byte(record.Recipe.Overrides)
	}
	if err != nil {
		return nil, err
	}
	cfg, err := mihomo.Build(opts)
	if err != nil {
		return nil, diagnostic.New("diagnostic.profile.build", "profile %s could not be built: %w", record.Profile, err)
	}
	return cfg, nil
}

func (a *App) validateRecipe(ctx context.Context, name string, m manifest, cfg []byte) error {
	if err := a.validateConfig(ctx, m, cfg); err != nil {
		return diagnostic.New("diagnostic.profile.validate", "profile %s failed core validation: %w", name, err)
	}
	return nil
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

	Quota *configuration.SubscriptionQuota `json:"quota,omitempty"`
}

func (a *App) profileInfo(lib configuration.Library, name string, m manifest, installed bool) (ProfileInfo, error) {
	record, err := recipeRecord(lib, name)
	if err != nil {
		return ProfileInfo{}, err
	}
	entry, ok := lib.Profiles[name]
	if !ok {
		return ProfileInfo{}, diagnostic.New("diagnostic.profile.missing", "profile does not exist")
	}
	info := ProfileInfo{
		ID: name, Name: entry.Name, Description: entry.Description,
		NameSource: entry.NameSource, NameBase: entry.NameBase,
		Core: entry.Spec.Core, Refreshable: entry.Address != "", UpdatedAt: entry.UpdatedAt,
		Kind: "imported", Origin: "Imported file", Validated: entry.Validated,
		HasOverrides: entry.Spec.Overrides != "",
	}
	if quota := entry.Quota; quota != (configuration.SubscriptionQuota{}) {
		info.Quota = &quota
	}
	if info.Refreshable {
		info.Kind, info.Origin = "local", "Local file"
		if address, err := url.Parse(entry.Address); err == nil && (address.Scheme == "http" || address.Scheme == "https") {
			info.Kind, info.Origin = "remote", address.Hostname()
		}
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
		for _, name := range names {
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

// SetProfileOverrides imports an override document and deploys it if selected.
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
		previous := entry
		entry.Spec.Overrides = string(raw)
		lib.Profiles[name] = entry
		return a.saveProfileEdit(ctx, lib, old, name, previous)
	})
}

func (a *App) DeleteProfile(ctx context.Context, name string) error {
	return a.DeleteProfileWithReplacement(ctx, name, "")
}

// DeleteProfileWithReplacement switches away from the reviewed selection before
// deleting it. Both arguments identify stable IDs, never display names.
// An interruption between those two commits leaves the old record
// available for an ordinary delete, with a valid selection at every point.
func (a *App) DeleteProfileWithReplacement(ctx context.Context, id, replacementID string) error {
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		entry, ok := lib.Profiles[id]
		if !ok {
			return diagnostic.New("diagnostic.profile.missing", "profile does not exist")
		}
		m, installed, err := a.buildContext()
		if err != nil {
			return err
		}
		if !installed || m.Applied != id {
			if replacementID != "" {
				return diagnostic.New("diagnostic.profile.deleteSelectionChanged", "the profile is no longer selected; review the deletion again")
			}
			delete(lib.Profiles, id)
			return a.library().Save(lib, old)
		}
		if replacementID == "" {
			return diagnostic.New("diagnostic.profile.deleteSelected", "choose a replacement profile or confirm using no subscription before deleting the selected profile")
		}
		if replacementID != "default" {
			if _, ok := lib.Profiles[replacementID]; !ok {
				return diagnostic.New("diagnostic.profile.missing", "profile does not exist")
			}
		}
		if id == replacementID {
			return diagnostic.New("diagnostic.profile.deleteSame", "choose a different replacement profile")
		}
		if err := a.applyProfileLocked(ctx, lib, replacementID); err != nil {
			return err
		}
		// Apply may have marked the replacement's revision validated.
		lib, old, err = a.library().Load()
		if err == nil && lib.Profiles[id] != entry {
			return diagnostic.New("diagnostic.profile.deleteTargetChanged", "selection changed; review the updated profile before deleting it")
		}
		if err == nil {
			delete(lib.Profiles, id)
			err = a.library().Save(lib, old)
		}
		if err != nil {
			return diagnostic.New("diagnostic.profile.deleteAfterSwitch", "selection changed, but the profile could not be removed: %w", err)
		}
		return nil
	})
}
