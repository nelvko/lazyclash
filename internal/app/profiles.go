package app

import (
	"context"

	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/service"
)

func (a *App) Apply(ctx context.Context, name string) error {
	return a.locked(ctx, func() error {
		lib, _, err := a.library().Load()
		if err != nil {
			return err
		}
		id, err := resolveProfileID(lib, name)
		if err != nil {
			return err
		}
		return a.applyProfileLocked(ctx, lib, id)
	})
}

// DeactivateProfile keeps an explicit card action bound to its original ID.
// An empty expectedID deliberately addresses whichever profile is selected.
func (a *App) DeactivateProfile(ctx context.Context, expectedID string) error {
	return a.locked(ctx, func() error {
		m, installed, err := a.buildContext()
		if err != nil || !installed || m.selectedProfile() == "" {
			return err
		}
		if expectedID != "" && expectedID != m.Applied {
			return diagnostic.New("diagnostic.profile.deactivateSelectionChanged", "the selected profile changed; review the selection before deactivating it")
		}
		return a.applyProfileLocked(ctx, configuration.Library{}, "default")
	})
}

func (a *App) applyProfileLocked(ctx context.Context, lib configuration.Library, name string) error {
	if err := a.requireService(); err != nil {
		return err
	}
	m, old, err := a.load()
	if err != nil {
		return err
	}
	status, err := a.profileServiceStatus(ctx)
	if err != nil {
		return err
	}
	cfg, inputs, err := a.buildProfile(ctx, lib, name, m, false)
	if err != nil {
		return err
	}
	candidate, err := a.prepareProfileCandidate(ctx, name, m, cfg)
	if err != nil {
		return err
	}
	defer candidate.Close()
	m.Build = &inputs
	m.Applied = name
	if err := a.publishProfileGeneration(ctx, old, m, cfg, status, false, candidate); err != nil {
		return err
	}
	if err := a.markProfileValidated(name); err != nil {
		return diagnostic.New("diagnostic.profile.metadataSave", "configuration applied, but profile validation metadata could not be saved: %w", err)
	}
	return nil
}

func (a *App) profileServiceStatus(ctx context.Context) (service.Status, error) {
	if err := a.requireService(); err != nil {
		return service.Status{}, err
	}
	bin, config, data := a.paths()
	if err := a.Service.Verify(ctx, bin, config, data); err != nil {
		return service.Status{}, err
	}
	status, err := a.Service.Inspect(ctx)
	if err == nil && (status.State == "activating" || status.State == "deactivating" || status.State == "reloading") {
		return status, diagnostic.New("diagnostic.profile.serviceChanging", "service is changing state; wait before applying a profile")
	}
	return status, err
}
