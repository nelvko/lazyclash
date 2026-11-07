package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type wizardStep string

const (
	stepCore       wizardStep = "Core"
	stepVersion    wizardStep = "Version"
	stepUpdate     wizardStep = "Update"
	stepConfirm    wizardStep = "Confirm"
	stepInstalling wizardStep = "Installing"
	stepResult     wizardStep = "Result"
)

// These stable state values are independent of the labels shown to the user.
const (
	checkNotTested     = "Not tested"
	checkTesting       = "Testing"
	checkNotApplicable = "Not applicable"
	checkPassed        = "Passed"
	checkFailed        = "Failed"
)

type checkState struct{ Status, Detail string }
type wizardState struct {
	updateMode, upgrading           bool
	updateCheck                     app.CoreUpdateCheck
	upgradeResult                   app.CoreUpgradeResult
	refreshError                    string
	open, networkOpen, inline, more bool
	step                            wizardStep
	cfg, draft                      downloadnet.Config
	focus                           string
	controls                        []string
	scroll                          int
	reveal                          bool
	localMode, specific, reinstall  bool
	installRequested                bool
	version, local, prefix          textinput.Model
	release                         mihomo.Release
	plan                            app.InstallPlan
	result                          app.InstallResult
	progress                        mihomo.Progress
	logs                            []string
	details, cancelling             bool
	err, failedKind, failedStage    string
	returnErr, returnFailed         string
	returnFocus                     string
	remember                        bool
	previewURL                      bool
	versionTest, fileTest           checkState
	testRelease                     mihomo.Release
	testPending                     int
	cachedRelease                   mihomo.Release
	cachedSelection                 string
}

func newWizard() wizardState {
	input := func(hint string, hidden bool) textinput.Model {
		v := textinput.New()
		v.SetVirtualCursor(false)
		v.CharLimit = 8192
		styleInput(&v, inputBackground, false)
		v.Placeholder = hint
		if hidden {
			v.EchoMode = textinput.EchoPassword
		}
		return v
	}
	return wizardState{step: stepCore, cfg: downloadnet.Config{Method: "direct"}, draft: downloadnet.Config{Method: "direct"}, focus: "continue", version: input("v1.19.33", false), local: input("/absolute/path/to/mihomo", false), prefix: input("https://your-accelerator.example/", false), versionTest: checkState{Status: checkNotTested}, fileTest: checkState{Status: checkNotTested}}
}
func (m *Model) selection() string {
	if m.wizard.specific {
		return strings.TrimSpace(m.wizard.version.Value())
	}
	return "latest"
}
func (m *Model) resolveVersion() tea.Cmd {
	if m.wizard.localMode {
		return m.preparePlan()
	}
	if m.wizard.specific && strings.TrimSpace(m.wizard.version.Value()) == "" {
		m.wizard.err = m.tr("wizard.version.required")
		return nil
	}
	if !m.wizard.specific && m.wizard.updateCheck.LatestVersion == "" {
		return m.checkInstallVersion()
	}
	cfg, selection := m.wizard.cfg, m.selection()
	if !m.wizard.specific {
		// Pin the shared version check, never look up mutable latest twice.
		selection = m.wizard.updateCheck.LatestVersion
	}
	ctx := m.newJob("resolve")
	id := m.id
	backend := m.backend
	m.wizard.err = ""
	m.wizard.failedKind = ""
	m.wizard.release = mihomo.Release{}
	m.wizard.plan = app.InstallPlan{}
	m.message = m.tr("wizard.version.querying")
	return tea.Batch(func() tea.Msg {
		r, err := backend.ResolveRelease(ctx, selection, cfg)
		return wizardMsg{id: id, kind: "resolve", release: r, err: err}
	}, m.spinner.Tick)
}
func (m *Model) preparePlan() tea.Cmd {
	ctx := m.newJob("prepare")
	id := m.id
	backend := m.backend
	release := m.wizard.release
	local := ""
	if m.wizard.localMode {
		local = strings.TrimSpace(m.wizard.local.Value())
	}
	reinstall := m.wizard.reinstall
	m.wizard.err = ""
	m.message = m.tr("wizard.preflight.checking")
	return tea.Batch(func() tea.Msg {
		p, err := backend.PrepareInstall(ctx, release, local, reinstall)
		return wizardMsg{id: id, kind: "prepare", plan: p, err: err}
	}, m.spinner.Tick)
}
func (m *Model) installCore() tea.Cmd {
	m.wizard.upgrading = false
	return m.runCoreInstall()
}

func (m *Model) runCoreInstall() tea.Cmd {
	if m.busy {
		return nil
	}
	if !m.wizard.upgrading && m.wizard.plan.BlockedReason != "" {
		m.wizard.err = m.locale.Diagnostic(m.wizard.plan.BlockedReason, m.wizard.plan.BlockedDiagnostic)
		return nil
	}
	ctx := m.newJob("install")
	id := m.id
	backend := m.backend
	plan, cfg := m.wizard.plan, m.wizard.cfg
	upgrading, check := m.wizard.upgrading, m.wizard.updateCheck
	m.wizard.refreshError = ""
	m.wizard.step = stepInstalling
	m.wizard.cancelling = false
	m.wizard.err = ""
	m.wizard.failedStage = ""
	m.wizard.progress = mihomo.Progress{Stage: "deploy", Detail: "Rechecking installation requirements"}
	m.wizard.logs = nil
	m.wizard.scroll = 0
	m.wizard.focus = "cancel"
	m.wizard.reveal = true
	updates := make(chan progressMsg, 32)
	var latestMu sync.Mutex
	latest := m.wizard.progress
	report := func(p mihomo.Progress) {
		latestMu.Lock()
		latest = p
		latestMu.Unlock()
		select {
		case updates <- progressMsg{id: id, progress: p}:
		default:
		}
	}
	run := func() tea.Msg {
		defer close(updates)
		var result app.InstallResult
		var upgraded app.CoreUpgradeResult
		var err error
		kind := "install"
		if upgrading {
			kind = "upgrade"
			upgraded, err = backend.UpgradeCore(ctx, check, cfg, report)
		} else {
			result, err = backend.InstallCore(ctx, plan, cfg, report)
		}
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		s, se := backend.Snapshot(refreshCtx)
		if se != nil && !upgrading {
			err = errors.Join(err, diagnostic.New("diagnostic.wizard_refresh", "refresh dashboard: %w", se))
		}
		latestMu.Lock()
		finalProgress := latest
		latestMu.Unlock()
		return wizardMsg{id: id, kind: kind, result: result, upgradeResult: upgraded, snapshot: s, hasSnapshot: se == nil, refreshError: se, progress: finalProgress, err: err}
	}
	return tea.Batch(run, waitProgress(ctx, updates), m.spinner.Tick)
}
func (m *Model) handleWizardResult(v wizardMsg) tea.Cmd {
	w := &m.wizard
	if v.progress.Stage != "" {
		w.progress = v.progress
	}
	if v.err != nil {
		w.installRequested = false
		w.err = clean(m.localizeError(v.err))
		if v.kind == "save-network" {
			w.err = m.tr("wizard.preferences_not_saved", w.err)
			return nil
		}
		w.failedKind = v.kind
		if v.kind == "check-update" {
			w.focus = "check-update"
		} else if v.kind == "install-check" || v.kind == "resolve" || v.kind == "prepare" {
			w.focus = "resolve"
			if v.kind == "prepare" {
				w.step, w.plan, w.focus = stepVersion, app.InstallPlan{}, "install"
			}
			if w.localMode {
				w.focus = "local"
			}
		}
		m.message = m.tr("wizard.operation_failed")
		if v.kind == "install" || v.kind == "upgrade" {
			w.failedStage = w.progress.Stage
			w.step = stepResult
			w.focus = "retry"
			if v.kind == "upgrade" {
				w.focus, w.cancelling = "check-update", false
			}
			if errors.Is(v.err, context.Canceled) {
				key := "wizard.install_cancelled"
				if v.kind == "upgrade" {
					key = "core_update.cancelled"
				}
				w.err = m.tr(key, w.err)
				w.focus = "done"
			}
		}
		w.reveal = true
		return nil
	}
	w.err = ""
	w.failedKind = ""
	switch v.kind {
	case "install-check":
		w.updateCheck = v.updateCheck
		w.focus = "install"
		m.message = m.tr("core_update.checked")
		if w.installRequested {
			return m.resolveVersion()
		}
	case "check-update":
		w.updateCheck = v.updateCheck
		w.focus = "check-update"
		if v.updateCheck.Status == mihomo.UpdateAvailable && v.updateCheck.Channel == mihomo.ChannelStable {
			w.focus = "upgrade"
		}
		m.message = m.tr("core_update.checked")
	case "upgrade":
		w.upgradeResult = v.upgradeResult
		w.step, w.focus, w.cancelling = stepResult, "done", false
		if v.refreshError != nil {
			w.refreshError = m.tr("core_update.refresh_failed")
		}
		m.message = m.tr("core_update.succeeded")
	case "resolve":
		w.release = v.release
		w.cachedRelease, w.cachedSelection = v.release, m.selection()
		m.message = m.tr("wizard.version_resolved", v.release.Version)
		w.focus = "install"
		if w.installRequested {
			return m.preparePlan()
		}
	case "prepare":
		w.plan = v.plan
		w.release = v.plan.Release
		if w.installRequested && v.plan.BlockedReason == "" && v.plan.ExistingVersion == "" {
			w.installRequested = false
			return m.installCore()
		}
		w.installRequested = false
		w.step = stepConfirm
		w.focus = "install"
		if v.plan.BlockedReason != "" {
			w.focus = "back"
		}
		w.scroll = 0
		m.message = m.tr("wizard.preflight.completed")
	case "install":
		w.result = v.result
		w.step = stepResult
		w.focus = "done"
		w.cancelling = false
		m.message = m.tr("wizard.installed")
	case "save-network":
		return m.applyNetwork()
	}
	w.reveal = true
	return nil
}
func (m *Model) openNetwork(fromFailure bool) {
	w := &m.wizard
	w.installRequested = false
	if m.busy && m.job != "install" {
		m.abortLookup()
	}
	if m.job == "install" {
		return
	}
	w.networkOpen = true
	w.draft = w.cfg.Normalized()
	w.prefix.SetValue(w.draft.Prefix)
	w.returnFocus = w.focus
	w.returnErr, w.returnFailed = w.err, w.failedKind
	w.focus = "download-method"
	w.scroll = 0
	w.reveal = true
	w.remember = false
	w.previewURL = false
	w.versionTest = checkState{Status: checkNotTested}
	w.fileTest = checkState{Status: checkNotTested}
	w.testPending = 0
	if !fromFailure && w.failedKind == "" {
		w.err = ""
	}
}
func (m *Model) draftChanged() {
	if m.job == "network" {
		m.abortLookup()
	}
	w := &m.wizard
	w.versionTest = checkState{Status: checkNotTested}
	w.fileTest = checkState{Status: checkNotTested}
	w.testPending = 0
	w.err = ""
}
func (m *Model) testNetwork() tea.Cmd {
	w := &m.wizard
	cfg := w.draft.Normalized()
	if err := cfg.Validate(); err != nil {
		w.err = m.localizeError(err)
		return nil
	}
	ctx := m.newJob("network")
	id := m.id
	backend := m.backend
	selection := m.selection()
	r := w.release
	if r.Asset == "" && w.cachedSelection == selection {
		r = w.cachedRelease
	}
	w.versionTest = checkState{Status: checkTesting}
	w.fileTest = checkState{Status: checkNotApplicable, Detail: m.tr("wizard.network.resolve_file")}
	w.testPending = 1
	w.testRelease = r
	cmds := []tea.Cmd{func() tea.Msg {
		if selection == "latest" {
			check, err := backend.CheckUpdate(ctx, mihomo.ChannelStable, cfg)
			if err != nil {
				return networkMsg{id: id, kind: "version", err: err}
			}
			if check.LatestVersion == "" {
				return networkMsg{id: id, kind: "version", err: diagnostic.New("diagnostic.release.versionChoice", "choose latest or a stable Mihomo version such as v1.19.33")}
			}
			selection = check.LatestVersion
		}
		release, err := backend.ResolveRelease(ctx, selection, cfg)
		return networkMsg{id: id, kind: "version", release: release, err: err}
	}, m.spinner.Tick}
	if r.Asset != "" {
		w.fileTest = checkState{Status: checkTesting}
		w.testPending++
		cmds = append(cmds, m.fileTestCmd(ctx, id, r, cfg))
	}
	return tea.Batch(cmds...)
}
func (m Model) fileTestCmd(ctx context.Context, id uint64, r mihomo.Release, cfg downloadnet.Config) tea.Cmd {
	return func() tea.Msg {
		err := m.backend.TestReleaseFile(ctx, r, cfg)
		return networkMsg{id: id, kind: "file", release: r, err: err}
	}
}
func (m *Model) handleNetworkResult(v networkMsg) tea.Cmd {
	w := &m.wizard
	w.testPending--
	status := checkState{Status: checkPassed}
	if v.err != nil {
		status = checkState{Status: checkFailed, Detail: clean(m.localizeError(v.err))}
	}
	var cmd tea.Cmd
	if v.kind == "version" {
		w.versionTest = status
		if v.err == nil {
			w.versionTest.Detail = m.tr("wizard.network.resolved", v.release.Version)
			different := w.testRelease != v.release
			w.testRelease = v.release
			if different || w.fileTest.Status == checkNotApplicable {
				w.fileTest = checkState{Status: checkTesting, Detail: v.release.Asset}
				w.testPending++
				cmd = m.fileTestCmd(m.opCtx, m.id, v.release, w.draft)
			}
		}
	} else if v.release == w.testRelease {
		status.Detail = m.tr("wizard.network.asset", v.release.Asset, status.Detail)
		w.fileTest = status
	}
	if w.testPending <= 0 {
		m.busy = false
		m.job = ""
		if m.cancel != nil {
			m.cancel()
		}
		m.message = m.tr("wizard.network.completed")
	}
	return cmd
}
func (m *Model) saveAndApplyNetwork() tea.Cmd {
	cfg := m.wizard.draft.Normalized()
	if err := cfg.Validate(); err != nil {
		m.wizard.err = m.localizeError(err)
		return nil
	}
	if !m.wizard.remember {
		return m.applyNetwork()
	}
	ctx := m.newJob("save-network")
	id := m.id
	backend := m.backend
	return tea.Batch(func() tea.Msg {
		err := backend.SaveNetworkPreferences(ctx, cfg)
		return wizardMsg{id: id, kind: "save-network", err: err}
	}, m.spinner.Tick)
}
func (m *Model) applyNetwork() tea.Cmd {
	w := &m.wizard
	changed := w.cfg != w.draft.Normalized()
	w.installRequested = false
	w.cfg = w.draft.Normalized()
	w.networkOpen = false
	w.focus = w.returnFocus
	w.scroll = 0
	w.reveal = true
	m.message = m.tr("wizard.network.applied")
	retry := w.returnFailed
	w.err = ""
	w.failedKind = ""
	if (w.open || w.inline) && w.updateMode {
		w.updateCheck = app.CoreUpdateCheck{}
		w.step = stepUpdate
		return m.checkCoreUpdate()
	}
	if changed {
		w.updateCheck = app.CoreUpdateCheck{}
		w.release = mihomo.Release{}
		w.plan = app.InstallPlan{}
		if w.step == stepConfirm || w.step == stepResult {
			w.step = stepVersion
		}
	}
	if (w.open || w.inline) && !w.localMode && (retry != "" || w.step == stepVersion) {
		if !w.specific {
			return m.checkInstallVersion()
		}
		return m.resolveVersion()
	}
	return nil
}
func (m *Model) closeNetwork() {
	if m.job == "network" || m.job == "save-network" {
		m.abortLookup()
	}
	w := &m.wizard
	w.networkOpen = false
	w.focus = w.returnFocus
	w.scroll = 0
	w.reveal = true
	w.err, w.failedKind = w.returnErr, w.returnFailed
}
func (m *Model) wizardKey(k tea.KeyPressMsg) tea.Cmd {
	key := k.String()
	w := &m.wizard
	if key == "esc" {
		w.installRequested = false
		if m.job == "install" {
			m.cancelTask(false)
			return nil
		}
		if w.networkOpen {
			m.closeNetwork()
			return nil
		}
		if m.busy {
			m.abortLookup()
		}
		if w.step == stepInstalling {
			return nil
		}
		if w.step == stepCore || w.step == stepResult || w.step == stepVersion && !m.snapshot.Installed {
			w.open = false

			return nil
		}
		if w.step == stepUpdate {
			w.step = stepCore
			w.updateMode = false
		} else if w.step == stepConfirm {
			w.step = stepVersion
		} else {
			w.step = stepCore
		}
		w.focus = "install"
		if w.step == stepCore {
			w.focus = "check-update"
		}
		w.scroll = 0
		w.reveal = true
		return nil
	}
	if key == "tab" || key == "shift+tab" {
		delta := 1
		if key == "shift+tab" {
			delta = -1
		}
		if len(w.controls) > 0 {
			i := -1
			for n, id := range w.controls {
				if id == w.focus {
					i = n
					break
				}
			}
			w.focus = w.controls[(i+delta+len(w.controls))%len(w.controls)]
			w.reveal = true
		}
		return textinput.Blink
	}
	if m.job == "save-network" {
		return nil
	}
	if m.wizardInput() != nil {
		if key == "enter" {
			return m.wizardActivate(w.focus)
		}
		return m.updateWizardInput(k)
	}

	switch key {
	case "enter", "space", " ":
		return m.wizardActivate(w.focus)
	case "left", "right":
		if w.focus == "download-method" || w.focus == "version-mode" {
			delta := 1
			if key == "left" {
				delta = -1
			}
			return m.cycleOption(w.focus, delta)
		}
	case "up":
		if w.focus == "download-method" || w.focus == "version-mode" {
			return m.cycleOption(w.focus, -1)
		}
		w.scroll = max(0, w.scroll-1)
	case "down":
		if w.focus == "download-method" || w.focus == "version-mode" {
			return m.cycleOption(w.focus, 1)
		}
		w.scroll++
	case "pgup":
		w.scroll = max(0, w.scroll-max(1, m.height-10))
	case "pgdown":
		w.scroll += max(1, m.height-10)
	case "home":
		w.scroll = 0
	case "end":
		w.scroll = 10000
	}
	return nil
}

// updateWizardInput handles printable keys, bracketed paste and cursor events
// through the same Bubbles v2 model, preserving draft invalidation semantics.
func (m *Model) updateWizardInput(msg tea.Msg) tea.Cmd {
	if m.job == "save-network" || m.job == "install" {
		return nil
	}
	input := m.wizardInput()
	if input == nil {
		return nil
	}
	previous := input.Value()
	if !input.Focused() {
		input.Focus()
	}
	var cmd tea.Cmd
	*input, cmd = input.Update(msg)
	if previous != input.Value() {
		w := &m.wizard
		if w.networkOpen {
			switch w.focus {
			case "prefix":
				w.draft.Prefix = input.Value()
			}
			m.draftChanged()
		} else {
			w.installRequested = false
			if m.busy {
				m.abortLookup()
			}
			w.release = mihomo.Release{}
			w.plan = app.InstallPlan{}
			w.err = ""
		}
	}
	return cmd
}

func (m *Model) wizardInput() *textinput.Model {
	w := &m.wizard
	if w.networkOpen {
		switch w.focus {
		case "prefix":
			if w.draft.Normalized().Method == "accelerated" {
				return &w.prefix
			}
		}
	} else {
		if w.focus == "version-input" {
			return &w.version
		}
		if w.focus == "local" {
			return &w.local
		}
	}
	return nil
}
func (m *Model) wizardActivate(id string) tea.Cmd {
	if handled, cmd := m.clearInputField(id); handled {
		return cmd
	}
	if m.wizardControlDisabled(id) {
		return nil
	}
	w := &m.wizard
	if m.job == "install" {
		if id == "cancel" {
			m.cancelTask(false)
		} else if id == "details" {
			w.details = !w.details
		}
		return nil
	}
	if w.networkOpen {
		if m.job == "save-network" && id != "back" {
			return nil
		}
		switch id {
		case "download-method":
			return m.cycleOption(id, 1)
		case "method-direct", "method-environment", "method-accelerated":
			w.draft.Method = strings.TrimPrefix(id, "method-")
			m.draftChanged()
			w.reveal = true
		case "url-preview":
			w.previewURL = !w.previewURL
			w.reveal = true
		case "remember":
			w.remember = !w.remember
		case "test":
			if !m.busy {
				return m.testNetwork()
			}
		case "apply-network":
			if m.busy {
				m.abortLookup()
			}
			return m.saveAndApplyNetwork()
		case "back":
			m.closeNetwork()
		case "prefix":
			if input := m.wizardInput(); input != nil {
				input.Focus()
				return textinput.Blink
			}
		}
		return nil
	}
	switch id {
	case "manual-install":
		w.step, w.focus, w.scroll, w.reveal = stepVersion, "install", 0, true
		w.updateMode, w.upgrading = false, false
		w.installRequested = false
		if !w.localMode && !w.specific {
			return m.checkInstallVersion()
		}
	case "check-update":
		if !m.busy {
			return m.checkCoreUpdate()
		}
	case "upgrade":
		if !m.busy && w.updateCheck.Status == mihomo.UpdateAvailable && w.updateCheck.Channel == mihomo.ChannelStable {
			w.upgrading = true
			return m.runCoreInstall()
		}
	case "network":
		m.openNetwork(w.err != "")
	case "cancel":
		w.installRequested = false
		if m.busy {
			m.abortLookup()
		}
		w.open = false

	case "back":
		w.installRequested = false
		if m.busy {
			m.abortLookup()
		}
		if w.step == stepUpdate {
			w.step = stepCore
			w.updateMode = false
		} else if w.step == stepConfirm {
			w.step = stepVersion
		} else if w.step == stepVersion && m.snapshot.Installed {
			w.step = stepCore
		} else {
			w.open = false
		}
		w.scroll = 0
		w.focus = "install"
		if w.step == stepCore {
			w.focus = "check-update"
		}
		w.reveal = true
	case "local-mode":
		if m.busy {
			m.abortLookup()
		}
		w.installRequested = false
		w.localMode = !w.localMode
		w.err, w.failedKind = "", ""
		w.release = mihomo.Release{}
		w.plan = app.InstallPlan{}
		if w.localMode {
			w.focus = "local"
			w.local.Focus()
		} else {
			w.focus = "install"
			if !w.specific {
				return m.checkInstallVersion()
			}
		}
		w.reveal = true
	case "version-mode":
		if m.busy {
			m.abortLookup()
		}
		w.specific = !w.specific
		w.installRequested = false
		w.cachedRelease = mihomo.Release{}
		w.cachedSelection = ""
		w.release = mihomo.Release{}
		w.plan = app.InstallPlan{}
		w.err = ""
		if w.specific {
			w.focus = "version-input"
		} else {
			return m.checkInstallVersion()
		}
		w.reveal = true
	case "version-input", "local":
		return textinput.Blink
	case "resolve", "retry":
		if m.busy {
			return nil
		}
		if w.upgrading || w.updateMode {
			return m.checkCoreUpdate()
		}
		if w.step == stepResult {
			w.step = stepVersion
			w.err = ""
			w.installRequested = true
			return m.resolveVersion()
		}
		if !w.localMode && !w.specific {
			return m.checkInstallVersion()
		}
		return m.resolveVersion()
	case "reinstall":
		if m.busy {
			return nil
		}
		w.reinstall = !w.reinstall
		w.installRequested = false
		if w.step == stepConfirm {
			return m.preparePlan()
		}
		w.plan = app.InstallPlan{}
	case "install":
		if !m.busy {
			if w.step == stepConfirm {
				return m.installCore()
			}
			w.installRequested = true
			return m.resolveVersion()
		}
	case "details":
		w.details = !w.details
	case "done":
		w.open = false
		m.navigate("overview")

	case "import":
		w.open = false
		m.navigate("profiles")
		return m.openForm()
	}
	return nil
}
func stageLabel(stage string) string {
	switch stage {
	case "query":
		return "Resolving version"
	case "download":
		return "Downloading"
	case "extract":
		return "Extracting and checking archive"
	case "verify":
		return "Checking executable"
	case "deploy":
		return "Deploying"
	case "done":
		return "Completed"
	default:
		return clean(stage)
	}
}

func (m *Model) cycleOption(id string, delta int) tea.Cmd {
	if id == "version-mode" {
		return m.wizardActivate(id)
	}
	w := &m.wizard
	if !w.networkOpen || m.job == "save-network" {
		return nil
	}
	if id != "download-method" {
		return nil
	}
	options := []string{"direct", "environment", "accelerated"}
	current := &w.draft.Method
	index := 0
	for i, value := range options {
		if value == *current {
			index = i
			break
		}
	}
	*current = options[(index+delta+len(options))%len(options)]
	m.draftChanged()
	w.reveal = true
	return nil
}
