package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	msg := batch[0]()
	// Update may batch an operation together with runtime or notification
	// subscriptions. Run the operation through those wrappers before updating
	// the model; a BatchMsg itself does not execute any backend work.
	for {
		nested, ok := msg.(tea.BatchMsg)
		if !ok {
			break
		}
		if len(nested) == 0 {
			t.Fatal("empty nested operation batch")
		}
		msg = nested[0]()
	}
	if _, ok := msg.(resultMsg); !ok {
		t.Fatalf("profile operation produced %T instead of a result", msg)
	}
	m, _ = update(m, msg)
	return m
}

func TestProfileSourceInputScrollsLongPathOnOneLine(t *testing.T) {
	for _, width := range []int{160, 80, 60, 40} {
		m := ready(&fakeBackend{})
		m.width, m.height = width, 24
		m.page = "profiles"
		m.activate("add")
		m.input.SetValue("/tmp/" + strings.Repeat("long-directory/", 20) + "config-visible.yaml")
		m.layout()
		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, "config-visible.yaml") {
			t.Fatalf("width %d wrapped or hid the path tail:\n%s", width, view)
		}
		assertCanvas(t, m.View().Content, width, 24)
	}
}

func TestProfileCreateFormSubmitsRemoteMetadataAndClosesOnSuccess(t *testing.T) {
	var got app.ProfileDraft
	b := &fakeBackend{createProfile: func(_ context.Context, draft app.ProfileDraft) error {
		got = draft
		return nil
	}}
	m := ready(b)
	m.page = "profiles"
	m.activate("profiles.create")
	if m.form != "profile-create" {
		t.Fatal("new profile action did not open the dedicated form")
	}
	for _, size := range [][2]int{{120, 30}, {80, 24}, {60, 20}, {40, 16}} {
		m.width, m.height = size[0], size[1]
		m.layout()
		assertCanvas(t, m.View().Content, size[0], size[1])
	}
	m.profileCreate.name.SetValue("Travel")
	m.profileCreate.description.SetValue("personal route")
	m.profileCreate.source.SetValue("https://example.com/profile.yaml")
	m.profileCreate.field = profileCreateSource
	m.profileCreate.source.Focus()
	m, cmd := update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if got.Name != "Travel" || got.Description != "personal route" || got.Address != "https://example.com/profile.yaml" {
		t.Fatalf("unexpected profile draft: %+v", got)
	}
	if m.form != "" {
		t.Fatal("successful profile creation left the form open")
	}
}

func TestProfileToolbarKeepsSourceForRetryAndClearsItAfterSuccess(t *testing.T) {
	const source = "/tmp/example.yaml"
	var drafts []app.ProfileDraft
	m := profileInteractionsModel(&fakeBackend{createProfile: func(_ context.Context, draft app.ProfileDraft) error {
		drafts = append(drafts, draft)
		if len(drafts) == 1 {
			return errors.New("read failed")
		}
		return nil
	}}, 80, 24)
	m.activate("profiles.url")
	m, _ = update(m, tea.PasteMsg{Content: source})
	for attempt := 1; attempt <= 2; attempt++ {
		var cmd tea.Cmd
		m, cmd = click(m, findTarget(t, m, "profiles.add"))
		m = finishProfileJob(t, m, cmd)
		if attempt == 1 && m.profilesUI.url.Value() != source {
			t.Fatal("failed import lost the source")
		}
	}
	if m.profilesUI.url.Value() != "" {
		t.Fatal("successful import left a stale source")
	}
	if len(drafts) != 2 || drafts[0].Address != source || drafts[0] != drafts[1] {
		t.Fatalf("unexpected submitted drafts: %+v", drafts)
	}
}

func TestProfileImportFeedbackUsesObservedInstallation(t *testing.T) {
	for _, installed := range []bool{false, true} {
		b := &fakeBackend{snapshot: app.Snapshot{Installed: !installed}}
		m := ready(b)
		m.page = "profiles"
		b.createProfile = func(context.Context, app.ProfileDraft) error {
			b.snapshot.Installed = installed
			return nil
		}
		cmd := m.beginProfileImport(app.ProfileDraft{Name: "Work", Address: "/tmp/work.yaml"}, "profile.add", m.tr("profiles.fetching"))
		m = finishProfileJob(t, m, cmd)
		key := "feedback.profile_install_to_use"
		if installed {
			key = "feedback.profile_ready_to_use"
		}
		if want := m.tr(key, m.tr("feedback.profile_created", "Work")); m.message != want {
			t.Errorf("installed=%t: import message = %q, want %q", installed, m.message, want)
		}
	}
}

func TestProfileCreateFormRetainsDraftAfterFailure(t *testing.T) {
	m := ready(&fakeBackend{createProfile: func(context.Context, app.ProfileDraft) error {
		return errors.New("create failed")
	}})
	m.page = "profiles"
	m.activate("profiles.create")
	m.profileCreate.name.SetValue("Travel")
	m.profileCreate.source.SetValue("https://example.com/profile.yaml")
	m.profileCreate.field = profileCreateSource
	m.profileCreate.source.Focus()
	m, cmd := update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if m.form != "profile-create" || m.profileCreate.name.Value() != "Travel" || m.profileCreate.source.Value() == "" {
		t.Fatal("failed creation discarded the editable draft")
	}
}

func TestProfileCreateFormButtonsUseModalHitTargets(t *testing.T) {
	called := false
	m := ready(&fakeBackend{createProfile: func(context.Context, app.ProfileDraft) error {
		called = true
		return nil
	}})
	m.page = "profiles"
	m.width, m.height = 120, 30
	m.activate("profiles.create")
	m.profileCreate.source.SetValue("https://example.com/profile.yaml")
	m.layout()
	m, cmd := click(m, findTarget(t, m, "profile-create.save"))
	m = finishProfileJob(t, m, cmd)
	if !called || m.form != "" {
		t.Fatal("modal Save target did not submit and close the form")
	}
}

func TestProfileOverridesSaveAndRecipeDeletionRequiresConfirmation(t *testing.T) {
	raw := []byte("prepend-rules: ['DOMAIN,local.example,DIRECT']\n")
	path := filepath.Join(t.TempDir(), "overrides.yaml")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var savedName, deletedName string
	var saved []byte
	b := &fakeBackend{
		snapshot: app.Snapshot{Profiles: []string{"work"}, ProfileDetails: profileFixtures("default", "work")},
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
	m.activate("profile.overrides.0")
	m.input.SetValue(path)
	cmd := m.formKey(special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if savedName != "work" || string(saved) != string(raw) || backend.applied != 0 || m.message != m.tr("profiles.overrides_saved") {
		t.Fatal("override import applied or changed its content")
	}
	m.activate("profile.delete.0")
	if m.form != "profile-delete" || m.busy || deletedName != "" {
		t.Fatal("recipe deletion skipped confirmation")
	}
	m.profileDeleteSetFocus("profile-delete.confirm")
	cmd = m.formKey(special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if deletedName != "work" || backend.applied != 0 {
		t.Fatal("recipe deletion did not call the shared use case")
	}
}

func TestProfileOverridesRejectSymlinkAndOversizedFiles(t *testing.T) {
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
		snapshot:     app.Snapshot{Profiles: []string{"work"}, ProfileDetails: profileFixtures("default", "work")},
		setOverrides: func(context.Context, string, []byte) error { calls++; return nil },
	}
	m := ready(b)
	m.activate("profile.overrides.0")
	m.input.SetValue(link)
	cmd := m.formKey(special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if calls != 0 || !strings.Contains(m.failure, "cannot read overrides") {
		t.Fatal("invalid file reached the application", m.failure)
	}
}

func TestProfilePageShowsFailedUpdateAndAccurateActionRows(t *testing.T) {
	m := ready(&fakeBackend{snapshot: app.Snapshot{
		Installed: true, Profiles: []string{"work"}, Applied: "work", ProfilePending: true,
		ProfileDetails: []app.ProfileInfo{{ID: "default", Name: "default", Validated: true}, {ID: "work", Name: "work", Validated: true, Pending: true}},
	}})
	m.page = "profiles"
	body, _ := m.pageBody(150)
	plain := ansi.Strip(body)
	for _, text := range []string{m.tr("profiles.url_placeholder"), "⎘", m.tr("profiles.add"), m.tr("profiles.create"), m.tr("profiles.refresh_failed")} {
		if !strings.Contains(plain, text) {
			t.Fatalf("missing %q in profiles page", text)
		}
	}
	for _, width := range []int{18, 25, 40} {
		body, hits := m.pageBody(width)
		rows := strings.Split(body, "\n")
		for _, hit := range hits {
			if hit.Y < 0 || hit.H < 1 || hit.Y+hit.H > len(rows) || hit.X < 0 || hit.X+hit.W > width || ansi.StringWidth(rows[hit.Y]) > width {
				t.Fatalf("invalid action row at width %d: %+v", width, hit)
			}
		}
	}
}

func TestDeletingLastSavedProfileShowsEmptyLibrary(t *testing.T) {
	m, b := profileClickModel(t, true, true, false, false)
	b.deleteProfile = func(_ context.Context, id string) error {
		if id != "p-stable-profile-id" {
			t.Fatalf("deleted unexpected profile %q", id)
		}
		b.snapshot.Profiles = []string{"default"}
		b.snapshot.ProfileDetails = b.snapshot.ProfileDetails[:1]
		return nil
	}
	m = profileMenuRightClick(t, m, "profile.0")
	m, _ = chooseProfileMenuItem(t, m, "delete")
	m, cmd := click(m, findTarget(t, m, "profile-delete.confirm"))
	m = finishProfileJob(t, m, cmd)
	if !strings.Contains(ansi.Strip(m.View().Content), m.tr("empty.profiles.none")) || hasAction(m.targets, "profile.0") ||
		m.snapshot.Applied != "default" || !m.snapshot.Service.Active || len(b.calls) != 0 {
		t.Fatal("deleting the last saved profile should leave an empty library and preserve the running default")
	}
}
