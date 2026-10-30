package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/nelvko/lazyclash/internal/configuration"
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
	return "", errors.New("profile does not exist")
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
		return errors.New("profile name must not be empty")
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
			return errors.New("the built-in profile cannot be renamed")
		}
		nameProfile(&lib, id, configuration.NameCandidate{Name: name, Source: "custom"})
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.library().Save(lib, old)
	})
}
