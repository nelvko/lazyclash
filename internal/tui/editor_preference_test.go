package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
