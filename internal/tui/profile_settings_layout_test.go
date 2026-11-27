package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestProfileSettingsFitsLocalAndRemoteEditors(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := profilesPreview(true)
			m.width, m.height, m.locale = 160, 45, i18n.New(language)
			m.profileCreate.source.SetValue("https://example.invalid/sub")
			m.profileCreate.description.SetValue("工作")
			m.layout()

			description := findTarget(t, m, "profile-create.description")
			source := findTarget(t, m, "profile-create.source")
			agent := findTarget(t, m, "profile-create.user-agent")
			save := findTarget(t, m, "profile-create.save")
			m, _ = click(m, description)
			m, _ = update(m, special(tea.KeyLeft))
			assertSearchCursorCell(t, m, "作")
			m, _ = click(m, findTarget(t, m, "profile-create.description.clear"))
			if m.focus != description.ID || m.profileCreate.description.Value() != "" || m.profileCreate.name.Value() == "" {
				t.Fatal("clicking the description's clear button should clear only that editor")
			}

			m.profileCreate.source.SetValue("/configs/home.yaml")
			m.layout()
			for _, hit := range m.targets {
				if hit.ID == agent.ID {
					t.Fatal("local profile still exposes a User-Agent editor")
				}
			}
			localSource, localSave := findTarget(t, m, source.ID), findTarget(t, m, save.ID)
			if localSave.Y-localSource.Y >= save.Y-source.Y {
				t.Fatal("hiding User-Agent should shorten the form")
			}
			m.profileCreateSetField(profileCreateSource)
			m.profileCreate.source.SetValue("https://")
			m, _ = update(m, special(tea.KeyEnter))
			if m.failure == "" || !strings.Contains(ansi.Strip(m.screen), m.failure) {
				t.Fatalf("invalid source should display its validation error: %q", m.failure)
			}
		})
	}
}

func TestProfileSettingsNarrowLayoutKeepsEditorsAndActionsReachable(t *testing.T) {
	m := profilesPreview(true)
	m.width, m.height, m.locale = 40, 36, i18n.New(i18n.Chinese)
	m.profileCreate.source.SetValue("https://example.invalid/sub")
	m.layout()
	name, description := findTarget(t, m, "profile-create.name"), findTarget(t, m, "profile-create.description")
	if name.X != description.X || name.W != description.W || description.Y < name.Y+name.H {
		t.Fatalf("narrow name and description should stack: %+v / %+v", name, description)
	}

	m.snapshot.ProfileDetails[1].Quota = &configuration.SubscriptionQuota{TotalBytes: 100, UsedBytes: 50, ExpiresAt: 1800000000}
	m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	save := findTarget(t, m, "profile-create.save")
	m, _ = update(m, special(tea.KeyPgDown))
	view, _ := m.renderProfileForm("")
	if !strings.Contains(ansi.Strip(view), "50 B / 100 B") || findTarget(t, m, save.ID).Y != save.Y {
		t.Fatal("paging through the form should expose subscription metadata and keep Save in place at 80×24")
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 16})
	var actions []hitRegion
	for _, id := range []string{"profile-settings.overrides", "profile-settings.deactivate", "profile-create.save", "profile-create.cancel"} {
		actions = append(actions, findTarget(t, m, id))
	}
	// Move to the first editor, then page away from it before typing.
	for range 2 {
		m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	}
	m, _ = update(m, special(tea.KeyPgDown))
	if hasAction(m.targets, "profile-create.source") {
		t.Fatal("paging down should move the first editor out of view")
	}
	m, _ = update(m, tea.PasteMsg{Content: "q"})
	if m.View().Cursor == nil || !strings.HasSuffix(m.profileCreate.source.Value(), "q") {
		t.Fatal("typing into the focused editor should bring it back into view")
	}
	for _, id := range []string{"profile-create.source", "profile-create.user-agent", name.ID, description.ID} {
		field := findTarget(t, m, id)
		cursor := m.View().Cursor
		if m.focus != id || cursor == nil || cursor.X < field.X || cursor.X >= field.X+field.W ||
			cursor.Y < field.Y || cursor.Y >= field.Y+field.H {
			t.Fatalf("Tab should reveal the active editor and its cursor: %+v, focus=%s, cursor=%+v", field, m.focus, cursor)
		}
		m, _ = update(m, special(tea.KeyTab))
	}
	for _, field := range []int{profileSettingsImport, profileSettingsDeactivate, profileCreateCancel, profileCreateSave} {
		if m.focus != profileCreateFieldID(field) {
			t.Fatalf("action focus = %s, want %s", m.focus, profileCreateFieldID(field))
		}
		m, _ = update(m, special(tea.KeyTab))
	}
	m, _ = update(m, special(tea.KeyPgDown))
	for _, action := range actions {
		if now := findTarget(t, m, action.ID); now.X != action.X || now.Y != action.Y {
			t.Fatalf("scrolling moved a footer action: %+v -> %+v", action, now)
		}
	}

	m.profileCreateSetField(profileCreateSource)
	m.profileCreate.source.SetValue("~/work")
	m.paths = pathCompletion{active: true, matches: []pathMatch{{label: "work.yaml", path: "~/work.yaml"}}}
	m.layout()
	source := findTarget(t, m, "profile-create.source")
	rows := strings.Split(ansi.Strip(m.screen), "\n")
	if !strings.Contains(rows[source.Y+source.H], "work.yaml") {
		t.Fatalf("selected path should follow its source editor:\n%s", ansi.Strip(m.screen))
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.profileCreate.source.Value() != "~/work.yaml" || m.focus != source.ID {
		t.Fatal("Tab did not complete the visible path")
	}
}
