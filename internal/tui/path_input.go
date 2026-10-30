package tui

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m Model) pathForm() bool {
	return m.form == "source" || m.form == "profile-url" || m.form == "profile-overrides"
}

func (m *Model) pathFormKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	if !m.pathForm() {
		return false, nil
	}
	switch k.String() {
	case "up", "down":
		delta := 1
		if k.String() == "up" {
			delta = -1
		}
		m.paths.move(delta)
		return true, nil
	case "tab", "shift+tab":
		if value, ok := m.paths.selectedPath(); ok {
			m.input.SetValue(value)
			m.input.CursorEnd()
			m.failure = ""
		}
		return true, nil
	case "enter":
		// Enter on a directory continues browsing. Only an explicit file path
		// can reach the import use case; Tab never submits a configuration.
		value := strings.TrimSpace(m.input.Value())
		if value == "" || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
			return false, nil
		}
		path, err := resolveLocalPath(value)
		if err != nil {
			m.failure = clean(err.Error())
			return true, nil
		}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			if !strings.HasSuffix(value, "/") {
				m.input.SetValue(value + "/")
				m.input.CursorEnd()
			}
			return true, nil
		}
	}
	return false, nil
}
