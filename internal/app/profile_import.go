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
		}
		if draft.RememberSource {
			entry.Address = draft.Address
		}
		lib.Profiles[id] = entry
		nameProfile(&lib, id, configuration.ResolveName(draft.Name, download.Headers, draft.Address))
		if err := a.validateProfile(ctx, &lib, id); err != nil {
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

// Refresh saves a validated download as a draft of this profile. It never
// changes the already applied generation or any other profile's content.
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
		if address == "" {
			return diagnostic.New("diagnostic.profile.noAddress", "this profile has no remembered address; set a URL or import a new configuration")
		}
		if _, err := a.library().Read(entry.Spec.Revision); err != nil {
			return err
		}
		download, err := a.downloadSource(ctx, address)
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
		entry.Spec.Revision, entry.UpdatedAt = revision, time.Now().UTC()
		if changeAddress {
			entry.Address = address
		}
		lib.Profiles[id] = entry
		if entry.NameSource != configuration.NameCustom {
			if changeAddress {
				nameProfile(&lib, id, configuration.ResolveName("", download.Headers, address))
			} else if name := configuration.ProviderName(download.Headers); name != "" {
				nameProfile(&lib, id, configuration.NameCandidate{Name: name, Source: configuration.NameProvider})
			}
		}
		if err := a.validateProfile(ctx, &lib, id); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.library().Save(lib, old)
	})
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

func (a *App) validateProfile(ctx context.Context, lib *configuration.Library, id string) error {
	m, installed, err := a.buildContext()
	if err != nil {
		return err
	}
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
