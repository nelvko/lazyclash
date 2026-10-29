package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

// This metadata is private for the same reason as raw profiles: source URLs may
// include subscription tokens. It is only created through explicit opt-in.
type subscriptionSource struct {
	Source string `json:"source"`
	SHA    string `json:"profile_sha256"`
}

// Refresh updates a saved source profile after full validation. Applying it is
// a separate user operation; the running generation and service remain intact.
func (a *App) Refresh(ctx context.Context, name string) error {
	if !profileName.MatchString(name) || name == "default" {
		return errors.New("choose a saved subscription profile; default cannot be refreshed")
	}
	return a.locked(ctx, func() error {
		base := filepath.Join(a.Home, "profiles")
		if err := existingDir(base); err != nil {
			return err
		}
		oldMetadata, err := store.Read(filepath.Join(base, name+".source.json"))
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("this profile has no remembered source; re-import with --remember-source under a new name")
		}
		if err != nil {
			return errors.New("cannot read private subscription source metadata")
		}
		var metadata subscriptionSource
		if json.Unmarshal(oldMetadata, &metadata) != nil || metadata.Source == "" || len(metadata.SHA) != 64 {
			return errors.New("invalid subscription source metadata; preserving it for inspection")
		}
		oldProfile, err := store.Read(filepath.Join(base, name+".yaml"))
		if err != nil {
			return errors.New("cannot read the saved subscription profile")
		}
		if digest(oldProfile) != metadata.SHA {
			return errors.New("saved profile differs from its remembered source metadata; preserving local edits or interrupted refresh; re-import under a new name")
		}
		m, _, err := a.load()
		installed := err == nil
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if !installed {
			m = manifest{Controller: "127.0.0.1:9097", Secret: "structure-validation-only", Port: 7897}
		}
		raw, err := a.source(ctx, metadata.Source)
		if err != nil {
			return err
		}
		effective, err := mihomo.Effective(raw, m.Controller, m.Secret, m.Port)
		if err != nil {
			return err
		}
		if installed {
			if err := a.requireService(); err != nil {
				return err
			}
			if err := a.validateConfig(ctx, m, effective); err != nil {
				return err
			}
		} else if err := validateProfileStructure(effective); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		metadata.SHA = digest(raw)
		return a.writeProfileLocked(name, raw, oldProfile, &metadata, oldMetadata, !installed)
	})
}

// writeProfileLocked publishes metadata before the raw bytes. CAS checks protect
// changes made outside this process. On interruption their hashes cannot match,
// so later refreshes refuse to overwrite either file. No network source is ever
// adopted silently and no active configuration pointer changes here.
func (a *App) writeProfileLocked(name string, raw, oldProfile []byte, metadata *subscriptionSource, oldMetadata []byte, unvalidated bool) error {
	base := filepath.Join(a.Home, "profiles")
	markerPath := filepath.Join(base, name+".unvalidated")
	oldMarker, err := store.Read(markerPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if oldProfile == nil && oldMarker != nil {
		return errors.New("profile validation marker already exists; preserving it for inspection")
	}
	if oldProfile == nil {
		if _, err := os.Lstat(filepath.Join(base, name+".source.json")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("subscription source metadata already exists; preserving it for inspection")
		}
	}
	var newMarker []byte
	markerWritten := false
	if unvalidated {
		newMarker = []byte(digest(raw))
		if err := store.Replace(markerPath, newMarker, oldMarker, 0600); err != nil {
			return err
		}
		markerWritten = true
	}
	rollbackMarker := func() error {
		if !markerWritten {
			return nil
		}
		if oldMarker != nil {
			return store.Replace(markerPath, oldMarker, newMarker, 0600)
		}
		return store.Remove(markerPath, newMarker)
	}
	metadataPath := filepath.Join(base, name+".source.json")
	var newMetadata []byte
	if metadata != nil {
		newMetadata, _ = json.MarshalIndent(metadata, "", "  ")
		if err := store.Replace(metadataPath, newMetadata, oldMetadata, 0600); err != nil {
			return errors.Join(err, rollbackMarker())
		}
	}
	if err := store.Replace(filepath.Join(base, name+".yaml"), raw, oldProfile, 0600); err != nil {
		var rollbackMetadata error
		if metadata != nil {
			if oldMetadata != nil {
				rollbackMetadata = store.Replace(metadataPath, oldMetadata, newMetadata, 0600)
			} else {
				rollbackMetadata = store.Remove(metadataPath, newMetadata)
			}
		}
		return errors.Join(err, rollbackMetadata, rollbackMarker())
	}
	// A refreshed profile that passed engine validation no longer needs its
	// previous structural-validation marker. Remove it only if still unchanged.
	if !unvalidated && oldMarker != nil {
		if err := store.Remove(markerPath, oldMarker); err != nil {
			return errors.New("profile refreshed and validated, but its validation marker changed; inspect it before applying")
		}
	}
	return nil
}
