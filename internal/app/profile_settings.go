package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// ProfileSettings is read on demand by the editor. The unredacted address
// may contain credentials and is never included in ProfileInfo or Snapshot.
type ProfileSettings struct {
	ID          string
	Name        string
	Description string
	Address     string `json:"-"`
	reviewed    string
}

func profileSettings(id string, record configuration.ProfileRecord) ProfileSettings {
	// Quota, timestamps and recipe revisions can change while the form is
	// open. They are never copied back from the form.
	fields := [...]string{id, record.Name, record.NameSource, record.NameBase, record.Description, record.Address}
	reviewed, _ := json.Marshal(fields)
	return ProfileSettings{
		ID: id, Name: record.Name, Description: record.Description, Address: record.Address,
		reviewed: digest(reviewed),
	}
}

func (a *App) GetProfileSettings(ctx context.Context, id string) (ProfileSettings, error) {
	var settings ProfileSettings
	err := a.locked(ctx, func() error {
		lib, _, err := a.library().Load()
		if err != nil {
			return err
		}
		entry, ok := lib.Profiles[id]
		if !ok {
			return diagnostic.New("diagnostic.profile.missing", "profile does not exist")
		}
		settings = profileSettings(id, entry)
		return nil
	})
	return settings, err
}

// SaveProfileSettings commits all three fields together. An unchanged name
// keeps its provenance, including when a replacement source provides a title.
func (a *App) SaveProfileSettings(ctx context.Context, edited ProfileSettings) error {
	edited.Name = configuration.CleanName(edited.Name)
	if edited.Name == "" {
		return diagnostic.New("diagnostic.profile.emptyName", "profile name must not be empty")
	}
	edited.Description = configuration.CleanDescription(edited.Description)
	edited.Address = strings.TrimSpace(edited.Address)
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		id := edited.ID
		entry, ok := lib.Profiles[id]
		if !ok {
			return diagnostic.New("diagnostic.profile.missing", "profile does not exist")
		}
		current := profileSettings(id, entry)
		if current.reviewed != edited.reviewed {
			return diagnostic.New("diagnostic.profile.settingsChanged", "Profile settings changed. Reopen settings to edit the latest values.")
		}
		if current == edited {
			return nil
		}
		previous := entry
		entry.Description = edited.Description
		lib.Profiles[id] = entry
		if edited.Name != current.Name {
			nameProfile(&lib, id, configuration.NameCandidate{Name: edited.Name, Source: configuration.NameCustom})
		}
		if edited.Address != current.Address {
			if err := a.readProfileSource(ctx, &lib, id, edited.Address, true); err != nil {
				return err
			}
			return a.saveProfileEdit(ctx, lib, old, id, previous)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.library().Save(lib, old)
	})
}
