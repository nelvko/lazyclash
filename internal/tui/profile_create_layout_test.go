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

func TestProfileCreateAlignsFieldsAndPaintsModalSurface(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := profilesPreview(false)
		m.locale, m.width, m.height = i18n.New(language), 160, 45
		m.openProfileCreate()
		m.layout()
		source := findTarget(t, m, "profile-create.source")
		last := source
		for _, id := range []string{"profile-create.user-agent", "profile-create.name", "profile-create.description"} {
			field := findTarget(t, m, id)
			if field.X != source.X || field.W != source.W || field.Y < last.Y+last.H {
				t.Fatalf("%s: %s is not aligned below %s: %+v / %+v", language, id, last.ID, field, last)
			}
			last = field
		}
		save, cancel := findTarget(t, m, "profile-create.save"), findTarget(t, m, "profile-create.cancel")
		if gap := save.Y - (last.Y + last.H); gap < 0 || gap > 1 ||
			cancel.Y != save.Y || save.X+save.W != cancel.X || cancel.X+cancel.W != source.X+source.W {
			t.Fatalf("%s: unexpected footer below %+v: %+v / %+v", language, last, save, cancel)
		}
		screen := uv.NewScreenBuffer(m.width, m.height)
		view := m.View().Content
		uv.NewStyledString(view).Draw(screen, screen.Bounds())
		plain := ansi.Strip(view)
		title := strings.Index(plain, m.tr("profiles.create_title"))
		if title < 0 {
			t.Fatalf("%s: missing dialog title", language)
		}
		titleY := strings.Count(plain[:title], "\n")
		for y := titleY; y <= save.Y; y++ {
			for x := source.X; x < source.X+source.W; x++ {
				if y == save.Y && x >= save.X {
					continue // The button faces have their own colors.
				}
				cell := screen.CellAt(x, y)
				if cell.Content == " " {
					want := cell.Style
					want.Bg = lipgloss.Color(modalBackground)
					if !cell.Style.Equal(&want) {
						t.Fatalf("%s: blank cell at %d,%d lost the modal surface", language, x, y)
					}
				}
			}
		}
		m, _ = click(m, last)
		if m.focus != last.ID {
			t.Fatalf("%s: clicking the description field focused %q", language, m.focus)
		}
	}
}

func TestProfileCreateShowsSourceTypeBesideItsLabel(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := profilesPreview(false)
		m.locale, m.width, m.height = i18n.New(language), 160, 45
		m.openProfileCreate()
		for _, sample := range []struct{ address, kind string }{
			{"", ""},
			{"https://example.invalid/profile", m.tr("profiles.create_remote")},
			{"~/profile.yaml", m.tr("profiles.create_local")},
			{"", ""},
		} {
			m.profileCreate.source.SetValue(sample.address)
			m.layout()
			source := findTarget(t, m, "profile-create.source")
			rows := strings.Split(ansi.Strip(m.screen), "\n")
			line := strings.TrimSpace(ansi.Cut(rows[source.Y-1], source.X, source.X+source.W))
			label := m.tr("profiles.create_source_label")
			if !strings.HasPrefix(line, label) || strings.TrimSpace(strings.TrimPrefix(line, label)) != sample.kind {
				t.Fatalf("%s: source %q has label row %q, want %q with type %q", language, sample.address, line, label, sample.kind)
			}
		}
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
