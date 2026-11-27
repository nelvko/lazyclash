package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestProfileFormsShareFieldAndActionLayout(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			var createWidth int
			for _, form := range []string{"profile-create", "profile-settings"} {
				t.Logf("checking %s", form)
				m := profilesPreview(false)
				m.locale, m.width, m.height = i18n.New(language), 160, 45
				title := m.tr("profiles.create_title")
				if form == "profile-create" {
					m.openProfileCreate()
				} else {
					m = openSettingsForTest(t, m, "Everyday")
					title = m.tr("profile_settings.title")
				}
				m.profileCreate.source.SetValue("https://example.invalid/sub")
				m.layout()
				source := findTarget(t, m, "profile-create.source")
				description := findTarget(t, m, "profile-create.description")
				right := source.X + source.W
				previous := source
				for _, id := range []string{"profile-create.user-agent", "profile-create.name", description.ID} {
					field := findTarget(t, m, id)
					if field.X != source.X || field.W != source.W || field.Y < previous.Y+previous.H {
						t.Fatalf("each editor should occupy a full row below the previous one: %+v / %+v", previous, field)
					}
					previous = field
				}
				if form == "profile-create" {
					createWidth = source.W
				} else if source.W != createWidth {
					t.Fatalf("settings field width %d differs from creation width %d", source.W, createWidth)
				}
				save := findTarget(t, m, "profile-create.save")
				cancel := findTarget(t, m, "profile-create.cancel")
				if save.Y != cancel.Y || cancel.X+cancel.W != save.X || save.X+save.W != right {
					t.Fatalf("Cancel and the primary action should align to the right: %+v / %+v", cancel, save)
				}

				view, _ := m.renderProfileForm("")
				rows := strings.Split(ansi.Strip(view), "\n")
				titleY := slices.IndexFunc(rows, func(row string) bool { return strings.TrimSpace(row) == title })
				if titleY < 0 || strings.TrimSpace(rows[source.Y-1]) != m.tr("profiles.create_source_label") {
					t.Fatalf("title and source label should stand alone:\n%s", ansi.Strip(view))
				}
				if strings.Contains(view, "Tab") || strings.Contains(view, "Esc") {
					t.Fatal("dialog repeats keyboard shortcuts from the global hint bar")
				}
				if !strings.Contains(rows[save.Y-1], "──") {
					t.Fatal("missing separator above the action bar")
				}
				screen := uv.NewScreenBuffer(m.width, m.height)
				uv.NewStyledString(m.View().Content).Draw(screen, screen.Bounds())
				for y := titleY; y < save.Y; y++ {
					for x := source.X; x < right; x++ {
						cell := screen.CellAt(x, y)
						if cell.Content == " " {
							want := cell.Style
							want.Bg = lipgloss.Color(modalBackground)
							if !cell.Style.Equal(&want) {
								t.Fatalf("blank cell at %d,%d lost the modal surface", x, y)
							}
						}
					}
				}
				m, _ = click(m, description)
				if m.focus != description.ID {
					t.Fatalf("clicking description focused %q", m.focus)
				}
				m, _ = click(m, cancel)
				if m.form != "" || m.busy {
					t.Fatal("clicking Cancel should dismiss the draft")
				}
			}
		})
	}
}

func TestProfileCreateShortWindowPlacesMatchesBelowSource(t *testing.T) {
	m := profilesPreview(false)
	m.width, m.height = 40, 16
	m.openProfileCreate()
	m.profileCreate.source.SetValue("~/sub")
	m.paths = pathCompletion{active: true, index: 1, matches: []pathMatch{{label: "sub-a.yaml", path: "~/sub-a.yaml"}, {label: "sub-b.yaml", path: "~/sub-b.yaml"}}}
	m.layout()
	source := findTarget(t, m, "profile-create.source")
	save := findTarget(t, m, "profile-create.save")
	rows := strings.Split(ansi.Strip(m.screen), "\n")
	firstMatch := source.Y + source.H
	if firstMatch >= save.Y || !strings.Contains(rows[firstMatch], ".yaml") || !strings.Contains(strings.Join(rows[firstMatch:save.Y], "\n"), "› sub-b.yaml") {
		t.Fatalf("short window hid the selected completion or the actions:\n%s", strings.Join(rows, "\n"))
	}
	for _, hit := range m.targets {
		if hit.ID == "profile-create.user-agent" {
			t.Fatal("local source retained a User-Agent field")
		}
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.profileCreate.source.Value() != "~/sub-b.yaml" || m.focus != source.ID {
		t.Fatal("Tab did not complete the visible selection into the source field")
	}
}

func TestProfileCreateNarrowWindowStacksCompleteFields(t *testing.T) {
	m := profilesPreview(false)
	m.locale, m.width, m.height = i18n.New(i18n.Chinese), 36, 40
	m.openProfileCreate()
	m.layout()
	fields := []string{"profile-create.source", "profile-create.user-agent", "profile-create.name", "profile-create.description"}
	source := findTarget(t, m, fields[0])
	nextY := source.Y
	for _, id := range fields {
		hit := findTarget(t, m, id)
		if hit.X != source.X || hit.W != source.W || hit.Y < nextY {
			t.Fatalf("field %s did not stack across the available width: %+v", id, hit)
		}
		nextY = hit.Y + hit.H
	}

	m, _ = update(m, tea.WindowSizeMsg{Width: 36, Height: 14})
	save := findTarget(t, m, "profile-create.save")
	m, _ = update(m, special(tea.KeyPgDown))
	if hasAction(m.targets, source.ID) || findTarget(t, m, save.ID).Y != save.Y {
		t.Fatal("paging down should move the fields while keeping the action bar in place")
	}
	m, _ = update(m, special(tea.KeyPgUp))
	for _, id := range fields {
		focused := findTarget(t, m, id)
		if m.focus != id || m.View().Cursor == nil {
			t.Fatalf("short layout lost its active editor %s: %+v", id, focused)
		}
		edges := map[int]bool{}
		for _, hit := range m.targets {
			if !slices.Contains(fields, hit.ID) {
				continue
			}
			if hit.InputWidth == 0 {
				t.Fatalf("short layout exposed a clipped field while editing %s: %+v", id, hit)
			}
			edges[hit.Y], edges[hit.Y+hit.H-1] = true, true
		}
		for y, row := range strings.Split(ansi.Strip(m.screen), "\n") {
			frame := ansi.Cut(row, source.X, source.X+source.W)
			if (strings.HasPrefix(frame, "╭─") || strings.HasPrefix(frame, "╰─")) && !edges[y] {
				t.Fatalf("short layout painted a detached field border while editing %s: %q", id, frame)
			}
		}
		m, _ = update(m, special(tea.KeyTab))
	}
}
