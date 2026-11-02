package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/diagnostic"
)

func configurationID(lib configuration.Library, preferred string) (string, error) {
	available := func(id string) bool {
		_, profile := lib.Profiles[id]
		return !profile
	}
	// Use an explicit name as the initial ID when it fits the identifier format.
	// The display label can subsequently change without changing this ID.
	if configuration.ValidName(preferred) && available(preferred) {
		return preferred, nil
	}
	for {
		var value [16]byte
		if _, err := rand.Read(value[:]); err != nil {
			return "", err
		}
		id := "p-" + hex.EncodeToString(value[:])
		if available(id) {
			return id, nil
		}
	}
}

func resolveProfileID(lib configuration.Library, reference string) (string, error) {
	if reference == "default" {
		return reference, nil
	}
	if _, ok := lib.Profiles[reference]; ok {
		return reference, nil
	}
	key := configuration.NameKey(reference)
	for id, profile := range lib.Profiles {
		if configuration.NameKey(profile.Name) == key {
			return id, nil
		}
	}
	return "", diagnostic.New("diagnostic.profile.missing", "profile does not exist")
}

func nameProfile(lib *configuration.Library, id string, candidate configuration.NameCandidate) {
	entry := lib.Profiles[id]
	entry.Name = configuration.UniqueName(candidate.Name, lib.Profiles, id)
	entry.NameBase, entry.NameSource = candidate.Name, candidate.Source
	lib.Profiles[id] = entry
}

// RenameProfile changes only display metadata. Runtime build records pin IDs
// and recipes, so renaming never regenerates or reloads the current core.
func (a *App) RenameProfile(ctx context.Context, reference, name string) error {
	name = configuration.CleanName(name)
	if name == "" {
		return diagnostic.New("diagnostic.profile.emptyName", "profile name must not be empty")
	}
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		id, err := resolveProfileID(lib, reference)
		if err != nil {
			return err
		}
		if id == "default" {
			return diagnostic.New("diagnostic.profile.builtinRename", "the built-in profile cannot be renamed")
		}
		nameProfile(&lib, id, configuration.NameCandidate{Name: name, Source: "custom"})
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.library().Save(lib, old)
	})
}

// UpdateProfileDescription changes only user-facing metadata. It does not
// rebuild or reload the profile because descriptions are outside the runtime
// recipe.
func (a *App) UpdateProfileDescription(ctx context.Context, reference, description string) error {
	description = configuration.CleanDescription(description)
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		id, err := resolveProfileID(lib, reference)
		if err != nil {
			return err
		}
		if id == "default" {
			return diagnostic.New("diagnostic.profile.builtinDescription", "the built-in profile cannot be edited")
		}
		entry, ok := lib.Profiles[id]
		if !ok {
			return diagnostic.New("diagnostic.profile.missing", "profile does not exist")
		}
		entry.Description = description
		lib.Profiles[id] = entry
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.library().Save(lib, old)
	})
}
