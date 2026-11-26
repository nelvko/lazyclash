package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type coreNumberEditor struct {
	id      string
	input   textinput.Model
	invalid bool
}

func coreNumberSetting(id string, s mihomo.GeneralSettings) (key, value string, enabled bool) {
	switch id {
	case "settings.keepAliveIdle":
		return "keep-alive-idle", s.KeepAliveIdle, s.DisableKeepAlive == "off"
	case "settings.keepAliveInterval":
		return "keep-alive-interval", s.KeepAliveInterval, s.DisableKeepAlive == "off"
	case "settings.geoUpdateInterval":
		return "geo-update-interval", s.GeoUpdateInterval, s.GeoAutoUpdate == "on"
	}
	return "", "", false
}

func (m *Model) openCoreNumber(id string) tea.Cmd {
	_, value, enabled := coreNumberSetting(id, m.snapshot.CoreSettings)
	if !enabled || !m.snapshot.Installed || !m.pageSelectionMenuAvailable("settings") {
		return nil
	}
	input := textinput.New()
	input.Prompt = ""
	if value != "default" {
		input.SetValue(value)
	} else {
		input.Placeholder = m.tr("settings.coreDefault")
	}
	m.coreNumber = coreNumberEditor{id: id, input: input}
	m.focus = id
	return m.coreNumber.input.Focus()
}

func (m *Model) saveCoreNumber() tea.Cmd {
	e := &m.coreNumber
	key, current, enabled := coreNumberSetting(e.id, m.snapshot.CoreSettings)
	if !enabled || !m.snapshot.Installed {
		m.coreNumber = coreNumberEditor{}
		return nil
	}
	next, err := m.snapshot.CoreSettings.With(key, strings.TrimSpace(e.input.Value()))
	if err != nil {
		e.invalid = true
		return nil
	}
	id := e.id
	m.coreNumber = coreNumberEditor{}
	_, value, _ := coreNumberSetting(id, next)
	return m.saveCoreSetting(id, key, current, value)
}

func (m *Model) updateCoreNumber(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "enter", "tab":
			return m.saveCoreNumber()
		case "esc":
			m.coreNumber = coreNumberEditor{}
			return nil
		}
	}
	var cmd tea.Cmd
	before := m.coreNumber.input.Value()
	m.coreNumber.input, cmd = m.coreNumber.input.Update(msg)
	if m.coreNumber.input.Value() != before {
		m.coreNumber.invalid = false
	}
	return cmd
}

func (e *coreNumberEditor) row(o dashboardOptions, label string, width int, surface string) dashboardBlock {
	fieldWidth := min(15, width)
	field := renderInputField(e.input, inputFieldOptions{ID: e.id, Width: fieldWidth, Surface: surface, Compact: true, Focused: true, Invalid: e.invalid})
	var b dashboardBlock
	lines := strings.Split(wrapText(label, width), "\n")
	for _, line := range lines {
		b.lines = append(b.lines, themed(line, text, surface, o.Dim))
	}
	last := len(b.lines) - 1
	gap := width - fieldWidth - ansi.StringWidth(b.lines[last])
	if gap < 1 {
		b.blank()
		last++
		gap = width - fieldWidth
	}
	b.lines[last] += themed(strings.Repeat(" ", gap), text, surface, o.Dim) + field.lines[0]
	for _, hit := range field.targets {
		hit.X += width - fieldWidth
		hit.Y += last
		b.targets = append(b.targets, hit)
	}
	return b
}
