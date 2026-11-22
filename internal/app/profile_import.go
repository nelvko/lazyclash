package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// ProfileDraft contains the user-facing metadata for a new complete profile.
// Address may be an HTTP(S) URL or an absolute local file path. The source is
// kept when RememberSource is true so the profile can be refreshed later.
type ProfileDraft struct {
	Name           string
	Description    string
	Address        string
	RememberSource bool
}

// CreateProfile downloads or reads a source, validates it, and publishes one
// complete profile atomically. Metadata is stored separately from the core
// recipe and never becomes part of the generated Mihomo YAML.
func (a *App) CreateProfile(ctx context.Context, draft ProfileDraft) error {
	if draft.Name != "" && configuration.CleanName(draft.Name) == "" {
		return diagnostic.New("diagnostic.profile.emptyName", "profile name must not be empty")
	}
	draft.Description = configuration.CleanDescription(draft.Description)
	draft.Address = strings.TrimSpace(draft.Address)
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		id, err := configurationID(lib, draft.Name)
		if err != nil {
			return err
		}
		download, err := a.downloadSource(ctx, draft.Address)
		if err != nil {
			return err
		}
		if err := mihomo.ValidateSource(download.Raw); err != nil {
			return err
		}
		revision, err := a.library().Put(download.Raw)
		if err != nil {
			return err
		}
		entry := configuration.ProfileRecord{
			Spec:        configuration.Profile{Core: "mihomo", Revision: revision},
			Description: draft.Description,
			UpdatedAt:   time.Now().UTC(),
			Quota:       configuration.ParseSubscriptionQuota(download.Headers),
		}
		if draft.RememberSource {
			entry.Address = draft.Address
		}
		lib.Profiles[id] = entry
		nameProfile(&lib, id, configuration.ResolveName(draft.Name, download.Headers, draft.Address))
		m, installed, err := a.buildContext()
		if err != nil {
			return err
		}
		if err := a.validateProfile(ctx, &lib, id, m, installed); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.library().Save(lib, old)
	})
}

// Add imports a private copy. AddSubscription also remembers the address for
// updates. Both create a complete profile without selecting or starting it.
func (a *App) Add(ctx context.Context, name, address string) error {
	return a.CreateProfile(ctx, ProfileDraft{Name: name, Address: address})
}

func (a *App) AddSubscription(ctx context.Context, name, address string) error {
	return a.CreateProfile(ctx, ProfileDraft{Name: name, Address: address, RememberSource: true})
}

// Refresh updates the saved source. A selected profile also picks up the new
// revision using the overrides already applied to this instance.
func (a *App) Refresh(ctx context.Context, reference string) error {
	return a.updateProfile(ctx, reference, "", false)
}

// UpdateProfileAddress replaces a profile's remembered URL or local path only
// after its new content and existing overrides pass validation together.
func (a *App) UpdateProfileAddress(ctx context.Context, reference, address string) error {
	return a.updateProfile(ctx, reference, strings.TrimSpace(address), true)
}

func (a *App) updateProfile(ctx context.Context, reference, address string, changeAddress bool) error {
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		id, err := resolveProfileID(lib, reference)
		if err != nil {
			return err
		}
		entry, ok := lib.Profiles[id]
		if !ok {
			return diagnostic.New("diagnostic.profile.chooseUpdate", "choose a saved profile to update")
		}
		if !changeAddress {
			address = entry.Address
		}
		sourceChanged, err := a.readProfileSource(ctx, &lib, id, address, changeAddress)
		if err != nil {
			return err
		}
		m, target, err := a.buildTarget()
		if err != nil {
			return err
		}
		var cfg []byte
		if !changeAddress && m.Applied == id {
			cfg, err = a.prepareSelectedRefresh(ctx, &lib, id, &m, sourceChanged)
		} else {
			err = a.validateProfile(ctx, &lib, id, m, target != "")
		}
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.library().Save(lib, old); err != nil {
			return err
		}
		if cfg != nil {
			if err := a.publishProfileRefresh(ctx, target, m, cfg); err != nil {
				return diagnostic.New("diagnostic.profile.refreshNotApplied", "subscription saved; refresh again to retry the runtime update: %w", err)
			}
		}
		return nil
	})
}

// readProfileSource updates the in-memory candidate using one fetch. Callers
// validate the resulting recipe before committing any metadata to the index.
func (a *App) readProfileSource(ctx context.Context, lib *configuration.Library, id, address string, changeAddress bool) (bool, error) {
	entry := lib.Profiles[id]
	if address == "" {
		return false, diagnostic.New("diagnostic.profile.noAddress", "this profile has no remembered address; set a URL or import a new configuration")
	}
	if _, err := a.library().Read(entry.Spec.Revision); err != nil {
		return false, err
	}
	download, err := a.downloadSource(ctx, address)
	if err != nil {
		return false, err
	}
	if err := mihomo.ValidateSource(download.Raw); err != nil {
		return false, err
	}
	revision, err := a.library().Put(download.Raw)
	if err != nil {
		return false, err
	}
	sourceChanged := revision != entry.Spec.Revision
	entry.Spec.Revision, entry.UpdatedAt = revision, time.Now().UTC()
	entry.Quota = configuration.ParseSubscriptionQuota(download.Headers)
	if changeAddress {
		entry.Address = address
	}
	lib.Profiles[id] = entry
	if entry.NameSource != configuration.NameCustom {
		if changeAddress {
			nameProfile(lib, id, configuration.ResolveName("", download.Headers, address))
		} else if name := configuration.ProviderName(download.Headers); name != "" {
			nameProfile(lib, id, configuration.NameCandidate{Name: name, Source: configuration.NameProvider})
		}
	}
	return sourceChanged, nil
}

// Advance only the source revision. The library may contain user overrides
// which should stay editable and separate from the active recipe.
func (a *App) prepareSelectedRefresh(ctx context.Context, lib *configuration.Library, id string, m *manifest, sourceChanged bool) ([]byte, error) {
	if m.Build == nil || m.Build.Profile != id || m.Build.Recipe == nil || m.Build.Recipe.Core != "mihomo" {
		return nil, diagnostic.New("diagnostic.profile.pinnedRecipeMissing", "could not identify the applied profile recipe")
	}
	entry := lib.Profiles[id]
	pinned := *m.Build.Recipe
	source := pinned
	source.Revision = entry.Spec.Revision
	if source == entry.Spec {
		entry.Validated = true
	} else if sourceChanged {
		// Only the active overrides will be validated against the new source.
		entry.Validated = false
	}
	lib.Profiles[id] = entry
	if source == pinned {
		return nil, nil
	}
	next := buildRecord{Profile: id, Recipe: &source}
	cfg, err := a.buildRecipe(next, *m)
	if err != nil {
		return nil, err
	}
	if digest(cfg) != m.SHA {
		if err := a.validateRecipe(ctx, id, *m, cfg); err != nil {
			return nil, err
		}
	}
	m.Build = &next
	return cfg, nil
}

// RefreshAllProfiles commits each profile independently, preserving successful
// downloads if another fails. Cancellation stops the remaining downloads.
func (a *App) RefreshAllProfiles(ctx context.Context) error {
	profiles, err := a.ListProfiles(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, profile := range profiles {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		if !profile.Refreshable {
			continue
		}
		if err := a.Refresh(ctx, profile.ID); err != nil {
			failures = append(failures, diagnostic.New("diagnostic.profile.namedError", "%s: %w", profile.Name, err))
		}
	}
	if len(failures) > 0 {
		return diagnostic.New("diagnostic.profile.updateFailures", "%d profiles could not be updated: %w", len(failures), errors.Join(failures...))
	}
	return nil
}

func (a *App) validateProfile(ctx context.Context, lib *configuration.Library, id string, m manifest, installed bool) error {
	if _, _, err := a.buildProfile(ctx, *lib, id, m, installed); err != nil {
		return err
	}
	entry := lib.Profiles[id]
	entry.Validated = installed
	lib.Profiles[id] = entry
	return nil
}

// markProfileValidated records a successful apply without changing its inputs.
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
