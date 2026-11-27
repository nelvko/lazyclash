package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestProfileSettingsWideLayoutFitsItsFields(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := profilesPreview(true)
			m.width, m.height, m.locale = 160, 45, i18n.New(language)
			m.profileCreate.source.SetValue("https://example.invalid/sub")
			m.profileCreate.description.SetValue("工作")
			m.layout()

			name := findTarget(t, m, "profile-create.name")
			description := findTarget(t, m, "profile-create.description")
			source := findTarget(t, m, "profile-create.source")
			agent := findTarget(t, m, "profile-create.user-agent")
			right := description.X + description.W
			if name.Y != description.Y || name.X+name.W >= description.X ||
				name.W-description.W > 1 || description.W-name.W > 1 {
				t.Fatalf("name and description should share a row: %+v / %+v", name, description)
			}
			if source.X != name.X || source.X+source.W != right || source.Y < name.Y+name.H ||
				agent.X != source.X || agent.W != source.W || agent.Y < source.Y+source.H {
				t.Fatalf("long fields should span both columns: %+v / %+v", source, agent)
			}
			save, cancel := findTarget(t, m, "profile-create.save"), findTarget(t, m, "profile-create.cancel")
			if save.Y != cancel.Y || max(save.X+save.W, cancel.X+cancel.W) != right {
				t.Fatalf("form actions should align to the right edge: %+v / %+v", save, cancel)
			}

			m, _ = click(m, description)
			m, _ = update(m, special(tea.KeyLeft))
			assertSearchCursorCell(t, m, "作")
			m, _ = click(m, findTarget(t, m, "profile-create.description.clear"))
			if m.focus != description.ID || m.profileCreate.description.Value() != "" || m.profileCreate.name.Value() == "" {
				t.Fatal("clicking clear in the second column did not clear just its editor")
			}

			m.profileCreate.source.SetValue("/configs/home.yaml")
			m.layout()
			for _, hit := range m.targets {
				if hit.ID == agent.ID {
					t.Fatal("local profile still exposes a User-Agent editor")
				}
			}
			localName, localSave := findTarget(t, m, name.ID), findTarget(t, m, save.ID)
			if localSave.Y-localName.Y >= save.Y-name.Y {
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

	// A normal 24-line terminal fits the entire remote form, including its
	// metadata values and the actions for the active subscription.
	m.snapshot.ProfileDetails[1].Quota = &configuration.SubscriptionQuota{TotalBytes: 100, UsedBytes: 50, ExpiresAt: 1800000000}
	m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(ansi.Strip(m.screen), "50 B / 100 B") || strings.Contains(m.screen, "PgUp/PgDn") {
		t.Fatal("the complete subscription should fit at 80×24")
	}
	findTarget(t, m, "profile-create.user-agent")
	m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 16})
	var actions []hitRegion
	for _, id := range []string{"profile-settings.overrides", "profile-settings.deactivate", "profile-create.save", "profile-create.cancel"} {
		actions = append(actions, findTarget(t, m, id))
	}
	m, _ = update(m, special(tea.KeyPgDown))
	for _, hit := range m.targets {
		if hit.ID == name.ID {
			t.Fatal("paging down should move the first editor out of view")
		}
	}
	// Typing after scrolling away from an editor must reveal the editor.
	m, _ = update(m, tea.PasteMsg{Content: "日"})
	if m.View().Cursor == nil || !strings.Contains(m.profileCreate.name.Value(), "日") {
		t.Fatal("typing into the focused editor should bring it back into view")
	}
	for _, id := range []string{name.ID, description.ID, "profile-create.source", "profile-create.user-agent"} {
		field := findTarget(t, m, id)
		cursor := m.View().Cursor
		if m.focus != id || cursor == nil || cursor.X < field.X || cursor.X >= field.X+field.W ||
			cursor.Y < field.Y || cursor.Y >= field.Y+field.H {
			t.Fatalf("Tab should reveal the active editor and its cursor: %+v, focus=%s, cursor=%+v", field, m.focus, cursor)
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
