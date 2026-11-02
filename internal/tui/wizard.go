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
	open, networkOpen                     bool
	step                                  wizardStep
	cfg, draft                            downloadnet.Config
	focus                                 string
	controls                              []string
	scroll                                int
	reveal                                bool
	localMode, specific, reinstall        bool
	version, local, prefix, mirror, proxy textinput.Model
	release                               mihomo.Release
	plan                                  app.InstallPlan
	result                                app.InstallResult
	progress                              mihomo.Progress
	logs                                  []string
	details, cancelling                   bool
	err, failedKind, failedStage          string
	returnErr, returnFailed               string
	returnFocus                           string
	remember                              bool
	versionTest, fileTest                 checkState
	testRelease                           mihomo.Release
	testPending                           int
	cachedRelease                         mihomo.Release
	cachedSelection                       string
	expanded                              string
}

func newWizard() wizardState {
	input := func(hint string, hidden bool) textinput.Model {
		v := textinput.New()
		v.CharLimit = 8192
		styleInput(&v, inputBackground, false)
		v.Placeholder = hint
		if hidden {
			v.EchoMode = textinput.EchoPassword
		}
		return v
	}
	return wizardState{step: stepCore, cfg: downloadnet.Config{Source: "official", ProxyMode: "none"}, draft: downloadnet.Config{Source: "official", ProxyMode: "none"}, focus: "continue", version: input("v1.19.33", false), local: input("/absolute/path/to/mihomo", false), prefix: input("https://accelerator.example.invalid/", true), mirror: input("https://mirror.example.invalid/mihomo/{version}/{asset}", true), proxy: input("http://127.0.0.1:7890", true), versionTest: checkState{Status: checkNotTested}, fileTest: checkState{Status: checkNotTested}}
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
	cfg, selection := m.wizard.cfg, m.selection()
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
	if m.busy {
		return nil
	}
	if m.wizard.plan.BlockedReason != "" {
		m.wizard.err = m.locale.Diagnostic(m.wizard.plan.BlockedReason, m.wizard.plan.BlockedDiagnostic)
		return nil
	}
	ctx := m.newJob("install")
	id := m.id
	backend := m.backend
	plan, cfg := m.wizard.plan, m.wizard.cfg
	m.wizard.step = stepInstalling
	m.wizard.cancelling = false
	m.wizard.err = ""
	m.wizard.failedStage = ""
	m.wizard.progress = mihomo.Progress{Stage: "deploy", Detail: "Rechecking the reviewed installation plan"}
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
		result, err := backend.InstallCore(ctx, plan, cfg, report)
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		s, se := backend.Snapshot(refreshCtx)
		if se != nil {
			err = errors.Join(err, diagnostic.New("diagnostic.wizard_refresh", "refresh dashboard: %w", se))
		}
		latestMu.Lock()
		finalProgress := latest
		latestMu.Unlock()
		return wizardMsg{id: id, kind: "install", result: result, snapshot: s, hasSnapshot: se == nil, progress: finalProgress, err: err}
	}
	return tea.Batch(run, waitProgress(ctx, updates), m.spinner.Tick)
}
func (m *Model) handleWizardResult(v wizardMsg) tea.Cmd {
	w := &m.wizard
	if v.progress.Stage != "" {
		w.progress = v.progress
	}
	if v.err != nil {
		w.err = clean(m.localizeError(v.err))
		if v.kind == "save-network" {
			w.err = m.tr("wizard.preferences_not_saved", w.err)
			return nil
		}
		w.failedKind = v.kind
		m.message = m.tr("wizard.operation_failed")
		if v.kind == "install" {
			w.failedStage = w.progress.Stage
			w.step = stepResult
			w.focus = "retry"
			if errors.Is(v.err, context.Canceled) {
				w.err = m.tr("wizard.install_cancelled", w.err)
				w.focus = "done"
			}
		}
		w.reveal = true
		return nil
	}
	w.err = ""
	w.failedKind = ""
	switch v.kind {
	case "resolve":
		w.release = v.release
		w.cachedRelease, w.cachedSelection = v.release, m.selection()
		m.message = m.tr("wizard.version_resolved", v.release.Version)
		w.focus = "continue"
	case "prepare":
		w.plan = v.plan
		w.release = v.plan.Release
		w.step = stepConfirm
		w.focus = "install"
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
	if m.busy && m.job != "install" {
		m.abortLookup()
	}
	if m.job == "install" {
		return
	}
	w.networkOpen = true
	w.draft = w.cfg.Normalized()
	w.prefix.SetValue(w.draft.Prefix)
	w.mirror.SetValue(w.draft.Mirror)
	w.proxy.SetValue(w.draft.ProxyURL)
	w.returnFocus = w.focus
	w.returnErr, w.returnFailed = w.err, w.failedKind
	w.focus = "source"
	w.scroll = 0
	w.reveal = true
	w.remember = false
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
			different := w.testRelease.Version != v.release.Version || w.testRelease.Asset != v.release.Asset || w.testRelease.SHA256 != v.release.SHA256
			w.testRelease = v.release
			if different || w.fileTest.Status == checkNotApplicable {
				w.fileTest = checkState{Status: checkTesting, Detail: v.release.Asset}
				w.testPending++
				cmd = m.fileTestCmd(m.opCtx, m.id, v.release, w.draft)
			}
		}
	} else if v.release.Version == w.testRelease.Version && v.release.Asset == w.testRelease.Asset && v.release.SHA256 == w.testRelease.SHA256 {
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
	w.cfg = w.draft.Normalized()
	w.networkOpen = false
	w.focus = w.returnFocus
	w.scroll = 0
	w.reveal = true
	m.message = m.tr("wizard.network.applied")
	retry := w.returnFailed
	w.err = ""
	w.failedKind = ""
	if changed {
		w.release = mihomo.Release{}
		w.plan = app.InstallPlan{}
		if w.step == stepConfirm || w.step == stepResult {
			w.step = stepVersion
		}
	}
	if w.open && (retry != "" || w.step == stepVersion) {
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
		if w.step == stepCore || w.step == stepResult {
			w.open = false

			return nil
		}
		if w.step == stepConfirm {
			w.step = stepVersion
		} else {
			w.step = stepCore
		}
		w.focus = "continue"
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
		if w.focus == "source" || w.focus == "proxy-mode" || w.focus == "version-mode" {
			delta := 1
			if key == "left" {
				delta = -1
			}
			return m.cycleOption(w.focus, delta)
		}
	case "up":
		if w.focus == "source" || w.focus == "proxy-mode" || w.focus == "version-mode" {
			return m.cycleOption(w.focus, -1)
		}
		w.scroll = max(0, w.scroll-1)
	case "down":
		if w.focus == "source" || w.focus == "proxy-mode" || w.focus == "version-mode" {
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
			case "mirror":
				w.draft.Mirror = input.Value()
			case "proxy-url":
				w.draft.ProxyURL = input.Value()
			}
			m.draftChanged()
		} else {
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
			return &w.prefix
		case "mirror":
			return &w.mirror
		case "proxy-url":
			return &w.proxy
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
		case "source":
			sources := []string{"official", "prefix", "mirror"}
			index := 0
			for i, s := range sources {
				if s == w.draft.Source {
					index = i
				}
			}
			w.draft.Source = sources[(index+1)%len(sources)]
			m.draftChanged()
			w.reveal = true
		case "proxy-mode":
			modes := []string{"none", "environment", "custom"}
			index := 0
			for i, s := range modes {
				if s == w.draft.ProxyMode {
					index = i
				}
			}
			w.draft.ProxyMode = modes[(index+1)%len(modes)]
			m.draftChanged()
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
		case "prefix", "mirror", "proxy-url":
			if input := m.wizardInput(); input != nil {
				input.Focus()
				return textinput.Blink
			}
		}
		return nil
	}
	switch id {
	case "network":
		m.openNetwork(w.err != "")
	case "cancel":
		if m.busy {
			m.abortLookup()
		}
		w.open = false

	case "back":
		if m.busy {
			m.abortLookup()
		}
		if w.step == stepConfirm {
			w.step = stepVersion
		} else if w.step == stepVersion {
			w.step = stepCore
		} else {
			w.open = false
		}
		w.scroll = 0
		w.focus = "continue"
		w.reveal = true
	case "local-mode":
		w.localMode = !w.localMode
		w.release = mihomo.Release{}
		w.plan = app.InstallPlan{}
		if w.localMode {
			w.focus = "local"
			w.local.Focus()
		}
		w.reveal = true
	case "core":
		w.localMode = false
	case "version-mode":
		if m.busy {
			m.abortLookup()
		}
		w.specific = !w.specific
		w.cachedRelease = mihomo.Release{}
		w.cachedSelection = ""
		w.release = mihomo.Release{}
		w.plan = app.InstallPlan{}
		w.err = ""
		if w.specific {
			w.focus = "version-input"
		}
		w.reveal = true
	case "version-input", "local":
		return textinput.Blink
	case "resolve", "retry":
		if m.busy {
			return nil
		}
		if w.step == stepResult {
			w.step = stepVersion
			w.err = ""
			return m.resolveVersion()
		}
		return m.resolveVersion()
	case "continue":
		if m.busy {
			return nil
		}
		switch w.step {
		case stepCore:
			w.step = stepVersion
			w.scroll = 0
			w.focus = "continue"
			w.reveal = true
			return m.resolveVersion()
		case stepVersion:
			if w.release.Version == "" {
				return m.resolveVersion()
			}
			return m.preparePlan()
		}
	case "reinstall":
		if m.busy {
			return nil
		}
		w.reinstall = !w.reinstall
		return m.preparePlan()
	case "install":
		if !m.busy {
			return m.installCore()
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
	case "checksum":
		return "Verifying checksum"
	case "extract":
		return "Extracting"
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
	options := []string{"official", "prefix", "mirror"}
	current := &w.draft.Source
	if id == "proxy-mode" {
		options = []string{"none", "environment", "custom"}
		current = &w.draft.ProxyMode
	}
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
