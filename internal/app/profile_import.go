package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// Add imports a private copy. AddSubscription also remembers the address for
// updates. Both create a complete profile without selecting or starting it.
func (a *App) Add(ctx context.Context, name, address string) error {
	return a.importProfile(ctx, name, address, false)
}

func (a *App) AddSubscription(ctx context.Context, name, address string) error {
	return a.importProfile(ctx, name, address, true)
}

func (a *App) importProfile(ctx context.Context, name, address string, remember bool) error {
	if name != "" && configuration.CleanName(name) == "" {
		return errors.New("profile name must not be empty")
	}
	address = strings.TrimSpace(address)
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		id, err := configurationID(lib, name)
		if err != nil {
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
		entry := configuration.ProfileRecord{Spec: configuration.Profile{Core: "mihomo", Revision: revision}, UpdatedAt: time.Now().UTC()}
		if remember {
			entry.Address = address
		}
		lib.Profiles[id] = entry
		nameProfile(&lib, id, configuration.ResolveName(name, download.Headers, address))
		if err := a.validateProfile(ctx, &lib, id); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.library().Save(lib, old)
	})
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
			return errors.New("choose a saved profile to update")
		}
		if !changeAddress {
			address = entry.Address
		}
		if address == "" {
			return errors.New("this profile has no remembered address; set a URL or import a new configuration")
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
			failures = append(failures, fmt.Errorf("%s: %w", profile.Name, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d profiles could not be updated: %w", len(failures), errors.Join(failures...))
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
