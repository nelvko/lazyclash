package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func switchStyleMenuItems(locale i18n.Localizer) []menuItem {
	return []menuItem{
		{ID: app.SwitchStyleSlanted, Label: locale.T("settings.switchStyle.slanted")},
		{ID: app.SwitchStyleSlider, Label: locale.T("settings.switchStyle.slider")},
		{ID: app.SwitchStyleText, Label: locale.T("settings.switchStyle.text")},
		{ID: app.SwitchStyleLine, Label: locale.T("settings.switchStyle.line")},
	}
}

func switchStyleLabelWidth(items []menuItem) int {
	width := 0
	for _, item := range items {
		width = max(width, ansi.StringWidth(item.Label))
	}
	return width
}

func settingsSwitchStyleRow(o dashboardOptions) settingsRow {
	items := switchStyleMenuItems(o.Locale)
	row := settingsSelectRow(o, "settings.switchStyle", o.Locale.T("settings.switchStyle"), app.NormalizeSwitchStyle(o.SwitchStyle), items)
	row.actions[0].enabled = !o.Busy && !o.SwitchStyleSaving
	label := row.actions[0].label
	width := switchStyleLabelWidth(items) + ansi.StringWidth(" ▾")
	row.actions[0].label = strings.Repeat(" ", width-ansi.StringWidth(label)) + label
	return row
}

func setSwitchStylePreviews(menu *actionMenu, monochrome bool) {
	labelWidth := switchStyleLabelWidth(menu.items)
	for i, item := range menu.items {
		options := toggleOptions{Style: item.ID, Surface: raised, Monochrome: monochrome}
		if i == menu.selected {
			options.Surface = selectionBackground
		}
		padding := strings.Repeat(" ", labelWidth-ansi.StringWidth(item.Label)+2)
		off := renderToggle(toggleModel{state: toggleOff}, options)
		on := renderToggle(toggleModel{state: toggleOn}, options)
		menu.items[i].preview = padding + off + " → " + on
	}
}

type switchStyleSavedMsg struct {
	id       uint64
	previous string
	err      error
}

func (m *Model) changeSwitchStyle(style string) tea.Cmd {
	if !m.pageSelectionMenuAvailable("settings") || m.styleSavePending || style == m.switchStyle || style != app.NormalizeSwitchStyle(style) {
		return nil
	}
	m.styleSaveGeneration++
	m.styleSavePending = true
	id, backend, ctx, previous := m.styleSaveGeneration, m.backend, m.ctx, m.switchStyle
	m.switchStyle = style
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		return switchStyleSavedMsg{id, previous, backend.SaveSwitchStylePreference(ctx, style)}
	}
}

func (m *Model) acceptSwitchStyleSaved(msg switchStyleSavedMsg) tea.Cmd {
	if !m.styleSavePending || msg.id != m.styleSaveGeneration {
		return nil
	}
	m.styleSavePending = false
	if msg.err != nil {
		m.switchStyle = msg.previous
		if !m.quitting && m.ctx.Err() == nil {
			m.preferenceSaveWarning("switch-style.save", m.tr("settings.switchStyleSaveFailed", m.localizeError(msg.err)))
		}
	}
	if m.quitting && !m.busy {
		return m.quitWhenSaved()
	}
	return nil
}
