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

func TestProfileCreateKeyboardReachesActionsWithoutEditingHiddenInput(t *testing.T) {
	m := ready(&fakeBackend{})
	m.openProfileCreate()
	for _, want := range []int{profileCreateDescription, profileCreateSource, profileCreateSave, profileCreateCancel, profileCreateName} {
		m, _ = update(m, special(tea.KeyTab))
		if m.profileCreate.field != want || m.focus != profileCreateFieldID(want) {
			t.Fatalf("Tab focus = %s, want %s", m.focus, profileCreateFieldID(want))
		}
	}
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.profileCreate.field != profileCreateCancel || m.profileCreateActive() != nil {
		t.Fatal("Shift+Tab did not focus Cancel")
	}
	m, _ = update(m, tea.PasteMsg{Content: "must not enter the name"})
	if m.profileCreate.name.Value() != "" {
		t.Fatal("button focus forwarded pasted text into an unfocused input")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.form != "" || m.busy {
		t.Fatal("Enter on Cancel submitted or left the form open")
	}
}

func TestProfileCreateSubmitsOnlyOnceAndFreezesDraftUntilResult(t *testing.T) {
	calls := 0
	m := ready(&fakeBackend{createProfile: func(context.Context, app.ProfileDraft) error {
		calls++
		return nil
	}})
	m.openProfileCreate()
	m.profileCreate.source.SetValue("https://example.invalid/profile.yaml")
	m.profileCreateSetField(profileCreateSource)
	m, cmd := update(m, special(tea.KeyEnter))
	id, address := m.id, m.profileCreate.source.Value()
	for _, key := range []tea.KeyPressMsg{special(tea.KeyEnter), special(tea.KeyTab), {Code: 'x', Text: "x"}} {
		m, _ = update(m, key)
	}
	m, _ = update(m, tea.PasteMsg{Content: "other input"})
	m.profileCreateMouse("profile-create.name")
	if extra := m.profileCreateMouse("profile-create.save"); extra != nil {
		t.Fatal("Save submitted a second job while creating")
	}
	m.profileCreateMouse("profile-create.cancel")
	if m.id != id || m.profileCreate.source.Value() != address || m.profileCreate.field != profileCreateSource || m.form == "" {
		t.Fatal("busy input edited, canceled or replaced the in-flight creation")
	}
	m = finishProfileJob(t, m, cmd)
	if calls != 1 || m.form != "" || m.busy {
		t.Fatalf("creation calls = %d, form = %q, busy = %v", calls, m.form, m.busy)
	}
}

func TestProfileCreateEscapeCancelsWorkAndKeepsDraftForRetry(t *testing.T) {
	m := ready(&fakeBackend{createProfile: func(ctx context.Context, _ app.ProfileDraft) error {
		return ctx.Err()
	}})
	m.openProfileCreate()
	m.profileCreate.source.SetValue("https://example.invalid/profile.yaml")
	m.profileCreateSetField(profileCreateSource)
	m, cmd := update(m, special(tea.KeyEnter))
	m, _ = update(m, special(tea.KeyEsc))
	if !m.busy || m.opCtx.Err() == nil || m.form == "" {
		t.Fatal("Escape did not wait for safe cancellation")
	}
	m = finishProfileJob(t, m, cmd)
	if m.busy || m.profileCreate.submitting || m.form != "profile-create" || m.profileCreate.source.Value() == "" {
		t.Fatal("canceled creation lost its draft or remained busy")
	}
}

func TestProfileCreateClosesCommittedDraftWhenStateRefreshFails(t *testing.T) {
	calls := 0
	b := &fakeBackend{createProfile: func(context.Context, app.ProfileDraft) error {
		calls++
		return nil
	}}
	m := ready(b)
	m.openProfileCreate()
	m.profileCreate.source.SetValue("https://example.invalid/profile.yaml")
	m.profileCreateSetField(profileCreateSource)
	b.snapshotErr = errors.New("state read unavailable")
	m, cmd := update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if calls != 1 || m.form != "" || m.profileCreate.source.Value() != "" {
		t.Fatal("committed creation remained ready to be submitted again")
	}
	if !strings.Contains(m.message, "Profile created") || !strings.Contains(m.failure, "state read unavailable") {
		t.Fatalf("outcome did not distinguish creation from refresh: %q, %q", m.message, m.failure)
	}
}

func TestProfileCreateCompletesLocalFileWithoutSubmitting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "配置 with spaces.yaml")
	if err := os.WriteFile(path, []byte("rules: ['MATCH,DIRECT']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var got app.ProfileDraft
	calls := 0
	m := ready(&fakeBackend{createProfile: func(_ context.Context, draft app.ProfileDraft) error {
		calls++
		got = draft
		return nil
	}})
	m.openProfileCreate()
	m.profileCreate.description.SetValue("local route")
	m.profileCreate.source.SetValue(filepath.Join(dir, "配"))
	m.profileCreateSetField(profileCreateSource)
	request := m.paths.request(m.profileCreate.source.Value())
	m, _ = update(m, request())
	m, _ = update(m, special(tea.KeyTab))
	if m.profileCreate.source.Value() != path || m.profileCreate.field != profileCreateSource || m.busy || calls != 0 {
		t.Fatal("Tab did not complete the local path without creating a profile")
	}
	// A completed file is also an exact match in the next asynchronous listing.
	// Tab must now reach Create instead of endlessly completing the same path.
	matches := readPathMatches(m.profileCreate.source.Value())
	matches.generation = m.paths.generation
	m, _ = update(m, matches)
	m, _ = update(m, special(tea.KeyTab))
	if m.profileCreate.field != profileCreateSave || m.busy || calls != 0 {
		t.Fatal("Tab on an already completed file did not reach Create")
	}
	m, cmd := update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if calls != 1 || got.Address != path || got.Name != "" || got.Description != "local route" || !got.RememberSource {
		t.Fatalf("local metadata or automatic naming intent was lost: %+v", got)
	}
}

func TestProfileCreateResizeKeepsActionsAndErrorsVisible(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {80, 24}, {60, 20}, {40, 16}, {40, 14}} {
		m := ready(&fakeBackend{})
		m.width, m.height = size[0], size[1]
		m.openProfileCreate()
		m.profileCreateSetField(profileCreateSource)
		m.failure = "Test failure"
		m.paths.active = true
		m.paths.matches = []pathMatch{{label: "candidate-a"}, {label: "candidate-b"}, {label: "candidate-c"}, {label: "candidate-d"}}
		m.layout()
		view := m.View().Content
		assertCanvas(t, view, size[0], size[1])
		if !strings.Contains(ansi.Strip(view), "Test failure") {
			t.Fatalf("error hidden at %dx%d:\n%s", size[0], size[1], ansi.Strip(view))
		}
		rows := strings.Split(ansi.Strip(view), "\n")
		for id, label := range map[string]string{"profile-create.save": "Create", "profile-create.cancel": "Cancel"} {
			target := findTarget(t, m, id)
			visible := ansi.Cut(rows[target.Y], target.X, target.X+target.W)
			if !strings.Contains(visible, label) {
				t.Fatalf("%s target covers %q at %dx%d", id, visible, size[0], size[1])
			}
		}
	}
}

func TestProfileDescriptionCanBeCleared(t *testing.T) {
	called := false
	b := &fakeBackend{snapshot: app.Snapshot{Profiles: []string{"work"}, ProfileDetails: profileFixtures("default", "work")}, updateDescription: func(_ context.Context, id, description string) error {
		called = id == "work" && description == ""
		return nil
	}}
	m := ready(b)
	m.openProfileEdit("profile-description", "work")
	m.input.SetValue(" ")
	m, cmd := update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if !called || m.form != "" {
		t.Fatal("empty description was rejected instead of cleared")
	}
}
