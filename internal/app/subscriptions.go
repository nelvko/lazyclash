package app

import (
	"context"
	"errors"
	"os"
	"sort"
	"time"

	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type SourceInfo struct {
	Name        string    `json:"name"`
	Format      string    `json:"format"`
	Revision    string    `json:"revision"`
	Refreshable bool      `json:"refreshable"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (a *App) ListSources(ctx context.Context) ([]SourceInfo, error) {
	out := []SourceInfo{}
	err := a.locked(ctx, func() error {
		lib, _, err := a.library().Load()
		if err != nil {
			return err
		}
		for name, source := range lib.Subscriptions {
			out = append(out, SourceInfo{Name: name, Format: source.Format, Revision: source.Revision, Refreshable: source.Address != "", UpdatedAt: source.UpdatedAt})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return nil
	})
	return out, err
}

// Add is a one-time import and a full recipe. AddSubscription additionally
// remembers its private address for refresh; neither operation selects it.
func (a *App) Add(ctx context.Context, name, source string) error {
	return a.addSource(ctx, name, source, false, true)
}
func (a *App) AddSubscription(ctx context.Context, name, source string) error {
	return a.addSource(ctx, name, source, true, true)
}

// AddSource creates an independent reusable source. It may be a full Mihomo
// document or an inline-proxies payload. Only a Profile defines its use.
func (a *App) AddSource(ctx context.Context, name, source string) error {
	return a.addSource(ctx, name, source, true, false)
}

func (a *App) addSource(ctx context.Context, name, address string, remember, fullProfile bool) error {
	if !configuration.ValidName(name) {
		return errors.New("name must use 1–48 letters, digits, _ or -; default is reserved")
	}
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		if _, ok := lib.Subscriptions[name]; ok {
			return errors.New("subscription already exists; choose a new name")
		}
		if _, ok := lib.Profiles[name]; fullProfile && ok {
			return errors.New("profile already exists; choose a new name")
		}
		raw, err := a.source(ctx, address)
		if err != nil {
			return err
		}
		if err := mihomo.ValidateSource(raw); err != nil {
			return err
		}
		revision, err := a.library().Put(raw)
		if err != nil {
			return err
		}
		source := configuration.Subscription{Format: "mihomo", Revision: revision, UpdatedAt: time.Now().UTC()}
		if remember {
			source.Address = address
		}
		lib.Subscriptions[name] = source
		if fullProfile {
			entry := configuration.ProfileRecord{Spec: configuration.Profile{Core: "mihomo", Mode: "full", Source: name}}
			lib.Profiles[name] = entry
			m, installed, err := a.buildContext()
			if err != nil {
				return err
			}
			if _, _, err := a.buildProfile(ctx, lib, name, m, installed); err != nil {
				return err
			}
			entry.Validated = installed
			lib.Profiles[name] = entry
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.library().Save(lib, old)
	})
}

// Refresh is the full-profile convenience command. Managed profiles can reuse
// several sources; refresh each source explicitly through RefreshSource.
func (a *App) Refresh(ctx context.Context, name string) error {
	return a.refresh(ctx, name, true)
}
func (a *App) RefreshSource(ctx context.Context, name string) error {
	return a.refresh(ctx, name, false)
}
func (a *App) refresh(ctx context.Context, name string, profile bool) error {
	if !configuration.ValidName(name) {
		return errors.New("invalid or reserved subscription name")
	}
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		if profile {
			entry, ok := lib.Profiles[name]
			if !ok || entry.Spec.Mode != "full" {
				return errors.New("choose a full profile, or refresh a managed profile's individual subscriptions")
			}
			name = entry.Spec.Source
		}
		source, ok := lib.Subscriptions[name]
		if !ok {
			return errors.New("subscription does not exist")
		}
		if source.Address == "" {
			return errors.New("this import has no remembered source; add a subscription with explicit source retention")
		}
		// Verify the previously published revision before fetching. A changed
		// immutable snapshot is corruption, never a new editable user layer.
		if _, err := a.library().Read(source.Revision); err != nil {
			return err
		}
		raw, err := a.source(ctx, source.Address)
		if err != nil {
			return err
		}
		if err := mihomo.ValidateSource(raw); err != nil {
			return err
		}
		revision, err := a.library().Put(raw)
		if err != nil {
			return err
		}
		source.Revision, source.UpdatedAt = revision, time.Now().UTC()
		lib.Subscriptions[name] = source
		m, installed, err := a.buildContext()
		if err != nil {
			return err
		}
		// Validate every dependent recipe against the candidate. A source
		// refresh never silently invalidates saved custom rules or group refs.
		for _, profileName := range sortedProfileNames(lib) {
			entry := lib.Profiles[profileName]
			for _, dependency := range entry.Spec.SourceNames() {
				if dependency != name {
					continue
				}
				if _, _, err := a.buildProfile(ctx, lib, profileName, m, installed); err != nil {
					return err
				}
				entry.Validated = installed
				lib.Profiles[profileName] = entry
				break
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.library().Save(lib, old)
	})
}

func sortedProfileNames(lib configuration.Library) []string {
	names := make([]string, 0, len(lib.Profiles))
	for name := range lib.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DeleteSource refuses referenced sources. Historical snapshot blobs remain so
// already committed generations keep their pinned inputs after library edits.
func (a *App) DeleteSource(ctx context.Context, name string) error {
	if !configuration.ValidName(name) {
		return errors.New("invalid or reserved subscription name")
	}
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		if _, ok := lib.Subscriptions[name]; !ok {
			return errors.New("subscription does not exist")
		}
		for _, entry := range lib.Profiles {
			for _, dependency := range entry.Spec.SourceNames() {
				if dependency == name {
					return errors.New("subscription is referenced by a profile; delete that profile first")
				}
			}
		}
		delete(lib.Subscriptions, name)
		return a.library().Save(lib, old)
	})
}

// markProfileValidated records a successful apply without modifying its recipe.
// A failed metadata write does not undo the already committed runtime change.
func (a *App) markProfileValidated(name string) error {
	if name == "default" {
		return nil
	}
	lib, old, err := a.library().Load()
	if err != nil {
		return err
	}
	entry, ok := lib.Profiles[name]
	if !ok {
		return os.ErrNotExist
	}
	if entry.Validated {
		return nil
	}
	entry.Validated = true
	lib.Profiles[name] = entry
	return a.library().Save(lib, old)
}
