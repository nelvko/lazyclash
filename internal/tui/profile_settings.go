package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

// The full address is read only when the user opens this form. The card list
// and its status snapshots carry only a display-safe origin.
type profileSettingsLoadedMsg struct {
	request uint64
	values  app.ProfileSettings
	err     error
}

func (m *Model) profileFieldsForm() bool {
	return m.form == "profile-create" || m.form == "profile-settings"
}

func (m *Model) openProfileSettings(id string) tea.Cmd {
	m.name = id
	m.openProfileFields("profile-settings")
	m.profileCreate.name.Placeholder = m.tr("profiles.display_name")
	m.message = m.tr("profile_settings.loading")
	ctx := m.newJob("profile.settings.load", m.profileNotification("settings.load", m.tr("dashboard.page.settings"), id))
	request, backend := m.id, m.backend
	return func() tea.Msg {
		values, err := backend.GetProfileSettings(ctx, id)
		return profileSettingsLoadedMsg{request, values, err}
	}
}

func (m *Model) acceptProfileSettings(msg profileSettingsLoadedMsg) tea.Cmd {
	if msg.request != m.id {
		return nil
	}
	m.cancel()
	m.busy, m.job, m.message = false, "", ""
	if m.quitting {
		return tea.Quit
	}
	if m.form != "profile-settings" {
		return nil
	}
	if msg.err != nil {
		m.closeProfileCreate()
		m.notification.operation.Dialog = false
		m.failure = m.localizeError(msg.err)
		return nil
	}
	m.resolveNotificationIssue(m.notification.operation.Key)
	m.profileCreate.settings = &msg.values
	m.profileCreate.name.SetValue(msg.values.Name)
	m.profileCreate.description.SetValue(msg.values.Description)
	m.profileCreate.source.SetValue(msg.values.Address)
	m.profileCreate.userAgent.SetValue(msg.values.UserAgent)
	return textinput.Blink
}

func (m *Model) profileSettingsDirty() bool {
	record := m.profileCreate.settings
	return record != nil && (m.profileCreate.name.Value() != record.Name ||
		m.profileCreate.description.Value() != record.Description ||
		m.profileCreate.source.Value() != record.Address ||
		m.profileCreate.effectiveUserAgent() != record.UserAgent)
}

func (m *Model) saveProfileSettings() tea.Cmd {
	if m.profileCreate.settings == nil {
		return nil
	}
	if !m.profileSettingsDirty() {
		m.closeProfileCreate()
		return nil
	}
	values := *m.profileCreate.settings
	values.Name = m.profileCreate.name.Value()
	values.Description = m.profileCreate.description.Value()
	values.UserAgent = m.profileCreate.effectiveUserAgent()
	address := strings.TrimSpace(m.profileCreate.source.Value())
	if address != values.Address && address != "" {
		normalized, err := resolveLocalPath(address)
		if err == nil {
			err = validateProfileSource(normalized)
		}
		if err != nil {
			m.failure = m.localizeError(err)
			return nil
		}
		address = normalized
	}
	values.Address = address
	m.profileCreate.submitting = true
	label := m.tr("profile_settings.saving")
	return m.begin(label, m.tr("profile_settings.saved"), func(ctx context.Context) error {
		return m.backend.SaveProfileSettings(ctx, values)
	}, m.profileNotification("settings.save", m.tr("profile_settings.save"), values.ID))
}

func (m *Model) focusProfileByID(id string) {
	if i := slices.Index(m.profiles(), id); i >= 0 {
		m.focusProfileControl(fmt.Sprintf("profile.%d", i))
		m.rememberPageView()
	} else {
		m.enterContent()
	}
}

func (m *Model) profileSettingsAction(field int) tea.Cmd {
	id := m.name
	if m.profileCreate.settings == nil || m.profileSettingsDirty() {
		m.failure = m.tr("profile_settings.save_first")
		return nil
	}
	switch field {
	case profileSettingsImport:
		m.closeProfileCreate()
		return m.openProfileEdit("profile-overrides", id)
	case profileSettingsDeactivate:
		if m.canUseDefaultFrom(id) {
			m.closeProfileCreate()
			label := m.tr("profiles.deactivating")
			return m.begin(label, m.tr("feedback.deactivated", m.profileFeedbackName(id)), func(ctx context.Context) error {
				return m.backend.DeactivateProfile(ctx, id)
			}, m.profileNotification("deactivate", m.tr("profile_settings.deactivate"), id))
		}
	}
	return nil
}

func (m *Model) profileSettingsMetadata(info app.ProfileInfo, width int) dashboardBlock {
	type metadataField struct{ label, value string }
	var fields []metadataField
	add := func(label, value string) {
		fields = append(fields, metadataField{m.tr(label), value})
	}
	if !info.UpdatedAt.IsZero() {
		add("profiles.updated_date", info.UpdatedAt.Local().Format(time.DateTime))
	}
	quota, expiresAt := profileAllowance(info)
	if quota != "" {
		add("profiles.remaining", quota)
	}
	if expiresAt > 0 {
		add("profiles.expires", time.Unix(expiresAt, 0).Format(time.DateOnly))
	}
	var block dashboardBlock
	if len(fields) == 0 {
		return block
	}

	const gutter = 2
	var labelWidth, valueWidth, columnsWidth int
	for _, field := range fields {
		labelWidth = max(labelWidth, ansi.StringWidth(field.label))
		valueWidth = max(valueWidth, ansi.StringWidth(field.value))
		columnsWidth += max(ansi.StringWidth(field.label), ansi.StringWidth(field.value))
	}
	if spare := width - columnsWidth - gutter*(len(fields)-1); spare >= 0 {
		gap := strings.Repeat(" ", gutter+spare/len(fields))
		var labels, values []string
		for _, field := range fields {
			column := max(ansi.StringWidth(field.label), ansi.StringWidth(field.value))
			labels = append(labels, cellPad(field.label, column))
			values = append(values, cellPad(field.value, column))
		}
		block.lines = []string{
			themed(strings.Join(labels, gap), muted, modalBackground, false),
			themed(strings.Join(values, gap), text, modalBackground, false),
		}
		return block
	}
	for _, field := range fields {
		label := themed(field.label, muted, modalBackground, false)
		if labelWidth+gutter+valueWidth <= width {
			block.lines = append(block.lines, cellPad(label, labelWidth+gutter)+themed(field.value, text, modalBackground, false))
			continue
		}
		block.lines = append(block.lines, label)
		for _, line := range strings.Split(wrapText(field.value, width), "\n") {
			block.lines = append(block.lines, themed(line, text, modalBackground, false))
		}
	}
	return block
}

func (m *Model) scrollProfileSettings(delta int) {
	m.profileCreate.firstRow = max(0, m.profileCreate.firstRow+delta)
	m.profileCreate.manualScroll = true
}
