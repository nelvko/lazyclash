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

func submitImport(t *testing.T, m Model, source string) (Model, tea.Cmd) {
	t.Helper()
	if m.form != "source" || m.input.EchoMode != textinput.EchoNormal {
		t.Fatal("source input is not editable as visible text")
	}
	m.input.SetValue(source)
	m.layout()
	if !strings.Contains(ansi.Strip(m.View().Content), source) {
		t.Fatal("source address is not visible in the import dialog")
	}
	return update(m, special(tea.KeyEnter))
}

func TestProfileSourceInputScrollsLongPathOnOneLine(t *testing.T) {
	for _, action := range []string{"add", "subscription.add"} {
		for _, width := range []int{160, 80, 60, 40} {
			m := ready(&fakeBackend{})
			m.width, m.height = width, 24
			m.page = "profiles"
			m.activate(action)
			m.input.SetValue("/tmp/" + strings.Repeat("long-directory/", 20) + "config-visible.yaml")
			m.layout()
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "config-visible.yaml") {
				t.Fatalf("%s at width %d wrapped or hid the path tail:\n%s", action, width, view)
			}
			assertCanvas(t, m.View().Content, width, 24)
		}
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
	if got.Name != "Travel" || got.Description != "personal route" || got.Address != "https://example.com/profile.yaml" || !got.RememberSource {
		t.Fatalf("unexpected profile draft: %+v", got)
	}
	if m.form != "" {
		t.Fatal("successful profile creation left the form open")
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

func TestProfileDescriptionEditUsesMetadataUseCase(t *testing.T) {
	var gotID, gotDescription string
	b := &fakeBackend{snapshot: app.Snapshot{Profiles: []string{"work"}, ProfileDetails: profileFixtures("default", "work")}, updateDescription: func(_ context.Context, id, description string) error {
		gotID, gotDescription = id, description
		return nil
	}}
	m := ready(b)
	m.page = "profiles"
	m.openProfileEdit("profile-description", "work")
	m.input.SetValue("office route")
	m, cmd := update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if gotID != "work" || gotDescription != "office route" || m.form != "" {
		t.Fatalf("description edit was not submitted: %q %q form=%q", gotID, gotDescription, m.form)
	}
}

func TestProfileImportExplicitlyRemembersOnlySubscriptionAction(t *testing.T) {
	imports, remembered := 0, 0
	b := &fakeBackend{
		add: func(_ context.Context, name, source string) error {
			imports++
			if name != "" || source != "https://example.com/private-token" {
				t.Fatal("one-time import arguments changed")
			}
			return nil
		},
		addSubscription: func(_ context.Context, name, source string) error {
			remembered++
			if name != "" || source != "https://example.com/private-token" {
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
	m, cmd := submitImport(t, m, "https://example.com/private-token")
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
	m, cmd = submitImport(t, m, "https://example.com/private-token")
	m = finishProfileJob(t, m, cmd)
	if remembered != 1 || imports != 1 || m.failure != "" {
		t.Fatal("one-time import inherited remember-source behavior", m.failure)
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
	m.activate("profile.overrides.1")
	m.input.SetValue(link)
	cmd := m.formKey(special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if calls != 0 || !strings.Contains(m.failure, "cannot read overrides") {
		t.Fatal("invalid file reached the application", m.failure)
	}
}

func TestProfilePageShowsPendingChangesAndAccurateActionRows(t *testing.T) {
	m := ready(&fakeBackend{snapshot: app.Snapshot{
		Installed: true, Profiles: []string{"work"}, Applied: "work", ProfilePending: true,
		ProfileDetails: []app.ProfileInfo{{ID: "default", Name: "default", Validated: true}, {ID: "work", Name: "work", Validated: true, Pending: true}},
	}})
	m.page = "profiles"
	m.profilesUI.detail = "work"
	body, hits := m.pageBody(150)
	plain := ansi.Strip(body)
	for _, text := range []string{"Paste subscription URL or local path", "Add", "New profile", "CHANGES PENDING", "Apply to load", "Apply changes", "Overrides"} {
		if !strings.Contains(plain, text) {
			t.Fatalf("missing %q in profiles page", text)
		}
	}
	for _, hit := range hits {
		if hit.ID == "profiles.overrides.0" || hit.ID == "profiles.delete.0" {
			t.Fatal("built-in edit/delete action was offered", hit.ID)
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

func TestProfileDisplayNameEditsKeepActionsOnStableID(t *testing.T) {
	const id = "p-stable-id"
	var renamedID, renamed, updatedID, address, deletedID string
	snapshot := app.Snapshot{Installed: true, Applied: "default", Profiles: []string{id},
		ProfileDetails: []app.ProfileInfo{{ID: "default", Name: "default", Validated: true}, {ID: id, Name: "机场 A", Validated: true}}}
	b := &fakeBackend{snapshot: snapshot,
		renameProfile: func(_ context.Context, profileID, name string) error {
			renamedID, renamed = profileID, name
			return nil
		},
		updateProfileAddress: func(_ context.Context, profileID, source string) error {
			updatedID, address = profileID, source
			return nil
		},
		deleteProfile: func(_ context.Context, profileID string) error { deletedID = profileID; return nil },
	}
	m := profileInteractionsModel(b, 120, 32)
	if card := ansi.Strip(strings.Join(m.profileCard(1, 60).lines, "\n")); !strings.Contains(card, "机场 A") || strings.Contains(card, id) {
		t.Fatal("profile card displayed its storage ID instead of its display name")
	}
	m.activate("profile.1")
	m.activate("profiles.rename.1")
	if m.input.Value() != "机场 A" || m.name != id {
		t.Fatal("Rename did not prefill the display name and retain the stable ID")
	}
	m.input.SetValue("我的订阅")
	m, cmd := update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if renamedID != id || renamed != "我的订阅" || m.profilesUI.detail != id {
		t.Fatal("Rename targeted a display name or lost the open profile identity")
	}
	m.activate("profiles.url-edit.1")
	m.input.SetValue("https://example.invalid/new-source")
	m, cmd = update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if updatedID != id || address != "https://example.invalid/new-source" {
		t.Fatal("Edit URL did not use the stable profile ID")
	}
	m.activate("profiles.delete.1")
	m.input.SetValue("机场 A")
	m, cmd = update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if deletedID != id {
		t.Fatal("display-name delete confirmation changed its backend identity")
	}
}
