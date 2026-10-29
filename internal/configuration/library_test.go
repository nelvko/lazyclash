package configuration

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func publishedRepository(t *testing.T) (Repository, Library, []byte, []byte) {
	t.Helper()
	r := Repository{Root: filepath.Join(t.TempDir(), "configuration")}
	lib, old, err := r.Load()
	if err != nil || old != nil || len(lib.Subscriptions) != 0 || len(lib.Profiles) != 0 {
		t.Fatal("fresh repository is not empty", err)
	}
	snapshot := []byte("proxies: [{name: initial, type: direct}]\n")
	revision, err := r.Put(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	lib.Subscriptions["source"] = Subscription{Format: "mihomo", Address: "https://source.invalid/private-token", Revision: revision}
	lib.Profiles["primary"] = ProfileRecord{Spec: Profile{Core: "mihomo", Mode: "full", Source: "source"}}
	if err := r.Save(lib, old); err != nil {
		t.Fatal(err)
	}
	lib, old, err = r.Load()
	if err != nil {
		t.Fatal(err)
	}
	return r, lib, old, snapshot
}

func TestRepositoryStaleEditCannotReplaceNewerRelationships(t *testing.T) {
	r, stale, staleIndex, _ := publishedRepository(t)
	latest, expected, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	latest.Profiles["secondary"] = ProfileRecord{Spec: Profile{Core: "mihomo", Mode: "full", Source: "source"}}
	if err := r.Save(latest, expected); err != nil {
		t.Fatal(err)
	}
	_, committed, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	delete(stale.Profiles, "primary")
	delete(stale.Subscriptions, "source")
	if err := r.Save(stale, staleIndex); err == nil {
		t.Fatal("stale editor discarded a newer profile and its dependency")
	}
	reopened, actual, err := r.Load()
	if err != nil || !bytes.Equal(actual, committed) || len(reopened.Profiles) != 2 || len(reopened.Subscriptions) != 1 {
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

func TestUncommittedSnapshotDoesNotPublishAnUpdatedSubscription(t *testing.T) {
	r, lib, oldIndex, original := publishedRepository(t)
	next := []byte("proxies: [{name: candidate, type: direct}]\n")
	revision, err := r.Put(next)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate interruption after the durable blob write, before index commit.
	// A new repository instance must observe only the last complete relationship.
	reopened, index, err := (Repository{Root: r.Root}).Load()
	if err != nil || !bytes.Equal(index, oldIndex) || reopened.Subscriptions["source"].Revision != lib.Subscriptions["source"].Revision {
		t.Fatal("uncommitted blob changed the published subscription", err)
	}
	published, err := r.Read(reopened.Subscriptions["source"].Revision)
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
	revision := lib.Subscriptions["source"].Revision
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
		t.Fatal("snapshot corruption rewrote committed source relationships", err)
	}
}

func TestRepositoryRejectsInvalidProfileRelationshipsBeforePublishing(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Library)
	}{
		{"deleted-dependency", func(lib *Library) { delete(lib.Subscriptions, "source") }},
		{"missing-dependency", func(lib *Library) {
			lib.Profiles["primary"] = ProfileRecord{Spec: Profile{Core: "mihomo", Mode: "full", Source: "missing"}}
		}},
		{"duplicate-managed-source", func(lib *Library) {
			lib.Profiles["primary"] = ProfileRecord{Spec: Profile{Core: "mihomo", Mode: "managed", Sources: []string{"source", "source"}, Template: "rules: ['MATCH,DIRECT']"}}
		}},
		{"mixed-mode-fields", func(lib *Library) {
			lib.Profiles["primary"] = ProfileRecord{Spec: Profile{Core: "mihomo", Mode: "full", Source: "source", Template: "rules: ['MATCH,DIRECT']"}}
		}},
		{"managed-without-template", func(lib *Library) {
			lib.Profiles["primary"] = ProfileRecord{Spec: Profile{Core: "mihomo", Mode: "managed", Sources: []string{"source"}}}
		}},
		{"unknown-mode", func(lib *Library) {
			lib.Profiles["primary"] = ProfileRecord{Spec: Profile{Core: "mihomo", Mode: "other", Source: "source"}}
		}},
		{"invalid-revision", func(lib *Library) {
			lib.Subscriptions["source"] = Subscription{Format: "mihomo", Revision: "../private-token"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, lib, original, _ := publishedRepository(t)
			tc.edit(&lib)
			if err := r.Save(lib, original); err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatal("invalid relationship was published or sensitive input leaked", err)
			}
			_, actual, err := r.Load()
			if err != nil || !bytes.Equal(actual, original) {
				t.Fatal("rejected relationship changed the committed index", err)
			}
		})
	}
}
