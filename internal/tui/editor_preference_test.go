package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestEditorPreferenceCanBeSetAndReturnedToAutomatic(t *testing.T) {
	b := &fakeBackend{}
	m := ready(b)
	m.page = "settings"
	m.focus = "editor.configure"
	for _, value := range []string{"vi -f", ""} {
		m, _ = update(m, special(tea.KeyEnter))
		if m.form != "editor-preference" || m.input.Value() != b.prefs.Editor {
			t.Fatal("editor form lost current selection")
		}
		m.input.SetValue(value)
		var cmd tea.Cmd
		m, cmd = update(m, special(tea.KeyEnter))
		if cmd == nil {
			t.Fatalf("saving %q returned no command: busy=%v failure=%s", value, m.busy, m.failure)
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			msg = batch[0]()
		}
		updated, _ := update(m, msg)
		if updated.form != "" || updated.editorPreference != value || b.prefs.Editor != value {
			t.Fatalf("editor preference did not persist: %q", value)
		}
		// Start another model to verify startup reads the saved command.
		m = ready(b)
		m.page, m.focus = "settings", "editor.configure"
		if m.editorPreference != value {
			t.Fatal("reopened TUI did not read preference")
		}
	}
}

func TestEditorDialogKeepsFeedbackBelowInputAndSaveKeysVisible(t *testing.T) {
	for _, language := range []string{i18n.Chinese, i18n.English} {
		m := ready(&fakeBackend{})
		m.page, m.focus, m.locale = "settings", "editor.configure", i18n.New(language)
		m.openEditorPreference()
		m.input.SetValue("nvim -f")
		for _, size := range [][2]int{{160, 45}, {40, 16}, {24, 10}, {24, 7}} {
			m.width, m.height = size[0], size[1]
			for _, failure := range []string{"", m.tr("editor.unavailable")} {
				m.failure = failure
				view, hits := m.renderProfileForm("")
				assertCanvas(t, view, m.width, m.height)
				field := findTarget(t, Model{targets: hits}, "import-input")
				lines := strings.Split(ansi.Strip(view), "\n")
				for i, line := range lines {
					lines[i] = strings.TrimSpace(ansi.Cut(line, field.X, field.X+field.W))
				}
				headings := lines[:field.Y]
				if !slices.Contains(headings, m.tr("settings.editorCommand")) || slices.Contains(headings, m.tr("settings.editor")) {
					t.Fatalf("%s %v: expected a single editor command heading: %q", language, size, headings)
				}
				feedback := lines[field.Y+field.H]
				message := m.tr("settings.editorHint")
				if failure != "" {
					message = failure
				}
				if feedback == "" || !strings.HasPrefix(message, strings.TrimSuffix(feedback, "…")) {
					t.Fatalf("%s %v: missing feedback below field: %q", language, size, feedback)
				}
				footer := strings.Join(strings.Fields(strings.Join(lines[field.Y+field.H+1:], " ")), " ")
				if !strings.Contains(footer, m.tr("settings.saveKeys")) {
					t.Fatalf("%s %v: missing save or cancel: %q", language, size, footer)
				}
			}
		}
	}
}
