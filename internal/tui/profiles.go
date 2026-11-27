package tui

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/store"
)

type profilesState struct {
	url               textinput.Model
	urlError          string
	urlErrorValue     string
	pasteDeadline     time.Time
	expiryAt          int64
	cancelExpiry      context.CancelFunc
	pasteUnavailable  bool
	editing           bool
	importing         bool
	menuProfile       string
	deleteFocus       string
	deleteCurrent     bool
	deleteReplacement string
}

const profilePasteTimeout = 15 * time.Second

type profilePasteTimeoutMsg struct{ deadline time.Time }

type profileExpiredMsg struct{}

// Wake the idle overview or subscription grid when a displayed subscription expires.
func (m *Model) syncProfileExpiry(msg tea.Msg, previousPage string) tea.Cmd {
	if m.page == previousPage && !m.quitting {
		switch msg.(type) {
		case resultMsg, coreStatusMsg, profileExpiredMsg:
		default:
			return nil
		}
	}
	var next int64
	if !m.quitting && (m.page == "profiles" || m.page == "overview") {
		now := time.Now().Unix()
		for _, info := range m.snapshot.ProfileDetails {
			if m.page == "overview" && info.ID != m.snapshot.Applied {
				continue
			}
			q := info.Quota
			if q != nil && q.ExpiresAt > now && (next == 0 || q.ExpiresAt < next) {
				next = q.ExpiresAt
			}
		}
	}
	if next == m.profilesUI.expiryAt {
		return nil
	}
	if m.profilesUI.cancelExpiry != nil {
		m.profilesUI.cancelExpiry()
		m.profilesUI.cancelExpiry = nil
	}
	m.profilesUI.expiryAt = next
	if next == 0 {
		return nil
	}
	ctx, cancel := context.WithDeadline(m.ctx, time.Unix(next, 0))
	m.profilesUI.cancelExpiry = cancel
	return func() tea.Msg {
		<-ctx.Done()
		if ctx.Err() == context.DeadlineExceeded {
			return profileExpiredMsg{}
		}
		return nil
	}
}

func newProfilesState(locales ...i18n.Localizer) profilesState {
	var locale i18n.Localizer
	if len(locales) > 0 {
		locale = locales[0]
	}
	input := textinput.New()
	input.SetVirtualCursor(false)
	input.CharLimit = 8192
	input.Placeholder = locale.T("profiles.url_placeholder")
	styleInput(&input, inputBackground, false)
	return profilesState{url: input}
}

func (m *Model) profileURLKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.page != "profiles" || m.focus != "profiles.url" || m.form != "" || m.sidebarOpen || m.busy || m.quitting {
		return false, nil
	}
	m.profilesUI.editing = true
	m.profilesUI.url.Focus()
	switch k.String() {
	case "tab", "shift+tab":
		m.profilesUI.editing = false
		m.profilesUI.url.Blur()
		return false, nil
	case "esc":
		m.profilesUI.editing = false
		m.profilesUI.url.Blur()
		m.focus = "profiles.add"
		return true, nil
	case "enter":
		return true, m.beginProfileURLImport()
	}
	m.profilesUI.pasteDeadline = time.Time{}
	m.profilesUI.pasteUnavailable = false
	var cmd tea.Cmd
	m.profilesUI.url, cmd = m.profilesUI.url.Update(k)
	m.reconcileProfileValidation()
	return true, cmd
}

func (m *Model) requestProfilePaste() tea.Cmd {
	m.focus, m.profilesUI.editing = "profiles.url", true
	focus := m.profilesUI.url.Focus()
	if !m.profilesUI.pasteDeadline.IsZero() {
		return focus
	}
	m.profilesUI.pasteUnavailable = false
	deadline := time.Now().Add(profilePasteTimeout)
	m.profilesUI.pasteDeadline = deadline
	return tea.Batch(focus, tea.ReadClipboard, tea.Tick(profilePasteTimeout, func(time.Time) tea.Msg {
		return profilePasteTimeoutMsg{deadline: deadline}
	}))
}

func (m *Model) finishProfilePaste(content string) tea.Cmd {
	if m.profilesUI.pasteDeadline.IsZero() {
		return nil
	}
	m.profilesUI.pasteDeadline = time.Time{}
	if _, id, active := m.activeInputField(); !active || id != "profiles.url" {
		return nil
	}
	if strings.TrimSpace(content) == "" {
		m.profilesUI.pasteUnavailable = true
		return nil
	}
	return m.pasteProfileURL(tea.PasteMsg{Content: content})
}

func (m *Model) pasteProfileURL(paste tea.PasteMsg) tea.Cmd {
	m.profilesUI.pasteDeadline = time.Time{}
	m.profilesUI.pasteUnavailable = false
	m.profilesUI.editing = true
	m.profilesUI.url.Focus()
	var cmd tea.Cmd
	m.profilesUI.url, cmd = m.profilesUI.url.Update(paste)
	return cmd
}

func (p *profilesState) importSource() string {
	return strings.TrimSpace(p.url.Value())
}

func (m *Model) beginProfileURLImport() tea.Cmd {
	m.profilesUI.pasteDeadline = time.Time{}
	m.profilesUI.pasteUnavailable = false
	source := m.profilesUI.importSource()
	if source == "" {
		return nil
	}
	if err := validateProfileSource(source); err != nil {
		m.profilesUI.urlError, m.profilesUI.urlErrorValue = m.localizeError(err), m.profilesUI.url.Value()
		m.focus, m.profilesUI.editing = "profiles.url", true
		return m.profilesUI.url.Focus()
	}
	m.clearProfileValidation()
	m.profilesUI.editing = false
	m.profilesUI.url.Blur()
	m.profilesUI.importing = true
	m.focus = "profiles.add"
	return m.beginProfileImport(app.ProfileDraft{Address: source}, "profile.add", m.tr("profiles.fetching"))
}

func validateProfileSource(source string) error {
	if filepath.IsAbs(source) {
		return nil
	}
	if !strings.HasPrefix(source, "https://") && !strings.HasPrefix(source, "http://") {
		return diagnostic.New("diagnostic.source.address", "subscription source must be an HTTP(S) URL or absolute file path")
	}
	u, err := url.ParseRequestURI(source)
	if err != nil || u.Hostname() == "" {
		return diagnostic.New("diagnostic.source.invalidURL", "invalid subscription URL")
	}
	return nil
}

func (m *Model) clearProfileValidation() {
	m.profilesUI.urlError, m.profilesUI.urlErrorValue = "", ""
}

func (m *Model) reconcileProfileValidation() {
	if m.page != "profiles" || m.profilesUI.url.Value() != m.profilesUI.urlErrorValue {
		m.clearProfileValidation()
	}
}

func (m *Model) focusProfileControl(id string) {
	m.focus = id
	_, hits := m.pageBody(dashboardBodyWidth(dashboardOptions{Width: m.width, Height: m.height, Page: m.page}))
	for _, hit := range hits {
		if hit.ID == id {
			m.revealBodyTarget(hit)
			break
		}
	}
}

// Detail actions follow a profile's identity when another interface changes the
// library order. A stale numeric focus must never apply a different profile.
func (m *Model) reconcileProfiles() {
	m.reconcileProfileValidation()
	if m.profilesUI.importSource() == "" {
		if m.page == "profiles" && m.focus == "profiles.add" {
			m.focus = "profiles.create"
		}
	}
	if !m.profilesUI.pasteDeadline.IsZero() {
		if _, id, active := m.activeInputField(); !active || id != "profiles.url" {
			m.profilesUI.pasteDeadline = time.Time{}
		}
	}
	if m.page != "profiles" {
		m.profilesUI.pasteUnavailable = false
	}
	if m.form == "profile-settings" && !slices.Contains(m.profiles(), m.name) {
		m.closeProfileCreate()
	}
}

func profileActionIndex(id, prefix string, size int) (int, bool) {
	if !strings.HasPrefix(id, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(id, prefix))
	return n, err == nil && n >= 0 && n < size
}

// Mouse clicks switch installed configurations. Enter opens settings.
func (m *Model) profileCardClick(id string) (bool, tea.Cmd) {
	index, card := profileActionIndex(id, "profile.", len(m.profiles()))
	if m.page != "profiles" || !card {
		return false, nil
	}
	if m.busy || m.quitting {
		return true, nil
	}
	if !m.snapshot.Installed {
		return m.profileAction(id)
	}
	m.profilesUI.editing = false
	m.profilesUI.url.Blur()
	name := m.profiles()[index]
	if name == m.snapshot.Applied {
		return true, nil
	}
	return m.profileAction(fmt.Sprintf("profiles.apply.%d", index))
}

// profileAction routes interface actions into shared app use cases. It does
// not recreate schema or active-profile validation.
func (m *Model) profileAction(id string) (bool, tea.Cmd) {
	index, card := profileActionIndex(id, "profile.", len(m.profiles()))
	if !card && !strings.HasPrefix(id, "profiles.") &&
		!strings.HasPrefix(id, "profile.overrides.") && !strings.HasPrefix(id, "profile.delete.") {
		return false, nil
	}
	if m.busy || m.quitting {
		return true, nil
	}
	if index, ok := profileActionIndex(id, "profiles.menu.", len(m.profiles())); ok {
		m.openProfileMenuForCard(index)
		return true, nil
	}
	if id == "profiles.url" {
		m.focus, m.profilesUI.editing = id, true
		return true, m.profilesUI.url.Focus()
	}
	if id == "profiles.url.paste" && m.page == "profiles" {
		return true, m.requestProfilePaste()
	}
	if id == "profiles.add" {
		return true, m.beginProfileURLImport()
	}
	m.profilesUI.editing = false
	m.profilesUI.url.Blur()
	if card {
		return true, m.openProfileSettings(m.profiles()[index])
	}
	switch id {
	case "profiles.create":
		return true, m.openProfileCreate()
	case "profiles.update-all":
		if len(m.profiles()) == 0 {
			return true, nil
		}
		label := m.tr("profiles.updating_all")
		return true, m.beginResult(label, func(ctx context.Context) (string, error) {
			summary, err := m.backend.RefreshAllProfiles(ctx)
			if err != nil && summary.Updated == 0 && summary.Failed == 0 {
				return "", err
			}
			if summary.Failed > 0 {
				return m.tr("feedback.refresh_partial", summary.Updated, summary.Failed), err
			}
			return m.tr("feedback.refreshed_all", summary.Updated), err
		}, notificationOperation{Key: "profile.refresh-all", Context: m.tr("header.updateAll")})

	}
	profiles := m.profiles()
	for _, action := range []struct{ prefix, operation string }{
		{"profiles.apply.", "apply"}, {"profiles.refresh.", "refresh"}, {"profiles.update.", "refresh"},
		{"profiles.overrides.", "overrides"}, {"profile.overrides.", "overrides"},
		{"profiles.delete.", "delete"}, {"profile.delete.", "delete"},
	} {
		i, ok := profileActionIndex(id, action.prefix, len(profiles))
		if !ok {
			continue
		}
		name := profiles[i]
		switch action.operation {
		case "apply":
			return true, m.applyProfile(name)
		case "refresh":
			return true, m.begin(m.tr("profiles.updating"), m.tr("feedback.refreshed", m.profileFeedbackName(name)), func(ctx context.Context) error { return m.backend.Refresh(ctx, name) },
				m.profileNotification("refresh", m.tr("profiles.update"), name))
		case "overrides":
			return true, m.openProfileEdit("profile-overrides", name)
		case "delete":
			return true, m.openProfileEdit("profile-delete", name)
		}
	}
	return true, nil
}

func (m *Model) canUseDefaultFrom(name string) bool {
	return m.snapshotReady && m.snapshot.Installed && name != "" && name != "default" && name == m.snapshot.Applied
}

func (m *Model) applyProfile(name string) tea.Cmd {
	label := m.tr("profiles.applying")
	return m.begin(label, m.tr("feedback.selected", m.profileFeedbackName(name)), func(ctx context.Context) error { return m.backend.Apply(ctx, name) },
		m.profileNotification("apply", m.tr("profiles.use"), name))
}

func (m *Model) profileNotification(operation, label, name string) notificationOperation {
	return notificationOperation{
		Key:     notificationKey("profile."+operation, name),
		Context: label + " · " + m.profileFeedbackName(name),
	}
}

func (m *Model) profileEditForm() bool {
	return m.form == "profile-overrides" || m.form == "profile-delete"
}

func (m *Model) openProfileEdit(form, name string) tea.Cmd {
	m.form, m.name = form, name
	m.failure = ""
	m.input.Reset()
	m.paths.reset()
	m.input.EchoMode = textinput.EchoNormal
	m.input.Placeholder = ""
	if form == "profile-overrides" {
		m.input.Placeholder = m.tr("profiles.overrides_path")
	}
	m.input.Focus()
	if form == "profile-delete" {
		m.prepareProfileDelete()
		return nil
	}
	return textinput.Blink
}

func (m *Model) closeProfileForm() {
	m.form, m.name = "", ""
	m.profilesUI.deleteFocus, m.profilesUI.deleteReplacement, m.profilesUI.deleteCurrent = "", "", false
	m.input.Reset()
	m.input.Blur()
}

func (m *Model) profileFormKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	if !m.profileEditForm() {
		return false, nil
	}
	if m.form == "profile-delete" {
		if m.busy || m.quitting {
			return true, nil
		}
		if k.String() != "esc" {
			return true, m.profileDeleteKey(k)
		}
	}
	switch k.String() {
	case "esc":
		m.closeProfileForm()
		m.message, m.failure = "", ""
		return true, nil
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		if value == "" {
			m.failure = m.tr("profiles.value_required")
			return true, nil
		}
		form, name := m.form, m.name
		if m.pathForm() {
			var err error
			value, err = resolveLocalPath(value)
			if err != nil {
				m.failure = m.localizeError(err)
				return true, nil
			}
		}
		switch form {
		case "profile-overrides":
			return true, m.begin(m.tr("profiles.importing_overrides"), m.tr("profiles.overrides_saved"), func(ctx context.Context) error {
				raw, err := readProfileOverrides(value)
				if err != nil {
					return err
				}
				return m.backend.SetProfileOverrides(ctx, name, raw)
			}, m.profileNotification("overrides", m.tr("profiles.overrides"), name))
		}
	}
	var cmd tea.Cmd
	before := m.input.Value()
	m.input, cmd = m.input.Update(k)
	if before != m.input.Value() {
		m.failure = ""
	}
	return true, cmd
}

func (m *Model) profileDeleteSetFocus(id string) tea.Cmd {
	m.profilesUI.deleteFocus = id
	m.input.Blur()
	return nil
}

func readProfileOverrides(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, diagnostic.New("diagnostic.overrides_absolute", "overrides input must be an absolute file path")
	}
	raw, err := store.Read(path)
	if err != nil {
		return nil, diagnostic.New("diagnostic.overrides_read", "cannot read overrides; use a regular file of at most 8 MiB")
	}
	if len(raw) > 8<<20 {
		return nil, diagnostic.New("diagnostic.overrides_size", "overrides input exceeds 8 MiB")
	}
	return raw, nil
}

func (m *Model) renderProfileForm(base string) (string, []hitRegion) {
	if m.form == "editor-preference" {
		return m.renderEditorPreference(base)
	}
	if m.form == "profile-document" {
		return m.renderProfileEditor(base)
	}
	if m.profileFieldsForm() {
		return m.renderProfileFieldsForm(base)
	}
	if m.form == "profile-delete" {
		return m.renderProfileDeleteForm(base)
	}
	limit := inputFieldMaxWidth
	if m.pathForm() {
		limit = inputImportMaxWidth
	}
	boxW, boxH := min(limit+2*modalPaddingX, m.width), min(12, m.height)
	if m.pathForm() {
		boxH = min(18, m.height)
	}
	if boxW < 24 || boxH < 7 {
		return m.renderModalNotice(m.tr("profiles.enlarge"))
	}
	title, label, hint := m.tr("profiles.add_subscription"), m.tr("profiles.source_label"), m.tr("profiles.source_hint")
	if m.form == "profile-overrides" {
		title, label = m.tr("profiles.overrides_title", clean(m.profileInfo(m.name).Name)), m.tr("profiles.overrides_path")
		hint = m.tr("profiles.overrides_hint")
	}
	inner := boxW - 2*modalPaddingX
	surface := m.newModalSurface(boxW, boxH)
	put := func(row int, value string) { surface.put(modalPaddingX, row, cellCrop(value, inner)) }
	put(1, themed(cellCrop(title, inner), text, modalBackground, false))
	inputRow := 2
	if boxH >= 11 {
		put(2, themed(cellCrop(hint, inner), muted, modalBackground, false))
		put(3, themed(cellCrop(label, inner), muted, modalBackground, false))
		inputRow = 5
	} else if boxH >= 9 {
		put(2, themed(cellCrop(label, inner), muted, modalBackground, false))
		inputRow = 3
	}
	field := m.formInputField(inner, false, m.hover)
	for row, line := range field.lines {
		put(inputRow+row, line)
	}
	footer := m.tr("profiles.form_footer")
	if m.pathForm() {
		completionRow := inputRow + len(field.lines) + 1
		capacity := max(0, boxH-3-completionRow)
		for row, line := range m.paths.view(inner, capacity, m.locale) {
			put(completionRow+row, line)
		}
		footer = m.tr("profiles.path_footer")
		if boxW < 60 {
			footer = m.tr("profiles.path_footer_short")
		}
		if boxW < 36 {
			footer = m.tr("profiles.path_footer_tiny")
		}
	}
	if m.failure != "" && inputRow+len(field.lines) < boxH-2 {
		put(boxH-3, themed(cellCrop(m.failure, inner), errorColor, modalBackground, false))
	}
	put(boxH-2, themed(cellCrop(footer, inner), muted, modalBackground, false))
	for i, hit := range field.targets {
		hit.X += modalPaddingX
		hit.Y += inputRow
		field.targets[i] = surface.screen(hit)
	}
	return surface.render(base), field.targets
}

func (m *Model) formInputField(width int, compact bool, hover string) dashboardBlock {
	m.input = prepareInputField(m.input, inputFieldTextWidth(width, 0, true), modalBackground, m.failure != "")
	return renderInputField(m.input, inputFieldOptions{
		ID: "import-input", ClearID: "import-input.clear", Width: width, Surface: modalBackground, Compact: compact,
		ClearHovered: hover == "import-input.clear", ClearFocused: m.focus == "import-input.clear",
		Focused: m.input.Focused(), Hovered: hover == "import-input", Invalid: m.failure != "", Disabled: m.busy || m.quitting,
	})
}
