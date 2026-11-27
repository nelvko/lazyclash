package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/store"
)

const profileEditJournalName = "profile-edit-pending.json"

// An active edit changes two durable pointers: the library entry and the
// deployed generation. The previous entry allows recovery to undo the first
// write if the process exits before the new generation is confirmed.
type profileEditJournal struct {
	Installation, Profile, OldBuild, NewBuild, NewRecord string
	Previous                                             configuration.ProfileRecord
}

func recordHash(entry configuration.ProfileRecord) string {
	raw, _ := json.Marshal(entry)
	return digest(raw)
}

func (a *App) saveProfileEdit(ctx context.Context, lib configuration.Library, old []byte, id string, previous configuration.ProfileRecord) error {
	m, target, err := a.buildTarget()
	if err != nil {
		return err
	}
	cfg, recipe, err := a.buildProfile(ctx, lib, id, m, false)
	if err != nil {
		return err
	}
	selected := target != "" && m.Applied == id
	var candidate *geoCandidate
	if target != "" && (!selected || digest(cfg) != m.SHA) {
		candidate, err = a.prepareProfileCandidate(ctx, id, m, cfg)
		if err != nil {
			return err
		}
		defer candidate.Close()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	next := lib.Profiles[id]
	next.Validated = target != ""
	lib.Profiles[id] = next
	if !selected || (m.Build != nil && m.Build.fingerprint() == recipe.fingerprint()) {
		return a.library().Save(lib, old)
	}
	if m.Build == nil {
		return diagnostic.New("diagnostic.profile.pinnedRecipeMissing", "could not identify the applied profile recipe")
	}
	journal := profileEditJournal{Installation: m.ID, Profile: id, OldBuild: m.Build.fingerprint(), NewBuild: recipe.fingerprint(), Previous: previous, NewRecord: recordHash(next)}
	raw, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	// store.Read must be able to read the whole undo record after a crash.
	if len(raw) > 16<<20 {
		return diagnostic.New("diagnostic.store.fileSize", "managed file exceeds size limit")
	}
	file := filepath.Join(a.Home, profileEditJournalName)
	if err = store.Replace(file, raw, nil, 0600); err != nil {
		return err
	}
	if err = a.library().Save(lib, old); err != nil {
		return errors.Join(err, a.recoverProfileEdit())
	}
	m.Build = &recipe
	if err = a.publishProfileRefreshCandidate(ctx, target, m, cfg, candidate); err != nil {
		// If generation recovery itself failed it must finish first. Every normal
		// invocation settles it before attempting to restore the library entry.
		if _, journalErr := os.Lstat(filepath.Join(a.Home, "apply-pending.json")); !errors.Is(journalErr, os.ErrNotExist) {
			return err
		}
		return errors.Join(err, a.recoverProfileEdit())
	}
	return store.Remove(file, raw)
}

// Run after generation recovery, so the live pointer identifies which edit
// committed. Refuse to overwrite independently modified library metadata.
func (a *App) recoverProfileEdit() error {
	file := filepath.Join(a.Home, profileEditJournalName)
	raw, err := store.Read(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var p profileEditJournal
	conflict := diagnostic.New("diagnostic.profile.editRecovery", "profile edit recovery found unexpected data; preserving the saved files")
	if json.Unmarshal(raw, &p) != nil || !configuration.ValidName(p.Profile) || p.OldBuild == p.NewBuild {
		return conflict
	}
	m, _, err := a.load()
	if err != nil {
		return err
	}
	if m.ID != p.Installation || m.Applied != p.Profile || m.Build == nil {
		return conflict
	}
	lib, old, err := a.library().Load()
	if err != nil {
		return err
	}
	entry, ok := lib.Profiles[p.Profile]
	if !ok {
		return conflict
	}
	switch m.Build.fingerprint() {
	case p.NewBuild:
		if recordHash(entry) != p.NewRecord {
			return conflict
		}
	case p.OldBuild:
		if entry != p.Previous {
			if recordHash(entry) != p.NewRecord {
				return conflict
			}
			lib.Profiles[p.Profile] = p.Previous
			if err := a.library().Save(lib, old); err != nil {
				return err
			}
		}
	default:
		return conflict
	}
	return store.Remove(file, raw)
}
