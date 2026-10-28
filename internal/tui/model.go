// Package tui presents shared application use cases in a terminal.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type Backend interface {
	Snapshot(context.Context) (app.Snapshot, error)
	LoadPreferences(context.Context) (app.Preferences, error)
	SaveNetworkPreferences(context.Context, downloadnet.Config) error
	SetGuideDismissed(context.Context, bool) error
	ResolveRelease(context.Context, string, downloadnet.Config) (mihomo.Release, error)
	TestReleaseFile(context.Context, mihomo.Release, downloadnet.Config) error
	PrepareInstall(context.Context, mihomo.Release, string, bool) (app.InstallPlan, error)
	InstallCore(context.Context, app.InstallPlan, downloadnet.Config, func(mihomo.Progress)) (app.InstallResult, error)
	Add(context.Context, string, string) error
	Apply(context.Context, string) error
	Control(context.Context, string) error
	Select(context.Context, string, string) error
}

type parentCanceledMsg struct{}
type resultMsg struct {
	id          uint64
	snapshot    app.Snapshot
	hasSnapshot bool
	prefs       *app.Preferences
	err         error
	success     string
}
type wizardMsg struct {
	progress    mihomo.Progress
	id          uint64
	kind        string
	release     mihomo.Release
	plan        app.InstallPlan
	result      app.InstallResult
	snapshot    app.Snapshot
	hasSnapshot bool
	err         error
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
	ctx                   context.Context
	backend               Backend
	snapshot              app.Snapshot
	busy, quitting        bool
	id                    uint64
	cancel                context.CancelFunc
	opCtx                 context.Context
	job, message, failure string
	page, focus, hover    string
	scroll, total         int
	sidebarOpen           bool
	form, name            string
	input                 textinput.Model
	spinner               spinner.Model
	width, height         int
	targets               []hitRegion
	screen                string
	wizard                wizardState
}

func New(ctx context.Context, backend Backend) Model {
	if ctx == nil {
		ctx = context.Background()
	}
	input := textinput.New()
	input.CharLimit = 8192
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	opCtx, cancel := context.WithCancel(ctx)
	m := Model{ctx: ctx, backend: backend, busy: true, id: 1, cancel: cancel, opCtx: opCtx, job: "refresh", message: "Loading state…", page: "overview", focus: "install", input: input, spinner: spin, width: 80, height: 24, wizard: newWizard()}
	m.layout()
	return m
}
func Run(ctx context.Context, backend Backend) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err := tea.NewProgram(New(runCtx, backend), tea.WithoutSignalHandler()).Run()
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
		return resultMsg{id: id, snapshot: s, hasSnapshot: err == nil, prefs: p, err: errors.Join(pe, err), success: "State refreshed"}
	}
}
func (m *Model) newJob(kind string) context.Context {
	if m.cancel != nil {
		m.cancel()
	}
	m.id++
	m.opCtx, m.cancel = context.WithCancel(m.ctx)
	m.busy = true
	m.job = kind
	m.failure = ""
	return m.opCtx
}
func (m *Model) abortLookup() {
	if m.cancel != nil {
		m.cancel()
	}
	m.id++
	m.busy = false
	m.job = ""
}
func (m *Model) refresh() tea.Cmd {
	ctx := m.newJob("refresh")
	m.message = "Refreshing…"
	return tea.Batch(m.snapshotCmd(m.id, ctx, false), m.spinner.Tick)
}
func (m *Model) begin(label, success string, work func(context.Context) error) tea.Cmd {
	ctx := m.newJob("action")
	id := m.id
	backend := m.backend
	m.message = label
	return tea.Batch(func() tea.Msg {
		err := work(ctx)
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		s, se := backend.Snapshot(refreshCtx)
		if se != nil {
			err = errors.Join(err, fmt.Errorf("refresh current state: %w", se))
		}
		return resultMsg{id: id, snapshot: s, hasSnapshot: se == nil, err: err, success: success}
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
	if m.cancel != nil {
		m.cancel()
	}
	m.quitting = m.quitting || quit
	m.message = "Cancelling / Cleaning up…"
	if m.job == "install" {
		m.wizard.cancelling = true
		m.wizard.progress.Detail = "Waiting for safe cleanup before leaving"
	}
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch v := msg.(type) {
	case parentCanceledMsg:
		if m.busy {
			m.cancelTask(true)
		} else {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, v.Width), max(1, v.Height)
		m.wizard.reveal = true
	case resultMsg:
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
		}
		if v.prefs != nil {
			m.wizard.cfg = v.prefs.Network.Normalized()
			m.wizard.draft = m.wizard.cfg
			m.snapshot.GuideDismissed = v.prefs.GuideDismissed || m.snapshot.GuideDismissed
		}
		if v.err != nil {
			m.failure = clean(v.err.Error())
			m.message = "Operation failed"
			if errors.Is(v.err, context.Canceled) {
				m.message = "Operation canceled; cleanup completed"
			}
		} else {
			m.message = v.success
			m.failure = ""
		}
		if m.quitting {
			return m, tea.Quit
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
		}
		cmd = m.handleWizardResult(v)
		if m.quitting {
			return m, tea.Quit
		}
	case networkMsg:
		if v.id != m.id {
			return m, nil
		}
		cmd = m.handleNetworkResult(v)
		if m.quitting && !m.busy {
			return m, tea.Quit
		}
	case nextProgressMsg:
		if v.id == m.id && m.busy {
			m.wizard.progress = v.progress
			m.wizard.logs = append(m.wizard.logs, stageLabel(v.progress.Stage)+": "+clean(v.progress.Detail))
			if len(m.wizard.logs) > 80 {
				m.wizard.logs = m.wizard.logs[len(m.wizard.logs)-80:]
			}
			cmd = waitProgress(v.ctx, v.updates)
		}
	case spinner.TickMsg:
		if m.busy {
			m.spinner, cmd = m.spinner.Update(v)
		}
	case tea.MouseMsg:
		cmd = m.mouse(v)
	case tea.PasteMsg:
		// Bracketed paste is text input, never a global command. In particular a
		// pasted q or digit cannot quit or navigate out of a form.
		if m.wizard.open || m.wizard.networkOpen {
			cmd = m.updateWizardInput(v)
		} else if m.form != "" {
			m.input, cmd = m.input.Update(v)
		}
	case tea.KeyPressMsg:
		key := v.String()
		if key == "ctrl+c" {
			if m.busy {
				m.cancelTask(true)
			} else {
				return m, tea.Quit
			}
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
		if key == "q" {
			if m.busy {
				m.cancelTask(true)
			} else {
				return m, tea.Quit
			}
			break
		}
		if m.busy {
			if key == "esc" {
				m.cancelTask(false)
			}
			break
		}
		switch key {
		case "i":
			m.openWizard(false)
		case "b":
			m.openWizard(true)
			cmd = textinput.Blink
		case "n":
			m.openNetwork(false)
		case "a":
			cmd = m.openForm()
		case "s", "x", "r":
			action := map[string]string{"s": "start", "x": "stop", "r": "restart"}[key]
			cmd = m.serviceAction(action)
		case "1", "2", "3", "4":
			m.navigate(map[string]string{"1": "overview", "2": "nodes", "3": "profiles", "4": "settings"}[key])
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
			m.scroll += max(1, m.height-9)
		case "pgup":
			m.scroll = max(0, m.scroll-max(1, m.height-9))
		case "home":
			m.scroll = 0
		case "end":
			m.scroll = m.total
		case "esc":
			m.sidebarOpen = false
		case "g", "ctrl+r":
			cmd = m.refresh()
		case "?":
			m.message = "i Install · n Network · a Import/Add · s/x/r Service · 1–4 Pages · Tab Focus · Enter Open · Esc Back"
		}
	default:
		// Forward cursor/clipboard messages to the focused Bubbles input. The
		// child model ignores messages it does not own, including key releases.
		if m.wizard.open || m.wizard.networkOpen {
			cmd = m.updateWizardInput(msg)
		} else if m.form != "" {
			m.input, cmd = m.input.Update(msg)
		}
	}
	m.layout()
	return m, cmd
}
func (m *Model) navigate(page string) {
	m.page = page
	m.scroll = 0
	m.focus = "nav." + page
	m.sidebarOpen = false
}
func (m *Model) openWizard(local bool) {
	m.wizard.open = true
	m.wizard.networkOpen = false
	m.wizard.step = stepCore
	m.wizard.err = ""
	m.wizard.failedKind = ""
	m.wizard.failedStage = ""
	m.wizard.localMode = local
	m.wizard.cancelling = false
	m.wizard.scroll = 0
	m.wizard.focus = "continue"
	if local {
		m.wizard.focus = "local"
		m.wizard.local.Focus()
	}
	m.failure = ""
	m.wizard.reveal = true
}
func (m *Model) openForm() tea.Cmd {
	m.form = "name"
	m.name = ""
	m.input.Reset()
	m.input.EchoMode = textinput.EchoNormal
	m.input.Placeholder = "profile name"
	m.input.Focus()
	return textinput.Blink
}
func (m *Model) formKey(k tea.KeyPressMsg) tea.Cmd {
	key := k.String()
	if key == "esc" {
		m.form = ""
		m.input.Reset()
		m.input.Blur()
		m.message = "Input canceled"
		return nil
	}
	if key == "enter" {
		value := strings.TrimSpace(m.input.Value())
		if value == "" {
			m.failure = "A value is required"
			return nil
		}
		if m.form == "name" {
			m.name = value
			m.form = "source"
			m.input.Reset()
			m.input.EchoMode = textinput.EchoPassword
			m.input.Placeholder = "HTTPS URL or absolute YAML path"
			return nil
		}
		name, source := m.name, value
		m.form = ""
		m.input.Reset()
		m.input.Blur()
		return m.begin("Fetching and validating configuration…", "Configuration saved; apply it from Subscriptions", func(ctx context.Context) error { return m.backend.Add(ctx, name, source) })
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return cmd
}
func (m *Model) serviceAction(action string) tea.Cmd {
	if !m.snapshot.Installed {
		m.failure = "Mihomo is not installed"
		return nil
	}
	return m.begin("Service: "+action+"…", "Service "+action+" completed", func(ctx context.Context) error { return m.backend.Control(ctx, action) })
}
func (m *Model) activate(id string) tea.Cmd {
	switch {
	case strings.HasPrefix(id, "nav."):
		m.navigate(strings.TrimPrefix(id, "nav."))
	case id == "menu":
		m.sidebarOpen = !m.sidebarOpen
	case id == "install":
		m.openWizard(false)
	case id == "network":
		m.openNetwork(false)
	case id == "import" || id == "add":
		return m.openForm()
	case id == "dismiss":
		return m.begin("Saving guide preference…", "Guide dismissed; Install and Network remain in Settings", func(ctx context.Context) error { return m.backend.SetGuideDismissed(ctx, true) })
	case id == "start" || id == "stop" || id == "restart":
		return m.serviceAction(id)
	case strings.HasPrefix(id, "profile."):
		n, err := strconv.Atoi(strings.TrimPrefix(id, "profile."))
		profiles := m.profiles()
		if err == nil && n >= 0 && n < len(profiles) {
			name := profiles[n]
			return m.begin("Validating and applying configuration…", "Configuration applied", func(ctx context.Context) error { return m.backend.Apply(ctx, name) })
		}
	case strings.HasPrefix(id, "group."):
		n, err := strconv.Atoi(strings.TrimPrefix(id, "group."))
		if err == nil && n >= 0 && n < len(m.snapshot.Groups) {
			g := m.snapshot.Groups[n]
			if m.wizard.expanded == g.Name {
				m.wizard.expanded = ""
			} else {
				m.wizard.expanded = g.Name
			}
		}
	case strings.HasPrefix(id, "node."):
		parts := strings.Split(strings.TrimPrefix(id, "node."), ".")
		if len(parts) != 2 {
			return nil
		}
		gi, e1 := strconv.Atoi(parts[0])
		ni, e2 := strconv.Atoi(parts[1])
		if e1 == nil && e2 == nil && gi >= 0 && gi < len(m.snapshot.Groups) {
			g := m.snapshot.Groups[gi]
			if ni >= 0 && ni < len(g.All) {
				if g.Type != "Selector" {
					m.failure = "This group selects nodes automatically"
					return nil
				}
				node := g.All[ni]
				return m.begin("Switching selected node…", "Node selected", func(ctx context.Context) error { return m.backend.Select(ctx, g.Name, node) })
			}
		}
	}
	return nil
}
func (m *Model) cycleFocus(delta int) {
	targets := m.targets
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
	m.focus = targets[current].ID
}
func (m *Model) mouse(event tea.MouseMsg) tea.Cmd {
	k := event.Mouse()
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
				if m.wizard.open || m.wizard.networkOpen {
					m.wizard.focus = r.ID
					return m.wizardActivate(r.ID)
				}
				if m.form != "" {
					m.input.Focus()
					return textinput.Blink
				}
				if !m.busy {
					m.focus = r.ID
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
	var lines []string
	var hits []hitRegion
	add := func(id, label string) {
		row := len(lines)
		s := "  " + clean(label)
		if m.focus == id {
			s = "> " + clean(label)
		}
		lines = append(lines, themed(cellCrop(s, width), text, bg, false))
		hits = append(hits, hitRegion{ID: id, Y: row, W: width, H: 1})
	}
	if m.page == "profiles" {
		lines = append(lines, themed("Saved configurations", purple, bg, false), "URL subscription and local YAML use the same validation and apply flow.", "")
		add("add", "Add subscription / Import config")
		if !m.snapshot.Installed {
			lines = append(lines, strings.Split(wrapText("You may import now. Engine validation runs before application after core installation.", width), "\n")...)
		}
		for i, name := range m.profiles() {
			mark := ""
			if name == m.snapshot.Applied {
				mark = " [applied]"
			}
			for _, pending := range m.snapshot.UnvalidatedProfiles {
				if pending == name {
					mark += " [engine validation pending]"
					break
				}
			}
			add(fmt.Sprintf("profile.%d", i), name+mark)
		}
	} else {
		lines = append(lines, themed("Proxy groups and nodes", purple, bg, false), "Enter a group to expand; select a node in a Selector group.", "")
		if m.snapshot.APIError != "" {
			lines = append(lines, strings.Split(wrapText("API unavailable: "+clean(m.snapshot.APIError), width), "\n")...)
		}
		for gi, g := range m.snapshot.Groups {
			add(fmt.Sprintf("group.%d", gi), fmt.Sprintf("%s [%s] → %s", g.Name, g.Type, g.Now))
			if m.wizard.expanded == g.Name {
				for ni, node := range g.All {
					mark := "  "
					if node == g.Now {
						mark = "✓ "
					}
					add(fmt.Sprintf("node.%d.%d", gi, ni), "    "+mark+node)
				}
			}
		}
		if len(m.snapshot.Groups) == 0 {
			lines = append(lines, "No proxy groups available. Start Mihomo, then refresh.")
		}
	}
	// Body rows are single display lines, including hard-wrapped descriptions.
	var expanded []string
	for _, s := range lines {
		expanded = append(expanded, strings.Split(s, "\n")...)
	}
	return strings.Join(expanded, "\n"), hits
}
func (m *Model) layout() {
	o := dashboardOptions{Width: m.width, Height: m.height, Page: m.page, SelectedGroup: m.wizard.expanded, Snapshot: m.snapshot, Focus: m.focus, Hover: m.hover, GuideDismissed: m.snapshot.GuideDismissed, Scroll: m.scroll, Dim: m.wizard.open || m.wizard.networkOpen || m.form != "", Message: m.message, Failure: m.failure, SidebarOpen: m.sidebarOpen}
	o.Body, o.BodyTargets = m.pageBody(dashboardBodyWidth(o))
	base, hits, total := renderDashboard(o)
	m.total = total
	m.scroll = min(m.scroll, max(0, total-max(0, m.height-7)))
	m.screen, m.targets = base, hits
	if m.wizard.open || m.wizard.networkOpen {
		m.screen, m.targets = m.renderWizard(base)
	} else if m.form != "" {
		m.screen, m.targets = m.renderImport(base)
	}
}
func (m Model) View() tea.View {
	view := tea.NewView(m.screen)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeAllMotion
	view.BackgroundColor = lipgloss.Color(bg)
	view.ForegroundColor = lipgloss.Color(text)
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
