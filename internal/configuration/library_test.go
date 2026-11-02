package configuration

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func publishedRepository(t *testing.T) (Repository, Library, []byte, []byte) {
	t.Helper()
	r := Repository{Root: filepath.Join(t.TempDir(), "configuration")}
	lib, old, err := r.Load()
	if err != nil || old != nil || len(lib.Profiles) != 0 {
		t.Fatal("fresh repository is not empty", err)
	}
	snapshot := []byte("proxies: [{name: initial, type: direct}]\n")
	revision, err := r.Put(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	lib.Profiles["primary"] = ProfileRecord{Spec: Profile{Core: "mihomo", Revision: revision}, Name: "Primary", NameSource: NameCustom, NameBase: "Primary", Address: "https://example.invalid/private-token", UpdatedAt: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)}
	if err := r.Save(lib, old); err != nil {
		t.Fatal(err)
	}
	lib, old, err = r.Load()
	if err != nil {
		t.Fatal(err)
	}
	return r, lib, old, snapshot
}

func TestRepositoryStaleEditCannotReplaceNewerProfiles(t *testing.T) {
	r, stale, staleIndex, _ := publishedRepository(t)
	latest, expected, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	latest.Profiles["secondary"] = ProfileRecord{Spec: Profile{Core: "mihomo", Revision: latest.Profiles["primary"].Spec.Revision}, Name: "Secondary", NameSource: NameCustom, NameBase: "Secondary"}
	if err := r.Save(latest, expected); err != nil {
		t.Fatal(err)
	}
	_, committed, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	delete(stale.Profiles, "primary")
	if err := r.Save(stale, staleIndex); err == nil {
		t.Fatal("stale editor discarded a newer profile")
	}
	reopened, actual, err := r.Load()
	if err != nil || !bytes.Equal(actual, committed) || len(reopened.Profiles) != 2 {
		t.Fatal("failed stale edit changed the committed library", err)
	}
}

func TestRepositoryExternalIndexEditIsPreserved(t *testing.T) {
	r, lib, old, _ := publishedRepository(t)
	path := filepath.Join(r.Root, "library.json")
	edited := []byte("user inspection: private-token\n")
	if err := os.WriteFile(path, edited, 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(lib, old); err == nil {
		t.Fatal("saved over an external edit made after the library was read")
	}
	if _, _, err := r.Load(); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("damaged index was accepted or its content leaked", err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, edited) {
		t.Fatal("damaged index was rewritten instead of preserved", err)
	}
}

func TestUncommittedSnapshotDoesNotPublishAnUpdatedProfile(t *testing.T) {
	r, lib, oldIndex, original := publishedRepository(t)
	next := []byte("proxies: [{name: candidate, type: direct}]\n")
	revision, err := r.Put(next)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate interruption after the durable blob write, before index commit.
	// A new repository instance must observe only the last complete relationship.
	reopened, index, err := (Repository{Root: r.Root}).Load()
	if err != nil || !bytes.Equal(index, oldIndex) || reopened.Profiles["primary"].Spec.Revision != lib.Profiles["primary"].Spec.Revision {
		t.Fatal("uncommitted blob changed the published profile", err)
	}
	published, err := r.Read(reopened.Profiles["primary"].Spec.Revision)
	if err != nil || !bytes.Equal(published, original) {
		t.Fatal("interruption lost the last usable snapshot", err)
	}
	candidate, err := r.Read(revision)
	if err != nil || !bytes.Equal(candidate, next) {
		t.Fatal("uncommitted snapshot cannot be inspected or reused", err)
	}
	for _, path := range []string{r.Root, filepath.Join(r.Root, "snapshots"), filepath.Join(r.Root, "library.json"), filepath.Join(r.Root, "snapshots", revision+".yaml")} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatal("private configuration material is accessible to other users", err)
		}
	}
}

func TestCorruptedSnapshotCannotBeReadOrSilentlyRepaired(t *testing.T) {
	r, lib, oldIndex, original := publishedRepository(t)
	revision := lib.Profiles["primary"].Spec.Revision
	path := filepath.Join(r.Root, "snapshots", revision+".yaml")
	damaged := []byte("private-token: externally-modified\n")
	if err := os.WriteFile(path, damaged, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Read(revision); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("corrupted content was accepted or leaked", err)
	}
	if _, err := r.Put(original); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("publishing the original bytes overwrote a changed snapshot", err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, damaged) {
		t.Fatal("damaged snapshot was not preserved for inspection", err)
	}
	_, index, err := r.Load()
	if err != nil || !bytes.Equal(index, oldIndex) {
		t.Fatal("snapshot corruption rewrote committed profiles", err)
	}
}

func TestRepositoryRejectsInvalidProfileMetadataBeforePublishing(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*ProfileRecord)
	}{
		{"missing-core", func(record *ProfileRecord) { record.Spec.Core = "" }},
		{"missing-revision", func(record *ProfileRecord) { record.Spec.Revision = "" }},
		{"invalid-revision", func(record *ProfileRecord) { record.Spec.Revision = "../private-token" }},
		{"oversized-overrides", func(record *ProfileRecord) { record.Spec.Overrides = strings.Repeat("x", MaxDocumentSize+1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, lib, original, _ := publishedRepository(t)
			record := lib.Profiles["primary"]
			tc.edit(&record)
			lib.Profiles["primary"] = record
			if err := r.Save(lib, original); err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatal("invalid metadata was published or sensitive input leaked", err)
			}
			_, actual, err := r.Load()
			if err != nil || !bytes.Equal(actual, original) {
				t.Fatal("rejected metadata changed the committed index", err)
			}
		})
	}
}

func TestProfileDescriptionIsNormalizedAndPersistedAsMetadata(t *testing.T) {
	r, lib, expected, _ := publishedRepository(t)
	record := lib.Profiles["primary"]
	record.Description = CleanDescription("  Daily\n  route\tprofile  ")
	lib.Profiles["primary"] = record
	if err := r.Save(lib, expected); err != nil {
		t.Fatal(err)
	}
	reopened, _, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Profiles["primary"].Description; got != "Daily route profile" {
		t.Fatalf("description = %q", got)
	}
}

func TestRepositoryRejectsUncleanProfileDescription(t *testing.T) {
	r, lib, expected, _ := publishedRepository(t)
	record := lib.Profiles["primary"]
	record.Description = "line\nbreak"
	lib.Profiles["primary"] = record
	if err := r.Save(lib, expected); err == nil {
		t.Fatal("unclean description was published")
	}
}

func TestRepositoryRejectsUnknownFieldsWithoutRewritingTheIndex(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"independent sources", func(value map[string]any) { value["subscriptions"] = map[string]any{} }},
		{"composition mode", func(value map[string]any) {
			value["profiles"].(map[string]any)["primary"].(map[string]any)["spec"].(map[string]any)["mode"] = "managed"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, original, _ := publishedRepository(t)
			var value map[string]any
			if err := json.Unmarshal(original, &value); err != nil {
				t.Fatal(err)
			}
			tc.edit(value)
			invalid, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(r.Root, "library.json")
			if err := os.WriteFile(path, invalid, 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := r.Load(); err == nil {
				t.Fatal("unsupported structure was accepted")
			}
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, invalid) {
				t.Fatal("unsupported structure was rewritten", err)
			}
		})
	}
}

func TestDeletingProfilePreservesItsPinnedSnapshot(t *testing.T) {
	r, lib, expected, original := publishedRepository(t)
	revision := lib.Profiles["primary"].Spec.Revision
	delete(lib.Profiles, "primary")
	if err := r.Save(lib, expected); err != nil {
		t.Fatal(err)
	}
	reopened, _, err := r.Load()
	if err != nil || len(reopened.Profiles) != 0 {
		t.Fatal("deleted profile remains in the library", err)
	}
	snapshot, err := r.Read(revision)
	if err != nil || !bytes.Equal(snapshot, original) {
		t.Fatal("removing a profile destroyed a historical runtime input", err)
	}
}
