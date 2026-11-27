// Package tui presents shared application use cases in a terminal.
package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type Backend interface {
	Snapshot(context.Context) (app.Snapshot, error)
	LookupIPInfo(context.Context) (app.IPInfo, error)
	AddWebsite(context.Context, app.Website) ([]app.Website, error)
	UpdateWebsite(context.Context, app.Website, app.Website) ([]app.Website, error)
	RelocateWebsite(context.Context, app.Website, app.Website, bool) ([]app.Website, error)
	RemoveWebsite(context.Context, app.Website) ([]app.Website, error)
	TestWebsite(context.Context, app.Website) (app.WebsiteTestResult, error)
	ReadPreferences(context.Context) (app.Preferences, error)
	SaveNetworkPreferences(context.Context, downloadnet.Config) error
	SetGuideDismissed(context.Context, bool) error
	SaveLanguagePreference(context.Context, string) error
	SaveStartPagePreference(context.Context, string) error
	SaveThemePreference(context.Context, string) error
	SaveEditorPreference(context.Context, string) error
	SaveKeyHintsPreference(context.Context, bool) error
	SetAutostart(context.Context, bool) error
	SetCoreSetting(context.Context, string, string) error
	UpdateActiveGeo(context.Context) error
	PrepareDefaultGeo(context.Context, downloadnet.Config, func(mihomo.Progress)) (app.GeoPreparation, error)
	SetMode(context.Context, string) error
	SetTUN(context.Context, bool) error
	CheckUpdate(context.Context, mihomo.Channel, downloadnet.Config) (app.CoreUpdateCheck, error)
	UpgradeCore(context.Context, app.CoreUpdateCheck, downloadnet.Config, func(mihomo.Progress)) (app.CoreUpgradeResult, error)
	ResolveRelease(context.Context, string, downloadnet.Config) (mihomo.Release, error)
	TestReleaseFile(context.Context, mihomo.Release, downloadnet.Config) error
	PrepareInstall(context.Context, mihomo.Release, string, bool) (app.InstallPlan, error)
	InstallCore(context.Context, app.InstallPlan, downloadnet.Config, func(mihomo.Progress)) (app.InstallResult, error)
	ImportProfile(context.Context, app.ProfileDraft) (string, error)
	GetProfileDocument(context.Context, string, app.ProfileDocumentKind) (app.ProfileDocument, error)
	SaveProfileDocument(context.Context, app.ProfileDocument, []byte) error
	GetProfileSettings(context.Context, string) (app.ProfileSettings, error)
	SaveProfileSettings(context.Context, app.ProfileSettings) error
	Refresh(context.Context, string) error
	RefreshAllProfiles(context.Context) (app.ProfileRefreshSummary, error)
	DeleteProfileWithReplacement(context.Context, string, string) error
	DeactivateProfile(context.Context, string) error
	SetProfileOverrides(context.Context, string, []byte) error
	Apply(context.Context, string) error
	Control(context.Context, string) error
	Select(context.Context, string, string) error
	StreamRuntime(context.Context, func(app.RuntimeEvent)) error
	CloseObservedConnection(context.Context, string, time.Time) error
	CloseAllConnections(context.Context) error
	Rules(context.Context) (app.RuleSnapshot, error)
	GetRuntimeSettings(context.Context) (app.RuntimeSettings, error)
	BindAddressOptions(context.Context) []app.BindAddressOption
	GetProxyAuth(context.Context) (app.ProxyAuthSettings, error)
	PrepareProxyAuthRules(context.Context, app.ProxyAuthSettings, []app.ProxyAuthUser, string) (app.RuntimeSettingsPlan, error)
	GetProxyAddress(context.Context, app.ProxyAuthSettings, string) (string, error)
	PrepareRuntimeSettingsEdit(context.Context, app.RuntimeSettings, app.RuntimeSettings) (app.RuntimeSettingsPlan, error)
	ApplyRuntimeSettings(context.Context, app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error)
}

type parentCanceledMsg struct{}
type languageSavedMsg struct {
	id       uint64
	language string
	err      error
}
type resultMsg struct {
	id          uint64
	snapshot    app.Snapshot
	hasSnapshot bool
	// actionSucceeded tracks the mutation separately from the following read.
	// A refresh failure must not leave a committed creation ready to submit again.
	actionSucceeded bool
	previousProfile string
	err             error
	success         string
	transient       bool
}
type wizardMsg struct {
	updateCheck   app.CoreUpdateCheck
	upgradeResult app.CoreUpgradeResult
	refreshError  error
	progress      mihomo.Progress
	id            uint64
	kind          string
	release       mihomo.Release
	plan          app.InstallPlan
	result        app.InstallResult
	snapshot      app.Snapshot
	hasSnapshot   bool
	err           error
}
type progressMsg struct {
	id       uint64
	progress mihomo.Progress
}
type nextProgressMsg struct {
	progressMsg
	ctx     context.Context
	updates <-chan progressMsg
}
type networkMsg struct {
	id      uint64
	kind    string
	release mihomo.Release
	err     error
}

// Bubble Tea receives a Model value at each update boundary. Internal helpers
// use pointer receivers to avoid copying all the page and input states again.
type Model struct {
	websiteMenu           app.Website
	websiteEditor         websiteEditorState
	coreNumber            coreNumberEditor
	locale                i18n.Localizer
	languageOverride      string
	savedLanguage         string
	startPage             string
	initialPreferencesErr error
	editorPreference      string
	theme                 string
	terminalLight         bool
	ctx                   context.Context
	backend               Backend
	snapshot              app.Snapshot
	snapshotReady         bool
	snapshotStale         bool // A mutation finished without a new full observation.
	busy, quitting        bool
	id                    uint64
	cancel                context.CancelFunc
	opCtx                 context.Context
	job, message, failure string
	notification          notificationState
	messageTransient      bool
	messageInformational  bool
	page, focus, hover    string
	tooltip               tooltipState
	settingsTab           coreSettingsTab
	settingsPointerFocus  bool
	settingsFromOverview  bool
	tunFocus              string
	guideSkipped          bool
	scroll, total         int
	logoFrame             int
	logoGeneration        uint64
	logoIntro             logoIntroState
	sidebarOpen           bool
	sidebar               sidebarState
	keyHints              keyHintsState
	pageViews             map[string]pageViewState
	form, name            string
	profilesUI            profilesState
	proxies               proxiesState
	overviewGroup         string
	profileEditor         profileEditorState
	profileCreate         profileCreateState
	menu                  actionMenu
	input                 textinput.Model
	paths                 pathCompletion
	spinner               spinner.Model
	width, height         int
	targets               []hitRegion
	screen                string
	colorProfile          colorprofile.Profile
	noColor               bool
	statusGeneration      uint64
	statusCancel          context.CancelFunc
	wizard                wizardState
	runtime               runtimeState
	ipInfo                overviewIPState
	overviewRoute         overviewProxyRoute
	websiteTests          websiteTestState
	websiteDrag           overviewWebsiteDragState
	websites              []app.Website
	websiteOffset         int
	connections           connectionsState
	rules                 rulesState
	providers             providerPanelState
	logs                  logsState
	ports                 portsState
}

func New(ctx context.Context, backend Backend, languages ...string) Model {
	if ctx == nil {
		ctx = context.Background()
	}
	input := textinput.New()
	input.CharLimit = 8192
	styleInput(&input, inputBackground, false)
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	opCtx, cancel := context.WithCancel(ctx)
	m := Model{ctx: ctx, backend: backend, busy: true, id: 1, cancel: cancel, opCtx: opCtx, job: "refresh", input: input, spinner: spin, width: 80, height: 24, wizard: newWizard(), proxies: newProxiesState(), profileCreate: newProfileCreateState()}
	if len(languages) > 0 {
		m.languageOverride = languages[0]
	}
	// Read the small, atomically published preference file before rendering.
	// The first frame can show the selected page while the service snapshot loads.
	prefs, err := backend.ReadPreferences(ctx)
	if err != nil {
		m.initialPreferencesErr = err
		prefs = app.Preferences{}
	}
	m.websites = prefs.WebsitesForTesting()
	m.startPage = cmp.Or(prefs.StartPage, app.StartPageOverview)
	m.page = m.startPage
	if m.page == app.StartPageOverview {
		m.focus = "install"
	}
	m.savedLanguage = prefs.Language
	m.locale = i18n.New(i18n.Resolve(m.languageOverride, m.savedLanguage, os.Getenv))
	m.theme = cmp.Or(prefs.Theme, app.ThemeDark)
	m.editorPreference = prefs.Editor
	m.keyHints.hidden = prefs.HideKeyHints
	m.wizard.cfg = prefs.Network.Normalized()
	m.wizard.draft = m.wizard.cfg
	m.profilesUI = newProfilesState(m.locale)
	m.message = m.tr("model.loading")
	m.connections.search = textinput.New()
	m.connections.search.SetVirtualCursor(false)
	m.connections.search.CharLimit = 256
	m.connections.search.Placeholder = m.tr("model.connection_search")
	m.rules.search = textinput.New()
	m.rules.search.SetVirtualCursor(false)
	m.rules.search.CharLimit = 512
	m.rules.search.Placeholder = m.tr("model.rule_search")
	m.providers = newProviderPanelState(m.locale)
	m.logs = newLogsState(m.locale)
	styleInput(&m.connections.search, bg, false)
	styleInput(&m.rules.search, inputBackground, false)
	env := terminalColorEnvironment(os.Environ())
	m.colorProfile = themeProfile(env, colorprofile.Env(env))
	m.noColor = env.Getenv("NO_COLOR") != ""
	m.prepareNotification("refresh", "")
	m.syncNotification()
	m.layout()
	return m
}
func Run(ctx context.Context, backend Backend, languages ...string) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	last, err := tea.NewProgram(New(runCtx, backend, languages...),
		tea.WithEnvironment(terminalColorEnvironment(os.Environ())), tea.WithoutSignalHandler()).Run()
	if m, ok := last.(Model); ok && m.profileEditor.tempDir != "" {
		fmt.Fprintln(os.Stderr, m.tr("profile_editor.recovery_file", clean(m.profileEditor.path)))
	}
	return err
}
func (m Model) Init() tea.Cmd {
	ctx := m.ctx
	var background tea.Cmd
	if m.theme == app.ThemeAuto {
		background = tea.RequestBackgroundColor
	}
	return tea.Batch(m.snapshotCmd(m.id, m.opCtx, true), background, m.spinner.Tick, func() tea.Msg { <-ctx.Done(); return parentCanceledMsg{} })
}

func (m *Model) snapshotCmd(id uint64, ctx context.Context, initial bool) tea.Cmd {
	backend := m.backend
	var preferencesErr error
	if initial {
		preferencesErr = m.initialPreferencesErr
	}
	return func() tea.Msg {
		s, err := backend.Snapshot(ctx)
		return resultMsg{id: id, snapshot: s, hasSnapshot: err == nil, err: errors.Join(preferencesErr, err), success: "State refreshed", transient: true}
	}
}
func (m *Model) newJob(kind string, operations ...notificationOperation) context.Context {
	m.stopProxyTests()
	m.stopRules()
	m.stopProviders()
	m.stopCoreStatus()
	if m.cancel != nil {
		m.cancel()
	}
	m.id++
	m.opCtx, m.cancel = context.WithCancel(m.ctx)
	m.busy = true
	m.job = kind
	op := notificationOperation{Key: kind}
	if len(operations) > 0 {
		op = operations[0]
	}
	// An automatic wizard phase gets its own cancellable request but keeps
	// the foreground notification of the user action that started the chain.
	if op.ID == 0 || op.ID != m.notification.operation.ID {
		m.startNotification(op)
	}
	m.failure = ""
	m.messageTransient = false
	m.messageInformational = false
	return m.opCtx
}
func (m *Model) abortLookup() {
	for _, item := range m.notification.items {
		if item.Operation == m.notification.operation.ID && item.Kind == "working" {
			m.dismissNotificationID(item.ID)
			break
		}
	}
	m.message, m.failure = "", ""
	if m.cancel != nil {
		m.cancel()
	}
	m.id++
	m.busy = false
	m.job = ""
	m.wizard.installRequested = false
}
func (m *Model) refresh() tea.Cmd {
	ctx := m.newJob("refresh")
	m.message = m.tr("model.refreshing")
	return tea.Batch(m.snapshotCmd(m.id, ctx, false), m.spinner.Tick)
}
func (m *Model) begin(label, success string, work func(context.Context) error, operations ...notificationOperation) tea.Cmd {
	return m.beginResult(label, func(ctx context.Context) (string, error) {
		if err := work(ctx); err != nil {
			return "", err
		}
		return success, nil
	}, operations...)
}

// work may return both a user-facing summary and an error for partial commits.
func (m *Model) beginResult(label string, work func(context.Context) (string, error), operations ...notificationOperation) tea.Cmd {
	if len(operations) == 0 {
		operations = []notificationOperation{{Key: "action", Context: label}}
	}
	ctx := m.newJob("action", operations...)
	id := m.id
	backend := m.backend
	previousProfile := m.snapshot.Applied
	m.message = label
	return tea.Batch(func() tea.Msg {
		success, err := work(ctx)
		actionSucceeded := err == nil
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		s, se := backend.Snapshot(refreshCtx)
		if se != nil {
			err = errors.Join(err, diagnostic.New("diagnostic.refresh_state", "refresh current state: %w", se))
		}
		return resultMsg{id: id, snapshot: s, hasSnapshot: se == nil, actionSucceeded: actionSucceeded, previousProfile: previousProfile, err: err, success: success}
	}, m.spinner.Tick)
}
func waitProgress(ctx context.Context, updates <-chan progressMsg) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-updates:
			if !ok {
				return nil
			}
			return nextProgressMsg{progressMsg: msg, ctx: ctx, updates: updates}
		}
	}
}
func (m *Model) cancelTask(quit bool) {
	m.stopProviders()
	m.stopProxyTests()
	m.stopRules()
	if quit {
		m.stopRuntime()
	}
	m.stopCoreStatus()
	if m.cancel != nil {
		m.cancel()
	}
	m.quitting = m.quitting || quit
	m.wizard.installRequested = false
	m.message = m.tr("model.canceling")
	if m.job == "install" {
		m.wizard.cancelling = true
		m.wizard.progress.Detail = "Waiting for safe cleanup before leaving"
	}
	if m.ports.open {
		m.ports.cancelling = true
	}
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if mouse, ok := msg.(tea.MouseMsg); ok && m.page == "overview" && m.form == "" {
		wasDragging := m.websiteDrag.active()
		if handled, cmd := m.overviewWebsiteDragMouse(mouse); handled {
			m.layout()
			return m, cmd
		}
		if wasDragging && !m.websiteDrag.active() {
			// A lost release may be followed by a hover on empty space.
			// Repaint the cleared gesture even when hover itself stays empty.
			m.layout()
		}
	}
	if motion, ok := msg.(tea.MouseMotionMsg); ok {
		if handled, cmd := m.updateDashboardHover(motion); handled {
			return m, cmd
		}
	}
	if mouse, ok := msg.(tea.MouseMsg); ok && m.updateRulesMouse(mouse) {
		return m, nil
	}
	if wheel, ok := msg.(tea.MouseWheelMsg); ok && m.updateProxyWheel(wheel) {
		return m, nil
	}
	var cmd tea.Cmd
	previousPage := m.page
	switch v := msg.(type) {
	case tooltipDelayMsg:
		if !m.tooltip.showAfterDelay(v) {
			return m, nil
		}
		m.renderLayout()
		return m, m.syncNotificationTimers(time.Now())
	case tea.BlurMsg:
		m.cancelOverviewWebsiteDrag()
		if strings.HasPrefix(m.hover, overviewWebsiteDragPrefix) {
			m.hover = ""
		}
	case websiteListSavedMsg:
		cmd = m.acceptWebsiteList(v)
	case websiteChangeMsg:
		cmd = m.acceptWebsiteChange(v)
	case profileEditorOpenedMsg:
		cmd = m.acceptProfileEditor(v)
	case profileEditorExitedMsg:
		cmd = m.finishProfileEditor(v)
	case profileSettingsLoadedMsg:
		cmd = m.acceptProfileSettings(v)
	case proxyDelayMsg:
		cmd = m.acceptProxyDelay(v)
	case proxyNodeDelayMsg:
		cmd = m.acceptProxyNodeDelay(v)
	case proxyTestStatusExpiredMsg:
		if v.generation == m.proxies.generation && !m.proxies.testing {
			m.proxies.testStatusVisible = false
		}
	case keyHintsSavedMsg:
		cmd = m.acceptKeyHintsSaved(v)
	case startPageSavedMsg:
		if v.id != m.id {
			return m, nil
		}
		m.cancel()
		m.busy, m.job = false, ""
		if v.err != nil {
			m.failure, m.message = m.localizeError(v.err), ""
		} else {
			m.startPage = v.page
			m.message = m.tr("settings.startPageSaved")
		}
		if m.quitting && !m.keyHints.pending {
			return m, tea.Quit
		}
	case themeSavedMsg:
		if v.id != m.id {
			return m, nil
		}
		m.cancel()
		m.busy, m.job = false, ""
		if v.err != nil {
			m.failure, m.message = m.localizeError(v.err), ""
		} else {
			cmd = m.applyThemePreference(v.theme)
			m.message = m.tr("settings.themeSaved")
		}
		if m.quitting && !m.keyHints.pending {
			return m, tea.Quit
		}
	case notificationExpiredMsg:
		m.expireNotification(v)
	case editorPreferenceSavedMsg:
		if v.id != m.id {
			return m, nil
		}
		m.cancel()
		m.busy, m.job = false, ""
		if v.err != nil {
			m.failure = m.localizeError(v.err)
		} else {
			m.editorPreference = v.value
			m.closeEditorPreference()
		}
		if m.quitting && !m.keyHints.pending {
			return m, tea.Quit
		}
	case languageSavedMsg:
		if v.id != m.id {
			return m, nil
		}
		if m.cancel != nil {
			m.cancel()
		}
		m.busy, m.job = false, ""
		m.messageInformational = false
		if v.err != nil {
			m.notification.details = m.locale.Error(v.err)
			m.failure = m.tr("language.save_failed", m.localizeError(v.err))
			m.message = ""
		} else {
			m.locale = i18n.New(v.language)
			m.languageOverride = v.language
			m.savedLanguage = v.language
			m.message, m.failure = m.tr("language.saved"), ""
			m.notification.operation.Context = m.tr("dashboard.language.title")
			m.refreshLocalizedInputs()
		}
		if m.quitting && !m.keyHints.pending {
			return m, tea.Quit
		}
	case portsMsg:
		if v.id != m.id || !m.ports.open {
			return m, nil
		}
		m.acceptPorts(v)
		if m.quitting && !m.busy && !m.keyHints.pending {
			return m, tea.Quit
		}
	case proxyCopyMsg:
		cmd = m.acceptProxyCopy(v)
	case coreStatusMsg:
		if v.generation == m.statusGeneration {
			if m.statusCancel != nil {
				m.statusCancel()
				m.statusCancel = nil
			}
			if v.err == nil {
				m.adoptSnapshot(v.snapshot)
			} else if !errors.Is(v.err, context.Canceled) {
				m.snapshot.ServiceError = "Service status unavailable; refresh or check doctor"
			}
		}
	case tea.ColorProfileMsg:
		m.colorProfile = v.Profile
		m.logoIntro.colored = true
	case tea.BackgroundColorMsg:
		if v.Color != nil && m.theme == app.ThemeAuto {
			m.terminalLight = !v.IsDark()
		}
	case providersResultMsg:
		cmd = m.acceptProviders(v)
	case providerUpdateResultMsg:
		cmd = m.acceptProviderUpdate(v)
	case providersTickMsg:
		if m.providers.active && v.kind == m.providers.kind && v.source == m.providerSource() && v.generation == m.providers.generation && v.request == m.providers.request {
			cmd = m.providersCmd()
		}
	case rulesResultMsg:
		accepted := m.rules.active && m.rules.pending && v.generation == m.rules.generation && m.rules.source == m.currentRulesSource()
		cmd = m.acceptRules(v)
		if accepted && v.err == nil {
			cmd = tea.Batch(cmd, m.observeCoreState(v.snapshot.State))
		}
	case runtimeResultMsg:
		cmd = m.acceptRuntime(v)
	case ipInfoResultMsg:
		m.acceptIPInfo(v)
	case websiteTestMsg:
		cmd = m.acceptWebsiteTest(v)
	case logRecoveryExpiredMsg:
		if m.logs.recoveryUntil == v.until {
			m.logs.recoveryUntil = time.Time{}
		}
	case runtimeEndedMsg:
		cmd = m.acceptRuntimeEnded(v)
	case runtimeRetryMsg:
		if v.generation == m.runtime.generation && !m.runtime.active {
			cmd = m.syncRuntime()
		}
	case parentCanceledMsg:
		m.ipInfo.stop()
		m.websiteTests.clear()
		m.stopProxyTests()
		m.stopRuntime()
		m.stopRules()
		m.stopProviders()
		m.stopCoreStatus()
		if m.busy {
			m.cancelTask(true)
		} else {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.cancelOverviewWebsiteDrag()
		if m.page == "settings" || m.tooltip.pointer.ID != "" || strings.HasPrefix(m.hover, overviewWebsiteDragPrefix) {
			m.hover = ""
		}
		if m.page == "proxies" && !m.proxiesNarrow() && max(1, v.Width) < sidebarMinWidth {
			if _, group, _, ok := m.proxyAtFocus(); ok && m.proxyGroupExpanded(group.Name) {
				m.proxies.activeGroup = group.Name
			}
		}
		if m.logoIntro.sized && (m.width != max(1, v.Width) || m.height != max(1, v.Height)) {
			m.stopLogoIntro()
		}
		m.logoIntro.sized = true
		m.sidebar.reveal = true
		m.width, m.height = max(1, v.Width), max(1, v.Height)
		if strings.HasPrefix(m.focus, "website.site.") {
			for i, site := range m.websites {
				if websiteActionID(site) == m.focus {
					m.websiteOffset = i
					break
				}
			}
		}
		if m.page == "connections" {
			m.resizeConnectionSortFocus()
		}
		m.wizard.reveal = true
		m.ports.reveal = true
		if m.page == "connections" && m.connections.detail == nil && strings.HasPrefix(m.focus, "connection.") {
			m.revealConnection(0)
		}
		if m.page == "rules" && strings.HasPrefix(m.focus, "rule.") {
			m.revealRule()
		}
		if m.page == "profiles" && !m.sidebarOpen {
			m.focusProfileControl(m.focus)
		}
		m.revealContentFocus()
	case resultMsg:
		m.connections.finishCloseRequest(v)
		if v.id != m.id {
			return m, nil
		}
		if v.id == 1 {
			m.logoIntro.loaded = true
			if v.err != nil {
				m.logoIntro.used = true
			}
		}
		if m.cancel != nil {
			m.cancel()
		}
		m.busy = false
		m.job = ""
		if v.actionSucceeded && v.err == nil && v.hasSnapshot {
			v.success, m.notification.warning = m.controlCompletion(v.snapshot, v.success)
		}
		m.proxies.pendingGroup, m.proxies.pendingNode = "", ""
		if v.hasSnapshot {
			m.adoptSnapshot(v.snapshot)
		}
		if m.profilesUI.importing {
			if v.actionSucceeded {
				m.profilesUI.url.Reset()
			}
			m.profilesUI.importing = false
		}
		if m.form == "profile-document" && v.actionSucceeded {
			m.closeProfileEditor()
			m.notification.operation.Dialog = false
		}
		if m.form == "profile-overrides" && v.actionSucceeded {
			m.closeProfileForm()
			m.notification.operation.Dialog = false
		}
		if m.profileFieldsForm() && m.profileCreate.submitting {
			m.profileCreate.submitting = false
			if v.actionSucceeded {
				if m.form == "profile-create" {
					m.notification.operation.Dialog = false
				}
				m.closeProfileCreate()
			}
		}
		m.messageInformational = false
		if v.err != nil {
			m.notification.details = m.locale.Error(v.err)
			m.notification.critical = diagnostic.RequiresAttention(v.err)
			if !v.hasSnapshot && m.notification.operation.Key != "refresh" && m.snapshotReady {
				m.markOperationStateUnknown()
			}
			m.failure = m.localizeError(v.err)
			m.message = m.tr("model.failed")
			if v.success != "" && !v.transient {
				// The mutation may have committed even when the subsequent observation failed.
				m.message = v.success
				if v.actionSucceeded {
					combined := v.success + " · " + m.failure
					if ansi.StringWidth(combined) <= 200 {
						m.failure = combined
					} else {
						m.failure = m.tr("feedback.refresh_failed", v.success)
					}
				} else {
					m.failure = v.success
				}
			}
			if diagnostic.CancellationOnly(v.err) {
				m.message = m.tr("model.canceled")
				if !v.transient && v.success != "" {
					m.message += " · " + v.success
				}
				m.messageInformational = true
				m.failure = ""
			}
		} else {
			m.message = v.success
			m.messageTransient = v.transient
			m.failure = ""
		}
		m.finishProfileSelection(v)
		if m.quitting && !m.keyHints.pending {
			return m, tea.Quit
		}
		if m.wizard.inline && m.wizard.step == stepResult && m.notification.operation.Key == "service.start" && v.actionSucceeded {
			m.wizard.inline = false
			if m.page == "overview" && strings.HasPrefix(m.focus, "setup.") {
				m.focus = "core.toggle"
			}
		}
	case wizardMsg:
		if v.id != m.id {
			return m, nil
		}
		if m.cancel != nil {
			m.cancel()
		}
		m.busy = false
		m.job = ""
		if v.hasSnapshot {
			m.adoptSnapshot(v.snapshot)
		}
		cmd = m.handleInlineWizardResult(v)
		if v.kind == "install" || v.kind == "upgrade" {
			m.notification.operation.Context = m.tr("notification.install")
			if v.kind == "upgrade" {
				m.notification.operation.Context = m.tr("core_update.title")
			}
			m.notification.critical = diagnostic.RequiresAttention(v.err)
			if !v.hasSnapshot && m.snapshotReady {
				m.markOperationStateUnknown()
			}
			if m.notification.critical || m.notification.retainDialog {
				m.notification.details = m.locale.Error(v.err)
				m.failure = m.localizeError(v.err)
			}
		}
		if m.quitting && !m.keyHints.pending {
			return m, tea.Quit
		}
	case networkMsg:
		if v.id != m.id {
			return m, nil
		}
		cmd = m.handleNetworkResult(v)
		if m.quitting && !m.busy && !m.keyHints.pending {
			return m, tea.Quit
		}
	case nextProgressMsg:
		if v.id == m.id && m.busy {
			m.wizard.progress = v.progress
			m.wizard.logs = append(m.wizard.logs, m.stageLabel(v.progress.Stage)+": "+clean(m.localizeDetail(v.progress.Detail)))
			if len(m.wizard.logs) > 80 {
				m.wizard.logs = m.wizard.logs[len(m.wizard.logs)-80:]
			}
			cmd = waitProgress(v.ctx, v.updates)
		}
	case spinner.TickMsg:
		if m.busy {
			m.spinner, cmd = m.spinner.Update(v)
		}
	case logoPulseMsg:
		cmd = m.advanceLogoPulse(v)
	case logoIntroMsg:
		cmd = m.advanceLogoIntro(v)
	case pathMatchesMsg:
		if m.pathForm() {
			m.paths.update(v)
		}
	case tea.ClipboardMsg:
		if v.Clipboard() == ansi.SystemClipboard {
			cmd = m.finishProfilePaste(v.Content)
		}
	case profilePasteTimeoutMsg:
		if v.deadline == m.profilesUI.pasteDeadline {
			cmd = m.finishProfilePaste("")
		}
	case tea.MouseMsg:
		previousScroll := m.scroll
		cmd = m.mouse(v)
		if _, wheel := v.(tea.MouseWheelMsg); wheel && m.page == "rules" && m.scroll != previousScroll {
			g := dashboardGeometry(m.dashboardOptions())
			m.scroll = min(m.scroll, max(0, m.total-g.height))
			point := v.Mouse()
			m.hover = m.rulesPointerTarget(point.X, point.Y)
		}
	case tea.PasteMsg:
		// Bracketed paste is text input, never a global command. In particular a
		// pasted q or digit cannot quit or navigate out of a form.
		if m.coreNumber.id != "" {
			cmd = m.updateCoreNumber(v)
			break
		}
		if m.notification.panel.open {
			break
		} else if m.menu.open {
			break
		} else if m.providers.open {
			if m.providers.editing {
				m.providers.search, cmd = m.providers.search.Update(v)
				m.providers.viewport.GotoTop()
				m.reconcileProviders()
			}
		} else if m.ports.open {
			cmd = m.updatePortsInput(v)
		} else if m.wizard.open || m.wizard.networkOpen {
			cmd = m.updateWizardInput(v)
		} else if m.inlineInputActive() {
			cmd = m.updateInlineInput(v)
		} else if m.form == "website-editor" {
			cmd = m.updateWebsiteEditorInput(v)
		} else if m.form == "tun-confirm" || m.form == "profile-delete" || m.form == "profile-document" || m.form == "profile-overrides" && m.busy {
			break
		} else if m.form != "" {
			if m.profileFieldsForm() {
				input := m.profileCreateActive()
				if m.busy || m.quitting || input == nil {
					break
				}
				m.profileCreate.manualScroll = false
				before := input.Value()
				*input, cmd = input.Update(v)
				if before != input.Value() {
					m.failure = ""
					if m.profileCreate.field == profileCreateSource {
						m.paths.reset()
					}
				}
			} else {
				before := m.input.Value()
				m.input, cmd = m.input.Update(v)
				if before != m.input.Value() {
					m.failure = ""
				}
			}
		} else if m.page == "profiles" && m.focus == "profiles.url" && !m.busy && !m.quitting && !m.sidebarOpen {
			cmd = m.pasteProfileURL(v)
		} else if m.proxies.editing {
			m.proxies.search, cmd = m.proxies.search.Update(v)
			m.proxySearchChanged()
		} else if m.connections.editing {
			m.connections.search, cmd = m.connections.search.Update(v)
			m.reconcileConnections()
		} else if m.rules.editing {
			m.rules.search, cmd = m.rules.search.Update(v)
			m.reconcileRules()
		} else if m.logs.editing {
			m.logs.search, cmd = m.logs.search.Update(v)
			m.resetLogsView()
		}
	case tea.KeyPressMsg:
		if m.page == "settings" || m.page == "overview" || m.tooltip.pointer.ID != "" {
			m.hover = ""
		}
		key := v.String()
		if m.websiteDrag.active() {
			m.cancelOverviewWebsiteDrag()
			if key == "esc" {
				break
			}
		}
		if key == "ctrl+c" {
			cmd = m.requestQuit()
			if cmd != nil {
				return m, cmd
			}
			break
		}
		if m.coreNumber.id != "" {
			cmd = m.updateCoreNumber(v)
			break
		}
		if m.providers.open {
			_, cmd = m.providersKey(v)
			break
		}
		if m.menu.open {
			cmd = m.applyMenuResult(m.menu.key(v))
			break
		}
		if m.ports.open {
			cmd = m.portsKey(v)
			break
		}
		// A size notice replaces the dialog controls. Keep Escape available
		// without letting an invisible button receive focus or activation.
		if (m.wizard.open || m.wizard.networkOpen || m.form != "") && len(m.targets) == 0 && key != "esc" {
			break
		}
		if m.wizard.open || m.wizard.networkOpen {
			cmd = m.wizardKey(v)
			break
		}
		if m.form != "" {
			cmd = m.formKey(v)
			break
		}
		if m.notification.panel.open {
			m.notificationAction(m.notification.panel.key(v))
			break
		}
		if handled, next := m.inlineInstallKey(v); handled {
			cmd = next
			break
		}
		if handled, next := m.profileURLKey(v); handled {
			cmd = next
			break
		}
		if m.page == "connections" && m.connections.editing {
			cmd = m.connectionSearchKey(v)
			break
		}
		if m.page == "proxies" && m.proxies.editing {
			cmd = m.proxySearchKey(v)
			break
		}
		if m.page == "rules" && m.rules.editing {
			cmd = m.ruleSearchKey(v)
			break
		}
		if m.page == "logs" && m.logs.editing {
			cmd = m.logsSearchKey(v)
			break
		}
		if m.page == "connections" && (m.connections.detail != nil || m.connections.confirming) {
			_, cmd = m.connectionKey(v)
			break
		}
		if key == "?" || key == "？" {
			if !v.IsRepeat {
				cmd = m.activate("key-hints.toggle")
			}
			break
		}
		if m.tooltip.key(key, m.focus) {
			if m.tooltip.open != "" && m.tooltip.open == m.tooltip.offscreen.ID {
				m.revealBodyTarget(m.tooltip.offscreen)
			}
			break
		}
		// Editors and overlays above own their keys. Only page navigation or
		// activation makes a mouse-focused settings control visibly focused.
		switch key {
		case "tab", "shift+tab", "enter", "space", " ":
			m.settingsPointerFocus = false
		}
		if m.websiteMenuKey(key) {
			break
		}
		if m.settingsTabNavigation(key) || m.settingsDirectionKey(key) || m.overviewModeNavigation(key) {
			m.settingsPointerFocus = false
			break
		}
		if key == "left" || key == "h" {
			// Editors and overlays above own their keys. Let horizontal controls
			// handle the original key before reusing the existing Escape hierarchy.
			if m.profileGridKey(v) {
				break
			}
			if m.page == "proxies" && !m.sidebarOpen && m.proxyHorizontal(-1) {
				break
			}
			if m.page == "proxies" && !strings.HasPrefix(m.focus, "nav.") && !m.sidebarOpen && m.proxyBack() {
				break
			}
			v = tea.KeyPressMsg{Code: tea.KeyEscape}
			key = v.String()
		}
		if key == "ctrl+n" {
			m.notificationAction("notification.expand")
			break
		}
		if strings.HasPrefix(m.focus, "notification.") {
			handled := true
			switch key {
			case "enter", "space", " ":
				m.notificationAction(m.focus)
			case "esc":
				m.dismissFocusedNotification()
			case "tab":
				m.cycleFocus(1)
			case "shift+tab":
				m.cycleFocus(-1)
			default:
				handled = false
			}
			if handled {
				break
			}
		}
		if m.page == "proxies" && !m.sidebarOpen {
			if key == "/" {
				cmd = m.focusProxySearch()
				break
			}
			if v.Code == tea.KeyEscape && m.closeProxySearch() {
				break
			}
		}
		if key == "q" {
			cmd = m.requestQuit()
			if cmd != nil {
				return m, cmd
			}
			break
		}
		if m.profileMenuKey(v) {
			break
		}
		if m.navigationKey(v) {
			m.settingsPointerFocus = false
			break
		}
		if m.sidebarOpen {
			// The compact navigation popup owns input until a page is opened or
			// it is dismissed. Page searches must not activate behind the menu.
			allowed := false
			switch key {
			case "1", "2", "3", "4", "5", "6", "7", "enter", "space", " ", "esc", "tab", "shift+tab":
				allowed = true
			}
			if !allowed {
				break
			}
		}
		if key >= "1" && key <= "7" && len(key) == 1 {
			m.settingsPointerFocus = false
			m.navigate(navPages()[int(key[0]-'1')])
			break
		}
		if key == "esc" && m.settingsFromOverview && !m.sidebarOpen {
			m.navigate("overview")
			break
		}
		if strings.HasPrefix(m.focus, "nav.") {
			switch key {
			case "enter", "space", " ", "tab", "shift+tab":
				if key == "tab" || key == "shift+tab" {
					if m.sidebarOpen {
						m.enterContent()
					} else if key == "tab" {
						m.cycleFocus(1)
					} else {
						m.cycleFocus(-1)
					}
				} else {
					m.enterContent()
				}
			case "esc":
				if m.sidebarOpen {
					m.sidebarOpen, m.focus = false, "menu"
				}
			}
			break
		}
		if m.focus == "menu" && (key == "enter" || key == "space" || key == " ") {
			cmd = m.activate("menu")
			break
		}
		if m.busy {
			if m.focus == geoMaintenanceID && (key == "enter" || key == "space" || key == " ") {
				cmd = m.manageGeo()
				break
			}
			if (key == "enter" || key == "space" || key == " ") && m.focus == "key-hints.toggle" {
				cmd = m.activateControl(m.focus, &v)
				break
			}
			switch key {
			case "up", "down", "left", "right", "j", "k", "h", "l", "home", "end", "pgup", "pgdown", "esc":
				if m.contentNavigationKey(v) {
					break
				}
				handled := false
				if m.page == "connections" {
					handled, cmd = m.connectionKey(v)
				}
				if m.page == "rules" {
					handled, cmd = m.ruleKey(v)
				}
				if m.page == "logs" && key != "esc" {
					handled, cmd = m.logsKey(v)
				}
				if !handled {
					handled = m.profileGridKey(v)
				}
				if !handled {
					handled = m.bodyListKey(v)
				}
				if handled {
					break
				}
				switch key {
				case "esc":
					m.returnToNavigation()
				case "up", "k":
					m.scroll = max(0, m.scroll-1)
				case "down", "j":
					m.scroll++
				case "home":
					m.scroll = 0
				case "end":
					m.scroll = m.total
				case "pgup":
					m.scroll = max(0, m.scroll-max(1, dashboardGeometry(m.dashboardOptions()).height-1))
				case "pgdown":
					m.scroll += max(1, dashboardGeometry(m.dashboardOptions()).height-1)
				}
			case "tab":
				m.cycleFocus(1)
			case "shift+tab":
				m.cycleFocus(-1)
			}
			break
		}
		if m.page == "proxies" {
			if handled, next := m.proxyKey(v); handled {
				cmd = next
				break
			}
		}
		if m.contentNavigationKey(v) {
			break
		}
		if m.page == "connections" {
			if handled, next := m.connectionKey(v); handled {
				cmd = next
				break
			}
		}
		if m.page == "rules" {
			if handled, next := m.ruleKey(v); handled {
				cmd = next
				break
			}
		}
		if m.page == "logs" {
			if handled, next := m.logsKey(v); handled {
				cmd = next
				break
			}
		}
		if m.profileGridKey(v) {
			break
		}
		if m.bodyListKey(v) {
			break
		}
		switch key {
		case "c":
			m.navigate("connections")
		case "i":
			if m.page != "proxies" {
				cmd = m.openWizard(false)
			}
		case "b":
			cmd = m.openWizard(true)
		case "n":
			m.openNetwork(false)
		case "a":
			if m.page == "profiles" {
				cmd = m.activate("profiles.url")
			} else {
				cmd = m.openForm()
			}
		case "s", "x", "r":
			if key != "r" || m.page != "proxies" && m.page != "overview" {
				action := map[string]string{"s": "start", "x": "stop", "r": "restart"}[key]
				cmd = m.serviceAction(action)
			}
		case "tab":
			m.cycleFocus(1)
		case "shift+tab":
			m.cycleFocus(-1)
		case "enter", "space", " ":
			cmd = m.activateControl(m.focus, &v)
		case "down", "j":
			m.scroll++
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		case "pgdown":
			g := dashboardGeometry(m.dashboardOptions())
			m.scroll += max(1, g.height-1)
		case "pgup":
			g := dashboardGeometry(m.dashboardOptions())
			m.scroll = max(0, m.scroll-max(1, g.height-1))
		case "home":
			m.scroll = 0
		case "end":
			m.scroll = m.total
		case "esc":
			m.returnToNavigation()
		case "g", "ctrl+r":
			if m.page != "proxies" && m.page != "connections" && m.page != "logs" {
				cmd = m.refresh()
			}
		}
	default:
		// Forward cursor/clipboard messages to the focused Bubbles input. The
		// child model ignores messages it does not own, including key releases.
		if m.coreNumber.id != "" {
			cmd = m.updateCoreNumber(msg)
		} else if m.providers.open {
			if m.providers.editing {
				m.providers.search, cmd = m.providers.search.Update(msg)
			}
		} else if m.ports.open {
			cmd = m.updatePortsInput(msg)
		} else if m.wizard.open || m.wizard.networkOpen {
			cmd = m.updateWizardInput(msg)
		} else if m.inlineInputActive() {
			cmd = m.updateInlineInput(msg)
		} else if m.form == "website-editor" {
			cmd = m.updateWebsiteEditorInput(msg)
		} else if m.form == "tun-confirm" || m.form == "profile-delete" || m.form == "profile-document" || m.form == "profile-overrides" && m.busy {
			break
		} else if m.form != "" {
			if m.profileFieldsForm() {
				if input := m.profileCreateActive(); input != nil && !m.busy && !m.quitting {
					*input, cmd = input.Update(msg)
				}
			} else {
				m.input, cmd = m.input.Update(msg)
			}
		} else if m.page == "profiles" && m.focus == "profiles.url" {
			m.profilesUI.url, cmd = m.profilesUI.url.Update(msg)

		} else if m.proxies.editing {
			m.proxies.search, cmd = m.proxies.search.Update(msg)
		} else if m.connections.editing {
			m.connections.search, cmd = m.connections.search.Update(msg)
		} else if m.rules.editing {
			m.rules.search, cmd = m.rules.search.Update(msg)
		} else if m.logs.editing {
			m.logs.search, cmd = m.logs.search.Update(msg)
		}
	}
	// Publish the preceding result before first-run setup can start its own job.
	m.syncNotification()
	inline := m.syncInlineInstall()
	if inline != nil {
		m.syncNotification()
	}
	m.reconcileProfiles()
	previousFocus := m.focus
	nextProxies := m.syncProxies()
	if result, ok := msg.(resultMsg); ok && result.actionSucceeded && result.hasSnapshot && m.page == "proxies" &&
		previousFocus == m.notification.operation.Key && previousFocus == "mode."+m.snapshot.Mode.Actual {
		// An explicitly activated mode keeps Enter while its list restores its scroll.
		m.focus = previousFocus
	}
	m.reconcileMenu()
	var completion tea.Cmd
	if m.pathForm() {
		completion = m.paths.request(m.pathInputValue())
	} else {
		m.paths.reset()
	}
	next := m.syncRuntime()
	if lookup := m.syncIPInfo(false); lookup != nil {
		cmd = tea.Batch(cmd, lookup)
	}
	nextRules := m.syncRules()
	nextRuleProviders := m.syncProviders()
	if expiry := m.syncProfileExpiry(msg, previousPage); expiry != nil {
		cmd = tea.Batch(cmd, expiry)
	}
	if m.page != previousPage {
		m.stopLogoIntro()
		m.tooltip.dismiss()
	}
	if m.websiteDrag.active() && (!m.pageSelectionMenuAvailable("overview") || m.menu.open) {
		m.cancelOverviewWebsiteDrag()
	}
	m.layout()
	intro := m.syncLogoIntro()
	timers := m.syncNotificationTimers(time.Now())
	if inline != nil || timers != nil || completion != nil || next != nil || nextRules != nil || nextRuleProviders != nil || intro != nil || nextProxies != nil {
		return m, tea.Batch(cmd, inline, timers, completion, next, nextRules, nextRuleProviders, intro, nextProxies)
	}
	return m, cmd
}
func (m *Model) navigate(page string) {
	m.browsePage(page)
	m.enterContent()
}
func (m *Model) openManagementWizard(local bool) tea.Cmd {
	m.wizard.inline = false
	m.wizard.open = true
	m.wizard.networkOpen = false
	m.wizard.step = stepCore
	m.wizard.err = ""
	m.wizard.failedKind = ""
	m.wizard.failedStage = ""
	m.wizard.localMode = local
	m.wizard.specific = false
	m.wizard.reinstall = false
	m.wizard.installRequested = false
	m.wizard.release = mihomo.Release{}
	m.wizard.plan = app.InstallPlan{}
	m.wizard.directUpdate = false
	m.wizard.updateMode, m.wizard.upgrading = false, false
	m.wizard.updateCheck = app.CoreUpdateCheck{}
	m.wizard.refreshError = ""
	m.wizard.cancelling = false
	m.wizard.scroll = 0
	m.wizard.focus = "check-update"
	if !m.snapshot.Installed || local {
		m.wizard.step = stepVersion
		m.wizard.focus = "install"
	}
	if local {
		m.wizard.focus = "local"
		m.wizard.local.Focus()
	}
	m.failure = ""
	m.wizard.reveal = true
	if local {
		return textinput.Blink
	}
	if !m.snapshot.Installed {
		return m.checkInstallVersion()
	}
	return nil
}
func (m *Model) openForm() tea.Cmd {
	m.form = "source"
	m.name = ""
	m.input.Reset()
	m.paths.reset()
	m.input.EchoMode = textinput.EchoNormal
	m.input.Placeholder = m.tr("model.source_placeholder")
	m.input.Focus()
	return textinput.Blink
}
func (m *Model) formKey(k tea.KeyPressMsg) tea.Cmd {
	if m.form == "website-editor" {
		return m.websiteEditorKey(k)
	}
	if m.form == "profile-document" {
		return m.profileEditorKey(k)
	}
	if m.form == "profile-overrides" && (m.busy || m.quitting) {
		if m.busy && k.String() == "esc" {
			m.cancelTask(false)
		}
		return nil
	}
	if m.form == "editor-preference" {
		if m.busy || m.quitting {
			return nil
		}
		switch k.String() {
		case "enter":
			return m.saveEditorPreference()
		case "esc":
			m.closeEditorPreference()
			return nil
		}
	}
	if m.form == "tun-confirm" {
		return m.tunConfirmationKey(k)
	}
	if handled, cmd := m.profileCreateKey(k); handled {
		return cmd
	}
	if handled, cmd := m.pathFormKey(k); handled {
		return cmd
	}
	if handled, cmd := m.profileFormKey(k); handled {
		return cmd
	}
	key := k.String()
	if key == "esc" {
		m.form = ""
		m.input.Reset()
		m.input.Blur()
		m.message, m.failure = "", ""
		return nil
	}
	if key == "enter" {
		value := strings.TrimSpace(m.input.Value())
		if value == "" {
			m.failure = m.tr("model.value_required")
			return nil
		}
		source, err := resolveLocalPath(value)
		if err != nil {
			m.failure = m.localizeError(err)
			return nil
		}
		if err := validateProfileSource(source); err != nil {
			m.failure = m.localizeError(err)
			return nil
		}
		m.form = ""
		m.input.Reset()
		m.input.Blur()
		return m.beginProfileImport(app.ProfileDraft{Address: source}, "profile.add", m.tr("model.fetch_config"))
	}
	var cmd tea.Cmd
	before := m.input.Value()
	m.input, cmd = m.input.Update(k)
	if before != m.input.Value() {
		m.failure = ""
	}
	return cmd
}
func (m *Model) serviceAction(action string) tea.Cmd {
	if m.busy || m.quitting {
		return nil
	}
	m.prepareNotification("service."+action, m.tr("notification.service."+action))
	if action != "start" && action != "stop" && action != "restart" {
		m.failure = m.tr("model.unsupported_action")
		return nil
	}
	issue := runtimeControlIssue(m.snapshot, m.locale)
	if action == "stop" {
		issue = runtimeStopIssue(m.snapshot, m.locale)
	}
	if issue != "" {
		m.failure = issue
		return nil
	}
	return m.begin(m.tr("model.service_"+action), m.tr("model.service_"+action+"_done"), func(ctx context.Context) error { return m.backend.Control(ctx, action) }, notificationOperation{Key: "service." + action, Context: m.tr("notification.service." + action)})
}
func (m *Model) activate(id string) tea.Cmd {
	return m.activateControl(id, nil)
}

func (m *Model) activateControl(id string, key *tea.KeyPressMsg) tea.Cmd {
	if handled, cmd := m.activateSwitch(id, key); handled {
		return cmd
	}
	if handled, cmd := m.clearInputField(id); handled {
		return cmd
	}
	if m.page == "proxies" && (strings.HasPrefix(id, "proxies.") || strings.HasPrefix(id, "group.") || strings.HasPrefix(id, "node.")) {
		return m.proxyActivate(id)
	}
	if strings.HasPrefix(id, "setup.") {
		return m.inlineInstallActivate(id)
	}
	if id == "content" {
		return nil
	}
	if strings.HasPrefix(id, "overview.card.") {
		m.enterOverviewCard()
		return nil
	}
	if m.notificationAction(id) {
		return nil
	}
	if handled, cmd := m.profileAction(id); handled {
		return cmd
	}
	switch id {
	case "website.add":
		if m.pageSelectionMenuAvailable("overview") {
			return m.beginWebsiteEdit(app.Website{})
		}
		return nil
	case "website.prev", "website.next":
		m.turnWebsitePage(id)
		return nil
	}
	if id == "website.all" || strings.HasPrefix(id, "website.site.") {
		return m.testWebsites(id)
	}
	switch id {
	case "ip.refresh":
		return m.syncIPInfo(true)
	case "runtime-settings":
		return m.openNetworkConfig("proxy")
	case "core.manage":
		if m.page == "overview" && m.snapshotReady && m.snapshot.Installed && !m.busy && !m.quitting {
			m.openCoreSettings()
		}
		return nil
	case "core.checkUpdate":
		if !m.snapshotReady || !m.snapshot.Installed || m.busy || m.quitting {
			return nil
		}
		m.openManagementWizard(false)
		m.wizard.directUpdate = true
		return m.checkCoreUpdate()
	case "settings.bindAddress":
		return m.openNetworkConfig("bind")
	case "settings.accessRules":
		return m.openNetworkConfig("rules")
	case "settings.copyProxyAddress":
		return m.copyProxyAddress()
	case "settings.externalControl":
		return m.openNetworkConfig("control")
	case "settings.proxyAuth":
		return m.openNetworkConfig("auth")
	}
	switch id {
	case "settings.tab.access", "settings.tab.network", "settings.tab.resources":
		m.selectSettingsTab(id)
		return nil
	case "settings.keepAliveIdle", "settings.keepAliveInterval", "settings.geoUpdateInterval":
		return m.openCoreNumber(id)
	case geoMaintenanceID:
		return m.manageGeo()
	case "settings.language", "settings.startPage", "settings.theme", "settings.logLevel", "settings.findProcessMode", "settings.geodataLoader":
		m.openPageSelectionMenu("settings", id, m.navigationOptions().settingsDropdown(id))
		return nil
	case "editor.configure":
		return m.openEditorPreference()
	case "card.profiles":
		m.navigate("profiles")
		return nil
	case "page.refresh":
		if m.page == "profiles" && !m.busy && !m.quitting {
			return m.refresh()
		}
		return nil
	case "snapshot.retry":
		if !m.snapshotReady && !m.busy && !m.quitting {
			return m.refresh()
		}
		return nil
	case "card.install", "core.install":
		return m.openWizard(false)
	case "card.start":
		return m.serviceAction("start")
	case "core.start", "core.stop", "core.restart":
		// Other pages retain their existing recovery/start actions. Overview
		// exposes only its observed-state toggle.
		if m.page != "overview" {
			return m.serviceAction(strings.TrimPrefix(id, "core."))
		}
		return nil
	case "terminal.activation":
		m.openTerminalActivation()
		return nil
	case "card.proxies":
		m.openOverviewProxies()
		return nil
	case overviewRouteGroupTarget:
		d := m.navigationOptions().overviewRuleGroupDropdown()
		if len(d.Items) > 1 {
			m.openPageSelectionMenu("overview", id, d)
		}
		return nil
	case "network.primaryConfig":
		return m.openNetworkConfig("terminal")
	case "network.tunConfig":
		return m.openNetworkConfig("tun")
	case "logo":
		return m.startLogoPulse()
	}
	switch {
	case strings.HasPrefix(id, "logs."):
		return m.logsActivate(id)
	case strings.HasPrefix(id, "rule.") || strings.HasPrefix(id, "rules."):
		return m.ruleActivate(id)
	case strings.HasPrefix(id, "connection.") || strings.HasPrefix(id, "conn."):
		return m.connectionActivate(id)
	case strings.HasPrefix(id, "nav."):
		m.enterContent()
	case id == "menu":
		m.sidebarOpen = !m.sidebarOpen
		if m.sidebarOpen {
			m.sidebar.reveal = true
			m.focus = "nav." + m.page
		} else {
			m.focus = "menu"
		}
	case id == "install":
		return m.openWizard(false)
	case id == "network":
		m.openNetwork(false)
	case id == "import" || id == "add":
		return m.openForm()
	case id == "guide.later":
		m.guideSkipped = true
		m.scroll = 0
		m.returnToNavigation()
	case id == "dismiss":
		return m.begin(m.tr("model.saving_guide"), m.tr("model.guide_dismissed"), func(ctx context.Context) error { return m.backend.SetGuideDismissed(ctx, true) }, notificationOperation{Key: "guide.dismiss", Context: m.tr("dashboard.guide.later")})
	case id == "start" || id == "stop" || id == "restart":
		return m.serviceAction(id)
	case strings.HasPrefix(id, "mode."):
		if m.busy || m.quitting {
			return nil
		}
		mode := strings.TrimPrefix(id, "mode.")
		if !mihomo.ValidMode(mode) {
			m.prepareNotification("mode."+mode, m.tr("dashboard.control.routingMode")+" · "+mode)
			m.failure = m.tr("model.invalid_mode")
			return nil
		}
		if issue := runtimeControlIssue(m.snapshot, m.locale); issue != "" {
			m.prepareNotification("mode."+mode, m.tr("dashboard.control.routingMode")+" · "+mode)
			m.failure = issue
			return nil
		}
		return m.begin(m.tr("model.applying_mode"), m.tr("model.mode_saved"), func(ctx context.Context) error { return m.backend.SetMode(ctx, mode) }, notificationOperation{Key: "mode." + mode, Context: m.tr("dashboard.control.routingMode") + " · " + mode})
	}
	return nil
}
func (m *Model) cycleFocus(delta int) {
	if m.providers.open {
		m.cycleProvidersFocus(delta)
		return
	}
	targets := m.focusTargets()
	if len(targets) == 0 {
		return
	}
	current := -1
	for i, r := range targets {
		if r.ID == m.focus {
			current = i
			break
		}
	}
	current = (current + delta + len(targets)) % len(targets)
	previous := m.focus
	m.focus = targets[current].ID
	if !m.inlineInputActive() {
		m.blurInlineInput()
	}
	if strings.HasPrefix(m.focus, "nav.") {
		m.sidebar.reveal = true
	}
	if strings.HasPrefix(m.focus, "notification.") && !strings.HasPrefix(previous, "notification.") {
		m.notification.restoreFocus = previous
	}
	if m.page == "profiles" {
		m.focusProfileControl(m.focus)
	}
	if m.focus == "profiles.url" {
		m.profilesUI.editing = true
		m.profilesUI.url.Focus()
	} else {
		m.profilesUI.editing = false
		m.profilesUI.url.Blur()
	}
	if strings.HasPrefix(m.focus, "connection.") {
		m.connections.selected = strings.TrimPrefix(m.focus, "connection.")
		m.reconcileConnections()
	}
	if strings.HasPrefix(m.focus, "provider.row.") {
		m.providersActivate(m.focus)
	}
	if m.focus != "menu" && !strings.HasPrefix(m.focus, "nav.") {
		for _, target := range m.contentTargets() {
			if target.ID == m.focus {
				m.revealBodyTarget(target)
				break
			}
		}
	}
	if strings.HasPrefix(m.focus, "rule.") {
		list := m.rulesList()
		if index, found := list.positions[m.focus]; found {
			m.focusRule(list, index)
		}
	}
}

// Proxy cards and tooltips use the current frame's hit regions for hover.
// Repaint only when the pointer crosses into a different target.
func (m *Model) updateDashboardHover(event tea.MouseMotionMsg) (bool, tea.Cmd) {
	if m.page != "proxies" && len(m.tooltip.anchors) == 0 {
		return false, nil
	}
	if m.menu.open || (m.feedbackDialogOpen() && !m.ports.editingProxyPorts()) || m.notification.panel.open {
		return false, nil
	}
	previous := m.hover
	m.mouse(event)
	cmd := m.tooltip.motion(m.hover)
	if m.hover != previous {
		m.renderLayout()
		// This frame can reveal delayed progress and move a timed card to overflow.
		return true, tea.Batch(cmd, m.syncNotificationTimers(time.Now()))
	}
	return true, cmd
}

func (m *Model) mouse(event tea.MouseMsg) tea.Cmd {
	k := event.Mouse()
	covered := m.tooltip.bounds.W > 0 && regionsOverlap(hitRegion{X: k.X, Y: k.Y, W: 1, H: 1}, m.tooltip.bounds)
	switch event.(type) {
	case tea.MouseClickMsg, tea.MouseWheelMsg:
		m.tooltip.dismiss()
	}
	if _, wheel := event.(tea.MouseWheelMsg); covered && !wheel {
		m.hover = ""
		return nil
	}
	if m.coreNumber.id != "" {
		if click, ok := event.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
			var tab string
			for _, target := range m.targets {
				if click.X < target.X || click.X >= target.X+target.W || click.Y < target.Y || click.Y >= target.Y+target.H {
					continue
				}
				if target.ID == m.coreNumber.id {
					return nil
				}
				if strings.HasPrefix(target.ID, "settings.tab.") {
					tab = target.ID
				}
			}
			if cmd := m.saveCoreNumber(); cmd != nil || m.coreNumber.id != "" {
				if tab != "" && m.coreNumber.id == "" {
					m.hover = tab
					m.settingsPointerFocus = true
					m.selectSettingsTab(tab)
				}
				return cmd
			}
		} else {
			return nil
		}
	}
	if m.providers.open {
		return m.providersMouse(event)
	}
	if m.notification.panel.open {
		m.notificationAction(m.notification.panel.mouse(event))
		return nil
	}
	if m.menu.open {
		if _, motion := event.(tea.MouseMotionMsg); motion && m.page == "settings" && m.menu.dropdown != nil {
			return nil
		}
		return m.applyMenuResult(m.menu.mouse(event))
	}
	if m.ports.open {
		return m.portsMouse(event)
	}
	if m.page == "connections" && (m.connections.detail != nil || m.connections.confirming) {
		return m.connectionDialogMouse(event)
	}
	if _, wheel := event.(tea.MouseWheelMsg); wheel && m.notification.covers(k.X, k.Y) {
		return nil
	}
	if wheel, ok := event.(tea.MouseWheelMsg); ok && m.sidebarWheel(wheel) {
		return nil
	}
	if m.sidebarOpen && !m.wizard.open && !m.wizard.networkOpen && m.form == "" {
		if _, clicked := event.(tea.MouseClickMsg); clicked && k.Button == tea.MouseLeft && k.X >= min(sidebarWidth, m.width) {
			m.sidebarOpen, m.focus = false, "menu"
			return nil
		}
	}
	if _, clicked := event.(tea.MouseClickMsg); clicked && k.Button == tea.MouseRight {
		if m.page == "overview" {
			m.openWebsiteMenuAt(k.X, k.Y)
			return nil
		}
		m.openProfileMenuAt(k.X, k.Y)
		return nil
	}
	if _, wheel := event.(tea.MouseWheelMsg); wheel {
		if m.page == "settings" {
			m.hover = ""
		}
		if k.Button != tea.MouseWheelUp && k.Button != tea.MouseWheelDown {
			return nil
		}
		d := 3
		if k.Button == tea.MouseWheelUp {
			d = -3
		}
		if m.wizard.open || m.wizard.networkOpen {
			m.wizard.scroll = max(0, m.wizard.scroll+d)
		} else if m.page == "logs" && m.form == "" {
			m.logsScroll(d)
		} else if m.profileFieldsForm() {
			m.profileCreate.scrollBy(cmp.Compare(d, 0))
		} else if m.form == "" {
			m.scroll = max(0, m.scroll+d)
		}
		return nil
	}
	m.hover = ""
	_, pressed := event.(tea.MouseClickMsg)
	for _, r := range m.targets {
		if k.X >= r.X && k.X < r.X+r.W && k.Y >= r.Y && k.Y < r.Y+r.H {
			m.hover = r.ID
			if r.tip.text != "" && r.tip.owner != r.ID {
				return nil
			}
			if pressed && k.Button == tea.MouseLeft {
				if handled, cmd := m.clearInputField(r.ID); handled {
					return cmd
				}
				if m.wizard.open || m.wizard.networkOpen {
					m.wizard.focus = r.ID
					return m.wizardActivate(r.ID)
				}
				if m.form != "" {
					if m.form == "website-editor" {
						return m.websiteEditorAction(r.ID)
					}
					if m.form == "tun-confirm" {
						return m.tunConfirmationAction(r.ID)
					}
					if m.profileFieldsForm() {
						return m.profileCreateMouse(r.ID)
					}
					if m.form == "profile-document" {
						return m.profileEditorActivate(r.ID)
					}
					if m.form == "profile-delete" {
						return m.profileDeleteMouse(r.ID)
					}
					m.input.Focus()
					return textinput.Blink
				}
				if strings.HasPrefix(r.ID, "nav.") {
					popup := m.sidebarOpen
					m.browsePage(strings.TrimPrefix(r.ID, "nav."))
					if popup {
						m.settingsPointerFocus = m.page == "settings"
						m.enterContent()
					}
					return nil
				}
				if m.notificationAction(r.ID) {
					return nil
				}
				if r.ID == geoMaintenanceID && m.busy {
					return m.manageGeo()
				}
				if strings.HasPrefix(r.ID, "setup.") {
					m.focus = r.ID
					return m.inlineInstallActivate(r.ID)
				}
				// The hint switch and tab strip stay interactive during background jobs.
				if !m.busy || r.ID == "key-hints.toggle" || strings.HasPrefix(r.ID, "settings.tab.") {
					if r.ID == "logo" {
						return m.startLogoPulse()
					}
					if m.page == "proxies" && (r.ID == "proxies.search" || r.ID == "proxies.search-hint") {
						return m.focusProxySearch()
					}
					m.focus = r.ID
					m.settingsPointerFocus = m.page == "settings"
					if handled, cmd := m.profileCardClick(r.ID); handled {
						return cmd
					}
					if m.page == "rules" && strings.HasPrefix(r.ID, "rule.") {
						m.selectRule(r.ID)
						return nil
					}
					return m.activate(r.ID)
				}
			}
			break
		}
	}
	return nil
}
func (m *Model) profiles() []string {
	var p []string
	seen := map[string]bool{"default": true}
	for _, n := range m.snapshot.Profiles {
		if !seen[n] {
			p = append(p, n)
			seen[n] = true
		}
	}
	return p
}
func (m *Model) pageBody(width int) (string, []hitRegion) {
	if m.page == "overview" && m.inlineInstallVisible() {
		return m.inlineInstallBody(width)
	}
	if m.page == "connections" {
		return m.connectionsBody(width)
	}
	if m.page == "rules" {
		return m.rulesBody(width)
	}
	if m.page == "logs" {
		return m.logsBody(width)
	}
	if m.page == "profiles" {
		return m.profilesBody(width)
	}
	if m.page == "proxies" {
		return m.proxiesBody(width)
	}
	return "", nil
}

func (m *Model) populatePageBody(o *dashboardOptions) {
	g := dashboardGeometry(*o)
	if m.page == "logs" && o.BodyTopRows > 0 {
		o.BodyTop = m.logsToolbar(g.width, o.BodyTopRows)
	}
	if m.page == "rules" {
		if o.BodyTopRows > 0 {
			o.BodyTop = m.rulesToolbar(g.width, o.BodyTopRows)
		}
		body := m.rulesBodyWindow(g.width, m.scroll, g.height)
		o.Body, o.BodyTargets = strings.Join(body.lines, "\n"), body.targets
		o.BodyStart, o.BodyTotal = body.start, body.total
		return
	}
	if m.page == "profiles" {
		layout := m.layoutProfiles(g.width)
		start := max(0, min(m.scroll, layout.total-g.height))
		body := m.paintProfiles(layout, start, min(layout.total, start+g.height))
		o.Body, o.BodyTargets = strings.Join(body.lines, "\n"), body.targets
		o.BodyStart, o.BodyTotal = start, layout.total
		return
	}
	if m.page == "proxies" {
		if o.BodyTopRows > 0 {
			o.BodyTop = m.proxiesFixedToolbar(*o)
		}
		layout := m.layoutProxiesBody(g.width)
		start := min(m.scroll, max(0, layout.total-g.height))
		end := min(layout.total, start+g.height)
		body := m.paintProxiesBody(layout, start, end)
		o.Body, o.BodyTargets = strings.Join(body.lines, "\n"), body.targets
		o.BodyStart, o.BodyTotal = start, layout.total
		o.ProxyStickyGroup, o.ProxyRangeLabel = m.proxyBodyRange(layout, start, end)
		return
	}
	o.Body, o.BodyTargets = m.pageBody(g.width)
}
func (m *Model) layout() {
	m.repairPageFocus()
	m.renderLayout()
}

func (m *Model) renderLayout() {
	if !m.logoVisible() {
		m.stopLogoPulse()
	}
	if !m.logoIntroVisible() {
		m.stopLogoIntro()
	}
	if dashboardGeometry(m.dashboardOptions()).sidebar {
		m.sidebarOpen = false
		if m.focus == "menu" {
			m.focus = "nav." + m.page
		}
	} else if !m.sidebarOpen && strings.HasPrefix(m.focus, "nav.") {
		// Folding navigation leaves content visible; only an explicit menu
		// action opens the compact drawer.
		m.focus = "menu"
	}
	o := m.navigationOptions()
	o.tooltips = &m.tooltip
	o.Sidebar = &m.sidebar
	o.settingsPointerFocus = m.settingsPointerFocus
	// Lay out the expanded controls before deriving their keyboard hints.
	p := &m.notification.panel
	var expanded string
	if p.open {
		expanded, _ = p.render(m.notification.items, m.width, m.height, m.locale, m.buttonPlain())
	}
	o.Help, o.Scroll = m.contextHelp(), m.scroll
	_, _, o.EditingInput = m.activeInputField()
	o.OverlayOwnsKeys = p.open || m.menu.open || m.providers.open || m.ports.open || m.form != "" || m.connections.confirming || m.connections.detail != nil
	g := dashboardGeometry(o)
	o.Header = m.pageHeader()
	resizeSearchInput(&m.connections.search, m.connectionSearchWidth(g.width))
	resizeSearchInput(&m.proxies.search, searchInputWidth(g.width))
	resizeSearchInput(&m.rules.search, m.ruleSearchWidth(g.width))
	resizeSearchInput(&m.logs.search, m.logSearchWidth(g.width))
	m.populatePageBody(&o)
	if m.page == "connections" && o.BodyTopRows > 0 {
		o.BodyTop = m.connectionsToolbar(g.width, o.BodyTopRows)
	}
	hoveredIcon := m.tooltip.pointer.ID
	modalTips := m.ports.editingProxyPorts()
	if modalTips {
		// The active dialog owns the anchors. Paint its backdrop independently.
		o.tooltips = nil
	}
	base, hits, total, notifications := drawDashboard(o)
	m.notification.overlays = notifications
	if o.Dim {
		base = modalBackdrop(base)
	}
	m.total = total
	m.scroll = min(m.scroll, max(0, total-g.height))
	m.screen, m.targets = base, hits
	if !o.Dim && !m.sidebarOpen && !m.menu.open {
		m.decorateContentFocus(g, notifications)
		m.rememberPageView()
	}
	if strings.HasPrefix(m.focus, "notification.") && !m.notification.panel.open && !o.Dim && !m.sidebarOpen {
		available := false
		for _, target := range hits {
			available = available || target.ID == m.focus
		}
		if !available {
			m.restoreNotificationFocus()
			m.layout()
			return
		}
	}
	if m.ports.open {
		if modalTips {
			m.tooltip.anchors, m.tooltip.offscreen = m.tooltip.anchors[:0], hitRegion{}
		}
		m.screen, m.targets = m.renderPorts(base)
		if modalTips {
			m.tooltip.sync(m.targets, m.ports.focus, m.hover)
			if !o.HideKeyHints && o.Width >= 20 && o.Height >= 7 {
				o.tooltips, o.Focus, o.Help = &m.tooltip, m.ports.focus, m.portsContextHelp()
				footer, _ := dashboardKeyHints(o)
				m.screen = overlay(m.screen, modalBackdrop(footer), 2, o.Height-1, o.Width, o.Height)
			}
		}
	} else if m.wizard.open || m.wizard.networkOpen {
		m.screen, m.targets = m.renderWizard(base)
	} else if m.form == "website-editor" {
		m.screen, m.targets = m.renderWebsiteEditor(base)
	} else if m.form == "tun-confirm" {
		m.screen, m.targets = m.renderTUNConfirmation(base)
	} else if m.form != "" {
		m.screen, m.targets = m.renderProfileForm(base)
		if m.form == "profile-delete" && m.menu.open {
			m.renderReplacementMenu()
		}
	} else if p.open {
		m.screen = overlay(m.screen, expanded, p.bounds.X, p.bounds.Y, m.width, m.height)
		m.targets = p.targets
	} else if m.providers.open {
		m.screen, m.targets = m.renderProviders(base)
	} else if m.page == "connections" && m.connections.detail != nil {
		m.screen, m.targets = m.renderConnectionDetail(base)
	} else if m.page == "connections" && m.connections.confirming {
		m.screen, m.targets = m.renderConnectionsCloseAll(base)
	} else if m.menu.open {
		m.anchorPageSelectionMenu(hits)
		m.screen = m.renderMenu(base, dashboardContentBottom(o))
	}
	if hoveredIcon != "" && m.hover == hoveredIcon && m.tooltip.pointer.ID == "" {
		// A layout change moved the icon out from beneath a stationary pointer.
		m.hover = ""
		m.renderLayout()
		return
	}
	if hit := tooltipAnchor(m.tooltip.anchors, m.tooltip.open); hit.ID != "" {
		m.screen, m.tooltip.bounds = m.renderTooltip(hit.tip.text, hit, dashboardContentBottom(o), notifications)
	}
}
func (m Model) View() tea.View {
	profile := m.colorProfile
	if m.noColor {
		profile = colorprofile.ASCII
	}
	// Style the canvas cells only. View's foreground/background properties emit
	// OSC 10/11 and change the terminal's default palette, which we do not own.
	light := m.effectiveTheme() == app.ThemeLight
	view := tea.NewView(paintTerminalTheme(themed(m.screen, text, m.canvasBackground(), false), profile, light))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeAllMotion
	view.Cursor = m.inputCursor()
	if view.Cursor != nil {
		view.Cursor.Color = terminalForeground(view.Cursor.Color, profile, light)
	}
	return view
}
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}
