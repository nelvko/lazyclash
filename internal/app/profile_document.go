package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

type ProfileDocumentKind string

const (
	ProfileConfiguration ProfileDocumentKind = "configuration"
	ProfileOverrides     ProfileDocumentKind = "overrides"
)

// ProfileDocument holds one reviewed editor session. Callers keep the original
// bytes and submit the editor's output separately, including after a failed save.
type ProfileDocument struct {
	ID        string
	Kind      ProfileDocumentKind
	LocalPath string `json:"-"`
	Remote    bool
	Bytes     []byte `json:"-"`
	reviewed  string
}

func documentToken(id string, kind ProfileDocumentKind, entry configuration.ProfileRecord) string {
	fields := []string{id, string(kind), entry.Spec.Revision, entry.Spec.Overrides, entry.Address}
	raw, _ := json.Marshal(fields)
	return digest(raw)
}

func (a *App) editableOriginal(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return diagnostic.New("diagnostic.source.readFile", "cannot read subscription file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	real, err := filepath.EvalSymlinks(path)
	if err != nil || !ok || stat.Nlink != 1 {
		return diagnostic.New("diagnostic.profile.unsafeEditPath", "choose an independent source file outside managed application files")
	}
	// Compare filesystem identity rather than a text prefix: the source may
	// use a symlinked ancestor or different casing on a case-insensitive disk.
	// User directories such as Home/subscriptions remain editable.
	for item := real; item != filepath.Dir(item); item = filepath.Dir(item) {
		name := strings.ToLower(filepath.Base(item))
		switch name {
		case "configuration", "installation", ".lock", "preferences.json", "runtime-state.json", "stop-intent.json",
			"apply-pending.json", "core-pending.json", "install-pending.json", profileEditJournalName:
			managed, err := os.Stat(filepath.Join(a.Home, name))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			actual, statErr := os.Stat(item)
			if err != nil || statErr != nil || os.SameFile(managed, actual) {
				return diagnostic.New("diagnostic.profile.unsafeEditPath", "choose an independent source file outside managed application files")
			}
		}
	}
	return nil
}

func (a *App) GetProfileDocument(ctx context.Context, id string, kind ProfileDocumentKind) (ProfileDocument, error) {
	var doc ProfileDocument
	err := a.locked(ctx, func() error {
		lib, _, err := a.library().Load()
		if err != nil {
			return err
		}
		entry, ok := lib.Profiles[id]
		if !ok {
			return diagnostic.New("diagnostic.profile.missing", "profile does not exist")
		}
		doc = ProfileDocument{ID: id, Kind: kind, reviewed: documentToken(id, kind, entry)}
		switch kind {
		case ProfileConfiguration:
			doc.Remote = configuration.IsRemoteSource(entry.Address)
			if !doc.Remote {
				if err := a.editableOriginal(entry.Address); err != nil {
					return err
				}
				doc.LocalPath = entry.Address
				doc.Bytes, err = store.Read(doc.LocalPath)
			} else {
				doc.Bytes, err = a.library().Read(entry.Spec.Revision)
			}
		case ProfileOverrides:
			doc.Bytes = []byte(entry.Spec.Overrides)
		default:
			return invalidDocumentKind()
		}
		if err == nil && len(doc.Bytes) > configuration.MaxDocumentSize {
			return diagnostic.New("diagnostic.library.documentSize", "configuration must be nonempty and at most 8 MiB")
		}
		return err
	})
	if err != nil {
		return ProfileDocument{}, err
	}
	return doc, nil
}

func invalidDocumentKind() error {
	return diagnostic.New("diagnostic.profile.documentKind", "unknown editable profile document")
}
func documentConflict() error {
	return diagnostic.New("diagnostic.profile.documentConflict", "profile or source changed while editing; reopen the document to review its latest content")
}

func (a *App) SaveProfileDocument(ctx context.Context, doc ProfileDocument, edited []byte) error {
	return a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		entry, ok := lib.Profiles[doc.ID]
		if !ok {
			return diagnostic.New("diagnostic.profile.missing", "profile does not exist")
		}
		if doc.reviewed == "" || doc.reviewed != documentToken(doc.ID, doc.Kind, entry) {
			return documentConflict()
		}
		previous := entry
		if bytes.Equal(edited, doc.Bytes) {
			return nil
		}
		switch doc.Kind {
		case ProfileConfiguration:
			if len(edited) > configuration.MaxDocumentSize {
				return diagnostic.New("diagnostic.library.documentSize", "configuration must be nonempty and at most 8 MiB")
			}
			if filepath.IsAbs(entry.Address) {
				if err := a.editableOriginal(entry.Address); err != nil {
					return err
				}
				disk, err := store.Read(entry.Address)
				if err != nil {
					return err
				}
				if !bytes.Equal(disk, edited) {
					return documentConflict()
				}
			}
			if err := mihomo.ValidateSource(edited); err != nil {
				return err
			}
			revision, err := a.library().Put(edited)
			if err != nil {
				return err
			}
			entry.Spec.Revision = revision
		case ProfileOverrides:
			if len(edited) > configuration.MaxDocumentSize {
				return diagnostic.New("diagnostic.profile.overridesSize", "overrides exceed 8 MiB")
			}
			entry.Spec.Overrides = string(edited)
		default:
			return invalidDocumentKind()
		}
		lib.Profiles[doc.ID] = entry
		return a.saveProfileEdit(ctx, lib, old, doc.ID, previous)
	})
}
