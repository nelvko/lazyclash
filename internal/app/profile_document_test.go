package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/nelvko/lazyclash/internal/configuration"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentSourceEditingUsesOriginalOrPrivateRevision(t *testing.T) {
	for _, kind := range []string{"local", "remote"} {
		t.Run(kind, func(t *testing.T) {
			a, svc := testApp(t)
			const source = "rules: ['MATCH,DIRECT']\n"
			const updated = "rules: ['MATCH,REJECT']\n"
			initial := []byte(source)
			path := filepath.Join(t.TempDir(), "source.yaml")
			write(t, path, initial)
			addr := path
			if kind == "remote" {
				server, _, _ := namingServer(t, namingResponse{body: source})
				a.HTTP = server.Client()
				addr = server.URL + "/private?token=secret"
			}
			if err := a.CreateProfile(t.Context(), ProfileDraft{Name: "work", Address: addr}); err != nil {
				t.Fatal(err)
			}
			if kind == "local" {
				initial = []byte("# latest disk contents\n" + source)
				write(t, path, initial)
			}
			doc, err := a.GetProfileDocument(t.Context(), "work", ProfileConfiguration)
			if err != nil || !bytes.Equal(doc.Bytes, initial) || doc.Remote != (kind == "remote") || (doc.LocalPath == path) != (kind == "local") {
				t.Fatal("incorrect editor source", err)
			}
			_, index, _ := a.library().Load()
			if err := a.SaveProfileDocument(t.Context(), doc, doc.Bytes); err != nil {
				t.Fatal(err)
			}
			if _, next, _ := a.library().Load(); !bytes.Equal(index, next) {
				t.Fatal("unchanged exit published metadata")
			}
			if kind == "local" {
				if err := a.SaveProfileDocument(t.Context(), doc, []byte(updated)); err == nil {
					t.Fatal("edited bytes did not match original file")
				}
				write(t, path, []byte(updated))
			}
			if err := a.SaveProfileDocument(t.Context(), doc, []byte(updated)); err != nil {
				t.Fatal(err)
			}
			lib, _, err := a.library().Load()
			if err != nil || lib.Profiles["work"].Spec.Revision != digest([]byte(updated)) || len(svc.actions) != 0 {
				t.Fatal("edit not saved independently", err)
			}
			if old, err := a.library().Read(digest([]byte(source))); err != nil || string(old) != source {
				t.Fatal("prior snapshot was edited", err)
			}
			if kind == "remote" {
				if err := a.Refresh(t.Context(), "work"); err != nil {
					t.Fatal(err)
				}
				if got, e := a.GetProfileDocument(t.Context(), "work", ProfileConfiguration); e != nil || string(got.Bytes) != source {
					t.Fatal("provider did not replace edited subscription", e)
				}
			}
		})
	}
}

func TestDocumentSaveRestoresRuntimeAndLibraryAndRetriesOriginalEdit(t *testing.T) {
	a, svc := installedApp(t)
	addFixtureProfile(t, a, "work")
	if err := a.Apply(t.Context(), "work"); err != nil {
		t.Fatal(err)
	}
	svc.active = true
	doc, err := a.GetProfileDocument(t.Context(), "work", ProfileOverrides)
	if err != nil {
		t.Fatal(err)
	}
	before, target, _ := a.load()
	_, index, _ := a.library().Load()
	puts := 0
	a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		resp := proxyTestResponse(r, `{"version":"v1.19.33"}`)
		if r.Method == http.MethodPut {
			puts++
			if puts == 1 {
				resp.StatusCode = 400
			} else {
				resp.StatusCode = 204
			}
		}
		return resp, nil
	})}
	raw := []byte("prepend-rules: ['DOMAIN,second.invalid,REJECT']")
	if err := a.SaveProfileDocument(t.Context(), doc, raw); err == nil {
		t.Fatal("rejected reload reported success")
	}
	_, after, _ := a.library().Load()
	if m, old, err := a.load(); err != nil || old != target || m.SHA != before.SHA || !bytes.Equal(index, after) || puts != 2 || len(svc.actions) > 0 {
		t.Fatal("failed edit did not restore original recipe and library", err)
	}
	if err := a.SaveProfileDocument(t.Context(), doc, raw); err != nil || puts != 3 {
		t.Fatal("failed edit could not retry unchanged editor session", err)
	}
	if err := a.SaveProfileDocument(t.Context(), doc, []byte("set: {log-level: warning}")); err == nil {
		t.Fatal("stale editor overwrote later revision")
	}
	assertMissing(t, filepath.Join(a.Home, "profile-edit-pending.json"))
}

func TestDocumentEffectiveEqualityDoesNotContactService(t *testing.T) {
	a, svc := installedApp(t)
	path := filepath.Join(t.TempDir(), "src.yaml")
	write(t, path, []byte("rules: ['MATCH,DIRECT']\n"))
	if err := a.AddSubscription(t.Context(), "work", path); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "work"); err != nil {
		t.Fatal(err)
	}
	doc, err := a.GetProfileDocument(t.Context(), "work", ProfileConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	svc.verifyErr = errors.New("must not contact service")
	a.Validate = func(context.Context, string, string, string) error {
		t.Fatal("identical effective config launched engine")
		return nil
	}
	raw := append([]byte("mixed-port: 7899\n# different source\n"), doc.Bytes...)
	write(t, path, raw)
	if err := a.SaveProfileDocument(t.Context(), doc, raw); err != nil {
		t.Fatal(err)
	}
	if m, _, err := a.load(); err != nil || m.Build.Recipe.Revision != digest(raw) {
		t.Fatal("mixed-port-only change lost new pin", err)
	}
}

func TestLocalDocumentRejectsManagedAliasesAndHardLinks(t *testing.T) {
	a, _ := testApp(t)
	path := filepath.Join(t.TempDir(), "source.yaml")
	write(t, path, []byte("rules: ['MATCH,DIRECT']\n"))
	if err := a.AddSubscription(t.Context(), "work", path); err != nil {
		t.Fatal(err)
	}
	lib, _, _ := a.library().Load()
	snapshot := filepath.Join(a.library().Root, "snapshots", lib.Profiles["work"].Spec.Revision+".yaml")
	links := t.TempDir()
	if err := os.Symlink(filepath.Dir(snapshot), filepath.Join(links, "dir")); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(links, "hard.yaml")
	if err := os.Link(snapshot, hard); err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{snapshot, filepath.Join(links, "dir", filepath.Base(snapshot)), hard} {
		if err := a.UpdateProfileAddress(t.Context(), "work", addr); err != nil {
			t.Fatal(err)
		}
		if _, err := a.GetProfileDocument(t.Context(), "work", ProfileConfiguration); err == nil {
			t.Fatal("exposed managed input for in-place editing")
		}
	}
}

func TestLocalDocumentAllowsUserSubscriptionsInsideHome(t *testing.T) {
	a, _ := testApp(t)
	dir := filepath.Join(a.Home, "subscriptions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "office.yaml")
	write(t, path, []byte("rules: ['MATCH,DIRECT']\n"))
	if err := a.AddSubscription(t.Context(), "work", path); err != nil {
		t.Fatal(err)
	}
	doc, err := a.GetProfileDocument(t.Context(), "work", ProfileConfiguration)
	if err != nil || doc.LocalPath != path {
		t.Fatal("user source was not editable", err)
	}
	changed := []byte("rules: ['MATCH,REJECT']\n")
	write(t, path, changed)
	if err := a.SaveProfileDocument(t.Context(), doc, changed); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".lock", "preferences.json", "runtime-state.json", "stop-intent.json", profileEditJournalName} {
		reserved := filepath.Join(a.Home, name)
		if name != ".lock" {
			write(t, reserved, []byte("{}"))
		}
		if err := a.editableOriginal(reserved); err == nil {
			t.Fatalf("managed %s exposed for editing", name)
		}
	}
}

func TestInterruptedProfileEditsReconcileIndexToConfirmedGeneration(t *testing.T) {
	for _, step := range []string{"before-index", "after-index", "confirmed", "external-change"} {
		t.Run(step, func(t *testing.T) {
			a, _ := installedApp(t)
			addFixtureProfile(t, a, "work")
			if err := a.Apply(t.Context(), "work"); err != nil {
				t.Fatal(err)
			}
			m, target, _ := a.load()
			lib, old, _ := a.library().Load()
			prev := lib.Profiles["work"]
			next := prev
			next.Spec.Overrides = "set: {log-level: debug}"
			lib.Profiles["work"] = next
			cfg, record, err := a.buildProfile(t.Context(), lib, "work", m, false)
			if err != nil {
				t.Fatal(err)
			}
			p := profileEditJournal{m.ID, "work", m.Build.fingerprint(), record.fingerprint(), recordHash(next), prev}
			raw, _ := json.Marshal(p)
			file := filepath.Join(a.Home, "profile-edit-pending.json")
			write(t, file, raw)
			if step != "before-index" {
				if step == "external-change" {
					next.Description = "independent edit"
					lib.Profiles["work"] = next
				}
				if err := a.library().Save(lib, old); err != nil {
					t.Fatal(err)
				}
			}
			if step == "confirmed" {
				m.Build = &record
				if err := a.publishProfileRefresh(t.Context(), target, m, cfg); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.locked(t.Context(), func() error { return nil }); (err != nil) != (step == "external-change") {
				t.Fatal(step, err)
			}
			current, _, err := a.library().Load()
			if err != nil {
				t.Fatal(err)
			}
			want := prev
			if step == "confirmed" || step == "external-change" {
				want = next
			}
			if current.Profiles["work"] != want {
				t.Fatal("unexpected record after recovery")
			}
			if step != "external-change" {
				assertMissing(t, file)
			}
		})
	}
}

func TestEditorRejectsInvalidAndOversizedEditsWithoutPublishing(t *testing.T) {
	a, _ := testApp(t)
	addFixtureProfile(t, a, "work")
	for _, kind := range []ProfileDocumentKind{ProfileConfiguration, ProfileOverrides} {
		doc, err := a.GetProfileDocument(t.Context(), "work", kind)
		if err != nil {
			t.Fatal(err)
		}
		_, old, _ := a.library().Load()
		for _, edited := range [][]byte{[]byte("yaml: [invalid"), bytes.Repeat([]byte("x"), configuration.MaxDocumentSize+1)} {
			if err := a.SaveProfileDocument(t.Context(), doc, edited); err == nil {
				t.Fatal("invalid edit accepted", kind)
			}
			_, after, _ := a.library().Load()
			if !bytes.Equal(old, after) {
				t.Fatal("invalid edit published to library")
			}
		}
	}
}

func TestDocumentBindsSavedRecipeAndChecksSelectionAtSave(t *testing.T) {
	a, svc := installedApp(t)
	addFixtureProfile(t, a, "first")
	addFixtureProfile(t, a, "second")
	if err := a.Apply(t.Context(), "first"); err != nil {
		t.Fatal(err)
	}
	doc, err := a.GetProfileDocument(t.Context(), "first", ProfileOverrides)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "second"); err != nil {
		t.Fatal(err)
	}
	reloads := observeActiveRefresh(t, a, svc, nil)
	before, target, _ := a.load()
	raw := []byte("set: {log-level: debug}")
	if err := a.SaveProfileDocument(t.Context(), doc, raw); err != nil {
		t.Fatal(err)
	}
	if after, current, err := a.load(); err != nil || target != current || after.Applied != before.Applied || *reloads != 0 || len(svc.actions) > 0 {
		t.Fatal("saving noncurrent item disturbed active service", err)
	}
	got, err := a.GetProfileDocument(t.Context(), "first", ProfileOverrides)
	if err != nil || !bytes.Equal(got.Bytes, raw) {
		t.Fatal("noncurrent item not saved", err)
	}
	if err := a.SaveProfileDocument(t.Context(), doc, []byte("set: {log-level: error}")); err == nil {
		t.Fatal("stale document replaced saved overrides")
	}
}

func TestProfileEditRejectsUnreadableUndoRecordBeforePublishing(t *testing.T) {
	a, _ := installedApp(t)
	addFixtureProfile(t, a, "work")
	if err := a.Apply(t.Context(), "work"); err != nil {
		t.Fatal(err)
	}
	// A legal old index can be just under the store limit while a journal
	// wrapping that record with three fingerprints exceeds it.
	lib, old, err := a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	previous := lib.Profiles["work"]
	previous.Address = "https://example.invalid/"
	lib.Profiles["work"] = previous
	raw, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	previous.Address += strings.Repeat("x", (16<<20)-len(raw))
	lib.Profiles["work"] = previous
	if err := a.library().Save(lib, old); err != nil {
		t.Fatal(err)
	}
	lib, old, err = a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	next := previous
	next.Address = "https://example.invalid/new"
	next.Spec.Overrides = "set: {log-level: debug}"
	lib.Profiles["work"] = next
	_, before, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	err = a.saveProfileEdit(t.Context(), lib, old, "work", previous)
	customRulesDiagnostic(t, err, "diagnostic.store.fileSize")
	assertMissing(t, filepath.Join(a.Home, profileEditJournalName))
	if _, current, err := a.load(); err != nil || current != before {
		t.Fatal("rejected edit changed runtime", err)
	}
	if lib, _, err := a.library().Load(); err != nil || lib.Profiles["work"] != previous {
		t.Fatal("rejected edit changed library", err)
	}
}
