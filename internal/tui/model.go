// Package tui presents shared application use cases in a terminal.
package tui

import (
	"context"
	"errors"
	"os"
	"strconv"
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
	LoadPreferences(context.Context) (app.Preferences, error)
	SaveNetworkPreferences(context.Context, downloadnet.Config) error
	SetGuideDismissed(context.Context, bool) error
	SaveLanguagePreference(context.Context, string) error
	SaveKeyHintsPreference(context.Context, bool) error
	SetAutostart(context.Context, bool) error
	SetMode(context.Context, string) error
	SetTUN(context.Context, bool) error
	CheckUpdate(context.Context, mihomo.Channel, downloadnet.Config) (app.CoreUpdateCheck, error)
	UpgradeCore(context.Context, app.CoreUpdateCheck, downloadnet.Config, func(mihomo.Progress)) (app.CoreUpgradeResult, error)
	ResolveRelease(context.Context, string, downloadnet.Config) (mihomo.Release, error)
	TestReleaseFile(context.Context, mihomo.Release, downloadnet.Config) error
	PrepareInstall(context.Context, mihomo.Release, string, bool) (app.InstallPlan, error)
	InstallCore(context.Context, app.InstallPlan, downloadnet.Config, func(mihomo.Progress)) (app.InstallResult, error)
	Add(context.Context, string, string) error
	AddSubscription(context.Context, string, string) error
	CreateProfile(context.Context, app.ProfileDraft) error
	RenameProfile(context.Context, string, string) error
	UpdateProfileDescription(context.Context, string, string) error
	UpdateProfileAddress(context.Context, string, string) error
	Refresh(context.Context, string) error
	RefreshAllProfiles(context.Context) error
	DeleteProfile(context.Context, string) error
	SetProfileOverrides(context.Context, string, []byte) error
	Apply(context.Context, string) error
	Control(context.Context, string) error
	Select(context.Context, string, string) error
	Runtime(context.Context) (app.RuntimeSnapshot, error)
	CloseConnection(context.Context, string) error
	Rules(context.Context) (app.RuleSnapshot, error)
	StreamLogs(context.Context, func(mihomo.LogEntry)) error
	GetRuntimeSettings(context.Context) (app.RuntimeSettings, error)
	PrepareRuntimeSettings(context.Context, app.RuntimeSettings) (app.RuntimeSettingsPlan, error)
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
	prefs           *app.Preferences
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

type Model struct {
	locale                i18n.Localizer
	languageOverride      string
	ctx                   context.Context
	backend               Backend
	snapshot              app.Snapshot
	snapshotReady         bool
	busy, quitting        bool
	id                    uint64
	cancel                context.CancelFunc
	opCtx                 context.Context
	job, message, failure string
	notification          notificationState
	messageTransient      bool
	messageInformational  bool
	page, focus, hover    string
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
	rememberSource        bool
	profilesUI            profilesState
	proxies               proxiesState
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
	helpOpen              bool
	helpScroll            int
	statusGeneration      uint64
	statusCancel          context.CancelFunc
	wizard                wizardState
	runtime               runtimeState
	connections           connectionsState
	rules                 rulesState
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
	m := Model{ctx: ctx, backend: backend, busy: true, id: 1, cancel: cancel, opCtx: opCtx, job: "refresh", message: "", page: "overview", focus: "install", input: input, spinner: spin, width: 80, height: 24, wizard: newWizard(), profilesUI: newProfilesState(), proxies: newProxiesState(), profileCreate: newProfileCreateState()}
	if len(languages) > 0 {
		m.languageOverride = languages[0]
	}
	m.locale = i18n.New(i18n.Resolve(m.languageOverride, "", os.Getenv))
	m.profilesUI = newProfilesState(m.locale)
	m.message = m.tr("model.loading")
	m.connections.search = textinput.New()
	m.connections.search.CharLimit = 256
	m.connections.search.Placeholder = m.tr("model.connection_search")
	m.rules.search = textinput.New()
	m.rules.search.CharLimit = 512
	m.rules.search.Placeholder = m.tr("model.rule_search")
	m.logs = newLogsState(m.locale)
	styleInput(&m.connections.search, bg, false)
	styleInput(&m.rules.search, bg, false)
	m.colorProfile = colorprofile.Env(os.Environ())
	m.noColor = os.Getenv("NO_COLOR") != ""
	m.colorProfile = themeProfile(os.Environ(), m.colorProfile)
	m.syncNotification()
	m.layout()
	return m
}
func Run(ctx context.Context, backend Backend, languages ...string) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err := tea.NewProgram(New(runCtx, backend, languages...), tea.WithoutSignalHandler()).Run()
	return err
}
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.snapshotCmd(m.id, m.opCtx, true), m.spinner.Tick, func() tea.Msg { <-m.ctx.Done(); return parentCanceledMsg{} })
}

func (m Model) snapshotCmd(id uint64, ctx context.Context, initial bool) tea.Cmd {
	return func() tea.Msg {
		var p *app.Preferences
		var pe error
		if initial {
			value, err := m.backend.LoadPreferences(ctx)
			pe = err
			if err == nil {
				p = &value
			}
		}
		s, err := m.backend.Snapshot(ctx)
		return resultMsg{id: id, snapshot: s, hasSnapshot: err == nil, prefs: p, err: errors.Join(pe, err), success: "State refreshed", transient: true}
	}
}
func (m *Model) newJob(kind string) context.Context {
	m.stopProxyTests()
	m.stopRuntime()
	m.stopRules()
	m.stopLogs()
	m.stopCoreStatus()
	if m.cancel != nil {
		m.cancel()
	}
	m.id++
	m.opCtx, m.cancel = context.WithCancel(m.ctx)
	m.busy = true
	m.job = kind
	m.prepareNotification(kind, "")
	m.failure = ""
	m.messageTransient = false
	m.messageInformational = false
	return m.opCtx
}
func (m *Model) abortLookup() {
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
	ctx := m.newJob("action")
	m.notification.operation.Context = label
	if len(operations) > 0 {
		op := operations[0]
		m.notification.operation.Key, m.notification.operation.Context = op.Key, op.Context
	}
	id := m.id
	backend := m.backend
	m.message = label
	return tea.Batch(func() tea.Msg {
		err := work(ctx)
		actionSucceeded := err == nil
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		s, se := backend.Snapshot(refreshCtx)
		if se != nil {
			err = errors.Join(err, diagnostic.New("diagnostic.refresh_state", "refresh current state: %w", se))
		}
		return resultMsg{id: id, snapshot: s, hasSnapshot: se == nil, actionSucceeded: actionSucceeded, err: err, success: success}
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
	m.stopProxyTests()
	m.stopRuntime()
	m.stopRules()
	m.stopLogs()
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
	if motion, ok := msg.(tea.MouseMotionMsg); ok && m.updateProxyHover(motion) {
		return m, nil
	}
	var cmd tea.Cmd
	previousPage := m.page
	switch v := msg.(type) {
	case proxyDelayMsg:
		cmd = m.acceptProxyDelay(v)
	case proxyTestStatusExpiredMsg:
		if v.generation == m.proxies.generation && !m.proxies.testing {
			m.proxies.testStatusVisible = false
		}
	case keyHintsSavedMsg:
		cmd = m.acceptKeyHintsSaved(v)
	case notificationExpiredMsg:
		m.expireNotification(v)
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
			m.message, m.failure = m.tr("language.saved"), ""
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
	case logsResultMsg:
		accepted := m.logs.active && v.generation == m.logs.generation
		refreshStatus := accepted && v.done && v.err != nil && !errors.Is(v.err, context.Canceled)
		cmd = m.acceptLogs(v)
		if accepted && !v.done && len(v.entries) > 0 {
			cmd = tea.Batch(cmd, m.observeCoreState("ready"))
		}
		if refreshStatus {
			cmd = tea.Batch(cmd, m.refreshCoreStatus())
		}
	case coreStatusMsg:
		if v.generation == m.statusGeneration {
			if m.statusCancel != nil {
				m.statusCancel()
				m.statusCancel = nil
			}
			if v.err == nil {
				m.snapshot = v.snapshot
				m.snapshotReady = true
			} else if !errors.Is(v.err, context.Canceled) {
				m.snapshot.ServiceError = "Service status unavailable; refresh or check doctor"
			}
		}
	case tea.ColorProfileMsg:
		m.colorProfile = v.Profile
		m.logoIntro.colored = true
	case rulesResultMsg:
		accepted := m.rules.active && v.generation == m.rules.generation
		cmd = m.acceptRules(v)
		if accepted && v.err == nil {
			cmd = tea.Batch(cmd, m.observeCoreState(v.snapshot.State))
		}
	case rulesTickMsg:
		if m.rules.active && v.generation == m.rules.generation && m.rulesVisible() {
			cmd = m.rulesCmd()
		}
	case runtimeResultMsg:
		cmd = m.acceptRuntime(v)
	case runtimeTickMsg:
		if m.runtime.active && v.generation == m.runtime.generation && m.runtimeVisible() {
			cmd = m.runtimeCmd()
		}
	case parentCanceledMsg:
		m.stopProxyTests()
		m.stopRuntime()
		m.stopRules()
		m.stopLogs()
		m.stopCoreStatus()
		if m.busy {
			m.cancelTask(true)
		} else {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		if m.logoIntro.sized && (m.width != max(1, v.Width) || m.height != max(1, v.Height)) {
			m.stopLogoIntro()
		}
		m.logoIntro.sized = true
		m.sidebar.reveal = true
		m.width, m.height = max(1, v.Width), max(1, v.Height)
		m.wizard.reveal = true
		m.ports.reveal = true
		if m.page == "connections" && m.connections.detail == nil && strings.HasPrefix(m.focus, "connection.") {
			m.revealConnection(0)
		}
		if m.page == "rules" && m.rules.detail == nil && strings.HasPrefix(m.focus, "rule.") {
			m.revealRule()
		}
		if m.page == "profiles" && !m.sidebarOpen {
			m.focusProfileControl(m.focus)
		}
		m.revealContentFocus()
	case resultMsg:
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
		m.proxies.pendingGroup, m.proxies.pendingNode = "", ""
		if v.hasSnapshot {
			m.snapshot = v.snapshot
			m.snapshotReady = true
		}
		if m.profilesUI.importing {
			if v.err == nil {
				m.profilesUI.url.Reset()
			}
			m.profilesUI.importing = false
		}
		profileCreated := m.form == "profile-create" && m.profileCreate.submitting && v.actionSucceeded
		if m.form == "profile-create" && m.profileCreate.submitting {
			m.profileCreate.submitting = false
			if v.actionSucceeded {
				m.closeProfileCreate()
			}
		}
		if v.prefs != nil {
			m.loadKeyHintsPreference(v.prefs.HideKeyHints)
			m.locale = i18n.New(i18n.Resolve(m.languageOverride, v.prefs.Language, os.Getenv))
			m.refreshLocalizedInputs()
			m.wizard.cfg = v.prefs.Network.Normalized()
			m.wizard.draft = m.wizard.cfg
			m.snapshot.GuideDismissed = v.prefs.GuideDismissed || m.snapshot.GuideDismissed
		}
		m.messageInformational = false
		if v.err != nil {
			m.notification.details = m.locale.Error(v.err)
			m.notification.critical = notificationCritical(v.err)
			if !v.hasSnapshot && m.notification.operation.Key != "refresh" && m.snapshotReady {
				m.markOperationStateUnknown()
			}
			m.failure = m.localizeError(v.err)
			m.message = m.tr("model.failed")
			if profileCreated {
				m.message = v.success
			}
			if notificationCancellation(v.err) {
				m.message = m.tr("model.canceled")
				m.messageInformational = true
				m.failure = ""
			}
		} else {
			m.message = v.success
			m.messageTransient = v.transient
			m.failure = ""
		}
		if m.quitting && !m.keyHints.pending {
			return m, tea.Quit
		}
		if m.wizard.inline && m.wizard.step == stepResult && m.notification.operation.Key == "service.start" && v.actionSucceeded {
			m.wizard.inline = false
			if m.page == "overview" && strings.HasPrefix(m.focus, "setup.") {
				m.focus = "core.stop"
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
			m.snapshot = v.snapshot
			m.snapshotReady = true
		}
		cmd = m.handleInlineWizardResult(v)
		if v.kind == "install" || v.kind == "upgrade" {
			m.notification.operation.Context = m.tr("notification.install")
			if v.kind == "upgrade" {
				m.notification.operation.Context = m.tr("core_update.title")
			}
			m.notification.critical = notificationCritical(v.err)
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
		if m.busy || m.proxies.testing {
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
	case tea.MouseMsg:
		cmd = m.mouse(v)
	case tea.PasteMsg:
		// Bracketed paste is text input, never a global command. In particular a
		// pasted q or digit cannot quit or navigate out of a form.
		if m.notification.panel.open {
			break
		} else if m.menu.open {
			break
		} else if m.ports.open {
			cmd = m.updatePortsInput(v)
		} else if m.helpOpen {
			break
		} else if m.wizard.open || m.wizard.networkOpen {
			cmd = m.updateWizardInput(v)
		} else if m.inlineInputActive() {
			cmd = m.updateInlineInput(v)
		} else if m.form == "tun-confirm" {
			break
		} else if m.form != "" {
			if m.form == "profile-create" {
				input := m.profileCreateActive()
				if m.busy || m.quitting || input == nil {
					break
				}
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
			m.profilesUI.editing = true
			m.profilesUI.url.Focus()
			m.profilesUI.url, cmd = m.profilesUI.url.Update(v)
		} else if m.proxies.editing {
			m.proxies.search, cmd = m.proxies.search.Update(v)
			m.scroll = 0
		} else if m.connections.editing {
			m.connections.search, cmd = m.connections.search.Update(v)
			m.reconcileConnections()
		} else if m.rules.editing {
			m.rules.search, cmd = m.rules.search.Update(v)
			m.reconcileRules()
		} else if m.logs.editing {
			m.pauseLogs()
			m.logs.search, cmd = m.logs.search.Update(v)
			m.scroll = 0
		}
	case tea.KeyPressMsg:
		key := v.String()
		if key == "ctrl+c" {
			cmd = m.requestQuit()
			if cmd != nil {
				return m, cmd
			}
			break
		}
		if handled, next := m.keyHintsShortcut(v); handled {
			cmd = next
			break
		}
		if m.menu.open {
			cmd = m.profileMenuResult(m.menu.key(v))
			break
		}
		if m.ports.open {
			cmd = m.portsKey(v)
			break
		}
		if m.helpOpen {
			cmd = m.helpKey(v)
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
		if m.page == "connections" && m.connections.confirming {
			_, cmd = m.connectionKey(v)
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
				m.dismissNotification()
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
			break
		}
		if m.sidebarOpen {
			// The compact navigation popup owns input until a page is opened or
			// it is dismissed. Page searches must not activate behind the menu.
			allowed := false
			switch key {
			case "1", "2", "3", "4", "5", "6", "7", "enter", "space", " ", "esc", "tab", "shift+tab", "?":
				allowed = true
			}
			if !allowed {
				break
			}
		}
		if key >= "1" && key <= "7" && len(key) == 1 {
			m.navigate(navPages()[int(key[0]-'1')])
			break
		}
		if key == "?" {
			_, cmd = m.keyHintsAction("help.open")
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
			if (key == "enter" || key == "space" || key == " ") && m.focus == "key-hints.toggle" {
				_, cmd = m.keyHintsAction(m.focus)
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
			cmd = m.openWizard(false)
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
			action := map[string]string{"s": "start", "x": "stop", "r": "restart"}[key]
			cmd = m.serviceAction(action)
		case "tab":
			m.cycleFocus(1)
		case "shift+tab":
			m.cycleFocus(-1)
		case "enter", "space", " ":
			cmd = m.activate(m.focus)
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
			cmd = m.refresh()
		}
	default:
		// Forward cursor/clipboard messages to the focused Bubbles input. The
		// child model ignores messages it does not own, including key releases.
		if m.ports.open {
			cmd = m.updatePortsInput(msg)
		} else if m.helpOpen {
			break
		} else if m.wizard.open || m.wizard.networkOpen {
			cmd = m.updateWizardInput(msg)
		} else if m.inlineInputActive() {
			cmd = m.updateInlineInput(msg)
		} else if m.form == "tun-confirm" {
			break
		} else if m.form != "" {
			if m.form == "profile-create" {
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
	inline := m.syncInlineInstall()
	notification := m.syncNotification()
	m.reconcileProfiles()
	m.syncProxies()
	m.reconcileProfileMenu()
	var completion tea.Cmd
	if m.pathForm() {
		completion = m.paths.request(m.pathInputValue())
	} else {
		m.paths.reset()
	}
	next := m.syncRuntime()
	nextRules := m.syncRules()
	nextLogs := m.syncLogs()
	if m.page != previousPage {
		m.stopLogoIntro()
	}
	m.layout()
	intro := m.syncLogoIntro()
	timers := m.syncNotificationTimers(time.Now())
	if inline != nil || notification != nil || timers != nil || completion != nil || next != nil || nextRules != nil || nextLogs != nil || intro != nil {
		return m, tea.Batch(cmd, inline, notification, timers, completion, next, nextRules, nextLogs, intro)
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
	m.rememberSource = false
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
		m.rememberSource = false
		m.form = ""
		m.input.Reset()
		m.input.Blur()
		m.prepareNotification("input.cancel", m.tr("model.input_canceled"))
		m.message = m.tr("model.input_canceled")
		m.messageTransient = false
		m.messageInformational = true
		m.failure = ""
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
		remember := m.rememberSource
		m.rememberSource = false
		m.form = ""
		m.input.Reset()
		m.input.Blur()
		return m.begin(m.tr("model.fetch_config"), m.tr("model.config_saved"), func(ctx context.Context) error {
			if remember {
				return m.backend.AddSubscription(ctx, "", source)
			}
			return m.backend.Add(ctx, "", source)
		}, notificationOperation{Key: notificationKey("profile.add", source), Context: m.tr("model.fetch_config")})
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
	if m.page == "proxies" && id != "proxies.search" {
		m.proxies.editing = false
		m.proxies.search.Blur()
	}
	if m.page == "proxies" && (strings.HasPrefix(id, "proxies.") || strings.HasPrefix(id, "group.") || strings.HasPrefix(id, "node.")) {
		return m.proxyActivate(id)
	}
	if strings.HasPrefix(id, "setup.") {
		return m.inlineInstallActivate(id)
	}
	if handled, cmd := m.keyHintsAction(id); handled {
		return cmd
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
	if strings.HasPrefix(id, "language.") {
		return m.changeLanguage(strings.TrimPrefix(id, "language."))
	}
	if handled, cmd := m.profileAction(id); handled {
		return cmd
	}
	if id == "runtime-settings" {
		return m.openPorts()
	}
	switch id {
	case "card.profiles":
		m.navigate("profiles")
		return nil
	case "page.refresh":
		if !m.busy && !m.quitting {
			return m.refresh()
		}
		return nil
	case "card.install", "core.install":
		return m.openWizard(false)
	case "card.start":
		return m.serviceAction("start")
	case "core.start", "core.stop", "core.restart":
		return m.serviceAction(strings.TrimPrefix(id, "core."))
	case "tun.enable", "tun.disable":
		return m.tunAction(id == "tun.enable")
	case "card.proxies":
		m.navigate("proxies")
		return nil
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
		return m.begin(m.tr("model.saving_guide"), m.tr("model.guide_dismissed"), func(ctx context.Context) error { return m.backend.SetGuideDismissed(ctx, true) }, notificationOperation{Key: "guide.dismiss", Context: m.tr("model.saving_guide")})
	case id == "start" || id == "stop" || id == "restart":
		return m.serviceAction(id)
	case id == "autostart-enable" || id == "autostart-disable":
		enabled := id == "autostart-enable"
		return m.begin(m.tr("model.updating_autostart"), m.tr("model.autostart_saved"), func(ctx context.Context) error { return m.backend.SetAutostart(ctx, enabled) }, notificationOperation{Key: id, Context: m.tr("model.updating_autostart")})
	case strings.HasPrefix(id, "mode."):
		if m.busy || m.quitting {
			return nil
		}
		mode := strings.TrimPrefix(id, "mode.")
		if !mihomo.ValidMode(mode) {
			m.prepareNotification("mode."+mode, m.tr("model.applying_mode")+" · "+mode)
			m.failure = m.tr("model.invalid_mode")
			return nil
		}
		if issue := runtimeControlIssue(m.snapshot, m.locale); issue != "" {
			m.prepareNotification("mode."+mode, m.tr("model.applying_mode")+" · "+mode)
			m.failure = issue
			return nil
		}
		return m.begin(m.tr("model.applying_mode"), m.tr("model.mode_saved"), func(ctx context.Context) error { return m.backend.SetMode(ctx, mode) }, notificationOperation{Key: "mode." + mode, Context: m.tr("model.applying_mode") + " · " + mode})
	}
	return nil
}
func (m *Model) cycleFocus(delta int) {
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
		for _, r := range m.filteredRules() {
			if m.focus == "rule."+strconv.Itoa(r.Index) {
				m.rules.selected = ruleIdentity(r)
				break
			}
		}
	}
}
func (m *Model) mouse(event tea.MouseMsg) tea.Cmd {
	k := event.Mouse()
	if m.notification.panel.open {
		m.notificationAction(m.notification.panel.mouse(event))
		return nil
	}
	if m.menu.open {
		return m.profileMenuResult(m.menu.mouse(event))
	}
	if m.ports.open {
		return m.portsMouse(event)
	}
	if m.helpOpen {
		return m.helpMouse(event)
	}
	if _, wheel := event.(tea.MouseWheelMsg); wheel {
		o := m.dashboardOptions()
		o.Body, o.BodyTargets = m.pageBody(dashboardGeometry(o).width)
		o.Scroll = m.scroll
		for _, toast := range notificationLayout(o) {
			if k.X >= toast.Bounds.X && k.X < toast.Bounds.X+toast.Bounds.W && k.Y >= toast.Bounds.Y && k.Y < toast.Bounds.Y+toast.Bounds.H {
				return nil
			}
		}
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
		m.openProfileMenuAt(k.X, k.Y)
		return nil
	}
	if _, wheel := event.(tea.MouseWheelMsg); wheel {
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
			if pressed && k.Button == tea.MouseLeft {
				if m.page == "connections" && m.connections.confirming && r.ID != "conn.cancel" && r.ID != "conn.confirm" {
					return nil
				}
				if m.wizard.open || m.wizard.networkOpen {
					m.wizard.focus = r.ID
					return m.wizardActivate(r.ID)
				}
				if m.form != "" {
					if m.form == "tun-confirm" {
						return m.tunConfirmationAction(r.ID)
					}
					if m.form == "profile-create" {
						return m.profileCreateMouse(r.ID)
					}
					if m.form == "profile-delete" {
						return m.profileDeleteMouse(r.ID)
					}
					m.input.Focus()
					return textinput.Blink
				}
				if handled, cmd := m.keyHintsAction(r.ID); handled {
					return cmd
				}
				if strings.HasPrefix(r.ID, "nav.") {
					popup := m.sidebarOpen
					m.browsePage(strings.TrimPrefix(r.ID, "nav."))
					if popup {
						m.enterContent()
					}
					return nil
				}
				if m.notificationAction(r.ID) {
					return nil
				}
				if strings.HasPrefix(r.ID, "setup.") {
					m.focus = r.ID
					return m.inlineInstallActivate(r.ID)
				}
				if !m.busy {
					if r.ID == "logo" {
						return m.startLogoPulse()
					}
					m.focus = r.ID
					if handled, cmd := m.profileCardClick(r.ID); handled {
						return cmd
					}
					return m.activate(r.ID)
				}
			}
			break
		}
	}
	return nil
}
func (m Model) profiles() []string {
	p := []string{"default"}
	seen := map[string]bool{"default": true}
	for _, n := range m.snapshot.Profiles {
		if !seen[n] {
			p = append(p, n)
			seen[n] = true
		}
	}
	return p
}
func (m Model) pageBody(width int) (string, []hitRegion) {
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
	if m.width >= 100 && m.height >= 20 {
		m.sidebarOpen = false
	} else if strings.HasPrefix(m.focus, "nav.") && !m.ports.open && !m.wizard.open && !m.wizard.networkOpen && m.form == "" && !m.helpOpen && m.width >= 20 && m.height >= 12 {
		// Preserve a focused navigation item when its persistent sidebar folds.
		m.sidebarOpen = true
	}
	o := m.dashboardOptions()
	o.Sidebar = &m.sidebar
	o.SelectedGroup, o.Snapshot, o.Focus, o.Hover = m.proxies.expanded, m.snapshot, m.focus, m.hover
	o.Help, o.GuideDismissed, o.Scroll, o.SidebarOpen = m.contextHelp(), m.snapshot.GuideDismissed, m.scroll, m.sidebarOpen
	o.Runtime, o.RuntimeReceived, o.RuntimePending = m.runtime.snapshot, m.runtime.received, m.runtime.pending
	o.StateUnavailable = !m.snapshotReady
	g := dashboardGeometry(o)
	o.Header = m.pageHeader()
	m.proxies.search.SetWidth(m.proxySearchWidth(g.width))
	m.proxies.search.SetCursor(m.proxies.search.Position())
	m.connections.search.SetWidth(max(1, g.width-2))
	m.rules.search.SetWidth(max(1, g.width-2))
	o.Body, o.BodyTargets = m.pageBody(g.width)
	base, hits, total := renderDashboard(o)
	if o.Dim {
		base = themed(base, backdropText, sidebarBackground, true)
	}
	m.total = total
	m.scroll = min(m.scroll, max(0, total-g.height))
	m.screen, m.targets = base, hits
	if !o.Dim && !m.sidebarOpen && !m.menu.open {
		m.decorateContentFocus(g, notificationLayout(o))
	}
	if !o.Dim && !m.sidebarOpen && !m.menu.open {
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
		m.screen, m.targets = m.renderPorts(base)
	} else if m.helpOpen {
		m.screen, m.targets = m.renderHelp(base)
	} else if m.wizard.open || m.wizard.networkOpen {
		m.screen, m.targets = m.renderWizard(base)
	} else if m.form == "tun-confirm" {
		m.screen, m.targets = m.renderTUNConfirmation(base)
	} else if m.form != "" {
		m.screen, m.targets = m.renderProfileForm(base)
	} else if m.notification.panel.open {
		m.screen, m.targets = m.notification.panel.render(m.notification.items, m.width, m.height, m.locale, m.buttonPlain())
	} else if m.menu.open {
		m.screen = m.menu.render(base, m.width, m.height)
	}
}
func (m Model) View() tea.View {
	profile := m.colorProfile
	if m.noColor {
		profile = colorprofile.ASCII
	}
	// Style the canvas cells only. View's foreground/background properties emit
	// OSC 10/11 and change the terminal's default palette, which we do not own.
	view := tea.NewView(terminalTheme(themed(m.screen, text, bg, false), profile))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeAllMotion
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
