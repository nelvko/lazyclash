package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func finishProfileSettingsLoad(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing profile settings lookup")
	}
	msg := cmd()
	for {
		cmds, ok := msg.(tea.BatchMsg)
		if !ok {
			break
		}
		msg = cmds[0]()
	}
	if _, ok := msg.(profileSettingsLoadedMsg); !ok {
		t.Fatalf("unexpected settings response %T", msg)
	}
	m, _ = update(m, msg)
	return m
}

func openSettingsForTest(t *testing.T, m Model, id string) Model {
	t.Helper()
	cmd := m.openProfileSettings(id)
	return finishProfileSettingsLoad(t, m, cmd)
}

func TestProfileSettingsSaveTogetherAndRetainFailedEdits(t *testing.T) {
	original := app.ProfileSettings{ID: "p-id", Name: "Provider name", Description: "old", Address: "https://provider.example/secret", UserAgent: "Previous/1.0"}
	var saved []app.ProfileSettings
	b := &fakeBackend{snapshot: app.Snapshot{Profiles: []string{"p-id"}, ProfileDetails: []app.ProfileInfo{{ID: "p-id", Name: original.Name}}},
		getProfileSettings: func(context.Context, string) (app.ProfileSettings, error) { return original, nil },
		saveProfileSettings: func(_ context.Context, s app.ProfileSettings) error {
			saved = append(saved, s)
			if len(saved) == 1 {
				return errors.New("unreadable source")
			}
			return nil
		}}
	m := profileInteractionsModel(b, 120, 32)
	m = openSettingsForTest(t, m, original.ID)
	if m.profileCreate.name.Value() != original.Name || m.profileCreate.description.Value() != original.Description || m.profileCreate.source.Value() != original.Address || m.profileCreate.userAgent.Value() != original.UserAgent {
		t.Fatal("fields not prefilled")
	}
	m.profileCreate.userAgent.SetValue("")
	if !m.profileSettingsDirty() {
		t.Fatal("clearing only the User-Agent was not detected as an edit")
	}
	m.profileCreate.name.SetValue("new name")
	m.profileCreate.description.SetValue("")
	m.profileCreateSetField(profileCreateSource)
	for _, invalid := range []string{"", "https://"} {
		m.profileCreate.source.SetValue(invalid)
		m, _ = update(m, special(tea.KeyEnter))
		if m.busy || m.failure == "" || m.form != "profile-settings" || len(saved) != 0 {
			t.Fatal("missing or malformed source escaped form validation")
		}
	}
	m.profileCreate.source.SetValue("https://example.invalid/new")
	m, cmd := update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if m.form != "profile-settings" || m.failure == "" || m.profileCreate.source.Value() != "https://example.invalid/new" || m.profileCreate.name.Value() != "new name" {
		t.Fatal("failed save lost draft")
	}
	m.profileCreate.source.SetValue(original.Address)
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "profile-create.user-agent" {
		t.Fatalf("remote settings skipped User-Agent, focus = %s", m.focus)
	}
	m, _ = update(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m, _ = update(m, tea.PasteMsg{Content: "Updated/2.0"})
	for _, want := range []int{profileCreateName, profileCreateDescription, profileCreateSave} {
		m, _ = update(m, special(tea.KeyEnter))
		if m.focus != profileCreateFieldID(want) || m.busy {
			t.Fatalf("Enter focused %s, want %s before saving", m.focus, profileCreateFieldID(want))
		}
	}
	m, cmd = update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if len(saved) != 2 || saved[1].ID != original.ID || saved[1].Address != original.Address || saved[1].Description != "" || saved[1].Name != "new name" || saved[1].UserAgent != "Updated/2.0" || m.form != "" || m.focus != "profile.0" {
		t.Fatal("settings save did not submit one complete form or restore card focus")
	}
	if strings.Contains(ansi.Strip(m.screen), original.Address) {
		t.Fatal("private source remained visible after save")
	}
}

func TestProfileSettingsKeyboardResizeAndExternalChanges(t *testing.T) {
	m := profileNavigationModel(160, 40)
	m = openSettingsForTest(t, m, "gamma")
	for _, size := range [][2]int{{80, 24}, {60, 20}, {40, 16}, {160, 40}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, field := range []int{profileCreateDescription, profileSettingsImport, profileCreateCancel, profileCreateSave, profileCreateSource, profileCreateName} {
			m, _ = update(m, special(tea.KeyTab))
			if m.focus != profileCreateFieldID(field) {
				t.Fatalf("focus %s at %v", m.focus, size)
			}
			findTarget(t, m, m.focus)
		}
	}
	next := m.snapshot
	next.Profiles = []string{"gamma", "alpha"}
	m, _ = update(m, coreStatusMsg{generation: m.statusGeneration, snapshot: next})
	if m.name != "gamma" || m.form != "profile-settings" {
		t.Fatal("reorder retargeted settings")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.form != "" || m.focus != "profile.0" {
		t.Fatalf("Escape did not restore current card position: form=%q focus=%q names=%v", m.form, m.focus, m.profiles())
	}
	m = openSettingsForTest(t, m, "gamma")
	next.Profiles = []string{"alpha"}
	m, _ = update(m, coreStatusMsg{generation: m.statusGeneration, snapshot: next})
	if m.form != "" || m.name != "" {
		t.Fatal("removed profile retained its settings form")
	}
}
