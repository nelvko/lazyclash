package tui

import tea "charm.land/bubbletea/v2"

func (o dashboardOptions) switchOptions(id, surface string) switchOptions {
	return switchOptions{Surface: surface, Focused: o.Focus == id, Hovered: o.Hover == id, Plain: o.PlainButtons, Dim: o.Dim}
}

// Page navigation or hit testing has already selected the owner. Resolve the
// switch from the current observation even if its last paint was actionable.
func (m *Model) activateSwitch(id string, key *tea.KeyPressMsg) (bool, tea.Cmd) {
	var control Switch
	var found bool
	var apply func(string, bool) tea.Cmd
	switch {
	case m.page == "settings" || id == "key-hints.toggle":
		control, found = m.navigationOptions().settingsSwitch(id)
		apply = m.applySettingsSwitch
	case m.page == "overview":
		control, found = m.navigationOptions().overviewSwitch(id)
		apply = m.applyOverviewSwitch
	}
	if !found {
		return false, nil
	}
	if m.quitting {
		return true, nil
	}
	var on, ok bool
	if key == nil {
		on, ok = control.Intent()
	} else {
		on, ok = control.KeyIntent(*key, m.focus == id)
	}
	if !ok {
		return true, nil
	}
	return true, apply(id, on)
}
