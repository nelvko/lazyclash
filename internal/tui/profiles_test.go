package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

type profileBackend struct {
	*fakeBackend
	applied int
}

func (b *profileBackend) Apply(context.Context, string) error {
	b.applied++
	return nil
}

func finishProfileJob(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	batch := commandBatch(t, cmd)
	m, _ = update(m, batch[0]())
	return m
}

func submitImport(t *testing.T, m Model, name, source string) (Model, tea.Cmd) {
	t.Helper()
	m.input.SetValue(name)
	m, _ = update(m, special(tea.KeyEnter))
	if m.form != "source" || m.input.EchoMode != textinput.EchoPassword {
		t.Fatal("source input was not hidden")
	}
	m.input.SetValue(source)
	m.layout()
	if strings.Contains(ansi.Strip(m.View().Content), source) {
		t.Fatal("source address leaked in the import dialog")
	}
	return update(m, special(tea.KeyEnter))
}

func TestProfileImportExplicitlyRemembersOnlySubscriptionAction(t *testing.T) {
	imports, remembered := 0, 0
	b := &fakeBackend{
		add: func(_ context.Context, name, source string) error {
			imports++
			if name != "copied" || source != "https://example.com/private-token" {
				t.Fatal("one-time import arguments changed")
			}
			return nil
		},
		addSubscription: func(_ context.Context, name, source string) error {
			remembered++
			if name != "refreshable" || source != "https://example.com/private-token" {
				t.Fatal("remembered import arguments changed")
			}
			return nil
		},
	}
	m := ready(b)
	m.page = "profiles"
	m.activate("subscription.add")
	m.layout()
	if !m.rememberSource || !strings.Contains(ansi.Strip(m.View().Content), "private source URL/path will be saved") {
		t.Fatal("remembered import did not disclose private source persistence")
	}
	m, cmd := submitImport(t, m, "refreshable", "https://example.com/private-token")
	m = finishProfileJob(t, m, cmd)
	if remembered != 1 || imports != 0 || m.rememberSource || m.form != "" || m.input.Value() != "" {
		t.Fatal("remembered import was not submitted and cleared")
	}
	m.activate("subscription.add")
	m, _ = update(m, special(tea.KeyEsc))
	if m.rememberSource {
		t.Fatal("cancel retained remember-source state")
	}
	m.activate("add")
	m, cmd = submitImport(t, m, "copied", "https://example.com/private-token")
	m = finishProfileJob(t, m, cmd)
	if remembered != 1 || imports != 1 || m.failure != "" {
		t.Fatal("one-time import inherited remember-source behavior", m.failure)
	}
}

func TestProfileSourceRefreshAndConfirmedDeletionUseSharedBackend(t *testing.T) {
	var refreshed, deleted string
	b := &fakeBackend{
		snapshot:      app.Snapshot{Sources: []app.SourceInfo{{Name: "airport", Refreshable: true}}},
		refreshSource: func(_ context.Context, name string) error { refreshed = name; return nil },
		deleteSource: func(_ context.Context, name string) error {
			deleted = name
			return errors.New("subscription is referenced by a profile")
		},
	}
	backend := &profileBackend{fakeBackend: b}
	m := ready(b)
	m.backend = backend
	m.page = "profiles"
	cmd := m.activate("profile.source.refresh.0")
	m = finishProfileJob(t, m, cmd)
	if refreshed != "airport" || backend.applied != 0 || !strings.Contains(m.message, "apply a profile") {
		t.Fatal("source refresh selected or applied a profile")
	}
	m.activate("profile.source.delete.0")
	m.input.SetValue("wrong")
	if cmd := m.formKey(special(tea.KeyEnter)); cmd != nil || deleted != "" || m.form != "source-delete" {
		t.Fatal("source deletion skipped exact-name confirmation")
	}
	m.input.SetValue("airport")
	cmd = m.formKey(special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if deleted != "airport" || !strings.Contains(m.failure, "referenced") || backend.applied != 0 {
		t.Fatal("source deletion did not preserve backend protection", m.failure)
	}
}

func TestProfileOverridesSaveDraftAndRecipeDeletionRequiresName(t *testing.T) {
	raw := []byte("prepend-rules: ['DOMAIN,local.example,DIRECT']\n")
	path := filepath.Join(t.TempDir(), "overrides.yaml")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var savedName, deletedName string
	var saved []byte
	b := &fakeBackend{
		snapshot: app.Snapshot{Profiles: []string{"work"}},
		setOverrides: func(_ context.Context, name string, data []byte) error {
			savedName, saved = name, data
			return nil
		},
		deleteProfile: func(_ context.Context, name string) error { deletedName = name; return nil },
	}
	backend := &profileBackend{fakeBackend: b}
	m := ready(b)
	m.backend = backend
	m.page = "profiles"
	m.activate("profile.overrides.1")
	m.input.SetValue(path)
	cmd := m.formKey(special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if savedName != "work" || string(saved) != string(raw) || backend.applied != 0 || !strings.Contains(m.message, "apply the profile") {
		t.Fatal("override import applied or changed its content")
	}
	m.activate("profile.delete.1")
	m.input.SetValue("other")
	if cmd := m.formKey(special(tea.KeyEnter)); cmd != nil || deletedName != "" {
		t.Fatal("recipe deletion skipped confirmation")
	}
	m.input.SetValue("work")
	cmd = m.formKey(special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if deletedName != "work" || backend.applied != 0 {
		t.Fatal("recipe deletion did not call the shared use case")
	}
}

func TestProfileOverridesRejectRelativeSymlinkAndOversizedFiles(t *testing.T) {
	if _, err := readProfileOverrides("relative.yaml"); err == nil {
		t.Fatal("accepted relative override path")
	}
	path := filepath.Join(t.TempDir(), "overrides.yaml")
	if err := os.WriteFile(path, []byte("set: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "overrides-link.yaml")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readProfileOverrides(link); err == nil {
		t.Fatal("accepted symlink override path")
	}
	if err := os.WriteFile(path, make([]byte, (8<<20)+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProfileOverrides(path); err == nil {
		t.Fatal("accepted oversized override input")
	}
	calls := 0
	b := &fakeBackend{
		snapshot:     app.Snapshot{Profiles: []string{"work"}},
		setOverrides: func(context.Context, string, []byte) error { calls++; return nil },
	}
	m := ready(b)
	m.activate("profile.overrides.1")
	m.input.SetValue("relative.yaml")
	cmd := m.formKey(special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if calls != 0 || !strings.Contains(m.failure, "absolute") {
		t.Fatal("invalid file reached the application", m.failure)
	}
}

func TestProfilePageShowsPendingSourcesAndAccurateActionRows(t *testing.T) {
	m := ready(&fakeBackend{snapshot: app.Snapshot{
		Installed: true, Profiles: []string{"work"}, Applied: "work", ProfilePending: true,
		Sources: []app.SourceInfo{{Name: "refreshable", Refreshable: true}, {Name: "copy"}},
	}})
	m.page = "profiles"
	body, hits := m.pageBody(150)
	plain := ansi.Strip(body)
	for _, text := range []string{"One-time import", "Add subscription (remember source)", "work [applied] [changes pending; apply to load]", "Import overrides from file", "Subscription sources", "refreshable", "copy"} {
		if !strings.Contains(plain, text) {
			t.Fatalf("missing %q in profiles page", text)
		}
	}
	for _, hit := range hits {
		if hit.ID == "profile.overrides.0" || hit.ID == "profile.delete.0" || hit.ID == "profile.source.refresh.1" {
			t.Fatal("built-in edit/delete or nonrefreshable source action was offered", hit.ID)
		}
	}
	for _, width := range []int{18, 25, 40} {
		body, hits := m.pageBody(width)
		rows := strings.Split(body, "\n")
		for _, hit := range hits {
			if hit.Y < 0 || hit.Y >= len(rows) || ansi.StringWidth(rows[hit.Y]) > width || hit.W != width {
				t.Fatalf("invalid action row at width %d: %+v", width, hit)
			}
		}
	}
	if handled, cmd := m.profileAction("profile.source.refresh.99"); !handled || cmd != nil {
		t.Fatal("stale source action was not ignored")
	}
}
