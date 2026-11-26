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

func TestEditorRowFocusAfterMouseAndKeyboardActivation(t *testing.T) {
	const editor = "editor.configure"
	m := ready(&fakeBackend{})
	directionCleanup(t, &m)
	m.page, m.focus = "settings", "settings.language"
	m.layout()
	row := func() string {
		hit := findTarget(t, m, editor)
		return ansi.Cut(strings.Split(m.screen, "\n")[hit.Y], hit.X, hit.X+hit.W)
	}
	normal := row()
	cancel := func() {
		t.Helper()
		if m.form != "editor-preference" {
			t.Fatal("editor dialog did not open")
		}
		m, _ = update(m, special(tea.KeyEscape))
		if m.form != "" || m.focus != editor {
			t.Fatalf("cancel should return to the editor row, got form %q, focus %q", m.form, m.focus)
		}
	}
	m, _ = click(m, findTarget(t, m, editor))
	m, _ = update(m, key("x"))
	cancel()
	if row() != normal {
		t.Fatal("mouse-opened editor left a visible focus marker or background after cancel")
	}

	m, _ = update(m, special(tea.KeyTab))
	if m.focus == editor {
		t.Fatal("Tab did not advance from the editor row")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	focused := row()
	if m.focus != editor || !strings.HasPrefix(ansi.Strip(focused), "›") {
		t.Fatal("tabbing back did not visibly focus the editor row")
	}
	m, _ = update(m, special(tea.KeyEnter))
	cancel()
	if row() != focused {
		t.Fatal("keyboard-opened editor lost its visible focus after cancel")
	}
	m, _ = click(m, findTarget(t, m, editor))
	cancel()
	if row() != normal {
		t.Fatal("clicking the keyboard-focused editor should return it to its normal appearance")
	}
	m, _ = update(m, special(tea.KeyEnter))
	cancel()
	if row() != focused {
		t.Fatal("reopening with Enter should restore visible keyboard focus")
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
