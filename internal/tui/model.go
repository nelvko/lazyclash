// Package tui presents application use cases without invoking CLI subprocesses.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

// Backend is the shared application layer; it contains no terminal operations.
type Backend interface {
	Snapshot(context.Context) (app.Snapshot, error)
	Install(context.Context, string, func(string)) error
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
	err         error
	success     string
}
type progressMsg struct {
	id   uint64
	text string
}
type row struct {
	group, node string
	label       string
	selectable  bool
}

type Model struct {
	ctx              context.Context
	backend          Backend
	snapshot         app.Snapshot
	busy, quitting   bool
	id               uint64
	cancel           context.CancelFunc
	opCtx            context.Context
	message, failure string
	tab, cursor      int
	expanded         string
	form, name       string
	input            textinput.Model
	spinner          spinner.Model
	viewport         viewport.Model
	width, height    int
}

func New(ctx context.Context, backend Backend) Model {
	if ctx == nil {
		ctx = context.Background()
	}
	input := textinput.New()
	input.CharLimit = 4096
	s := spinner.New()
	s.Spinner = spinner.Dot
	opCtx, cancel := context.WithCancel(ctx)
	m := Model{ctx: ctx, backend: backend, input: input, spinner: s, viewport: viewport.New(80, 19), width: 80, height: 24, busy: true, id: 1, opCtx: opCtx, cancel: cancel, message: "Loading state…"}
	m.layout()
	return m
}

func Run(ctx context.Context, backend Backend) error {
	// Keyboard cancellation is handled by the model so transactions can roll back
	// before the alternate screen closes. Backend operations inherit ctx as well.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err := tea.NewProgram(New(runCtx, backend), tea.WithAltScreen(), tea.WithoutSignalHandler()).Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.snapshotCmd(m.id, m.opCtx), m.spinner.Tick, func() tea.Msg { <-m.ctx.Done(); return parentCanceledMsg{} })
}

func (m Model) snapshotCmd(id uint64, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		snapshot, err := m.backend.Snapshot(ctx)
		return resultMsg{id: id, snapshot: snapshot, hasSnapshot: err == nil, err: err, success: "State refreshed"}
	}
}

func progressWait(ctx context.Context, updates <-chan progressMsg) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-updates:
			if !ok {
				return nil
			}
			return msg
		}
	}
}

type nextProgressMsg struct {
	progressMsg
	ctx     context.Context
	updates <-chan progressMsg
}

func waitProgress(ctx context.Context, updates <-chan progressMsg) tea.Cmd {
	return func() tea.Msg {
		if msg := progressWait(ctx, updates)(); msg != nil {
			return nextProgressMsg{progressMsg: msg.(progressMsg), ctx: ctx, updates: updates}
		}
		return nil
	}
}

func (m *Model) begin(label, success string, work func(context.Context, func(string)) error) tea.Cmd {
	m.id++
	id := m.id
	ctx, cancel := context.WithCancel(m.ctx)
	m.opCtx, m.cancel = ctx, cancel
	m.busy, m.message, m.failure = true, label, ""
	updates := make(chan progressMsg, 8)
	report := func(s string) {
		select {
		case updates <- progressMsg{id: id, text: s}:
		default:
		}
	}
	backend := m.backend
	run := func() tea.Msg {
		defer close(updates)
		err := work(ctx, report)
		// Even a failed mutation can have completed a rollback. Refresh the visible
		// state independently of the canceled operation, with a bounded context.
		refreshCtx, refreshCancel := context.WithTimeout(m.ctx, 5*time.Second)
		defer refreshCancel()
		snapshot, snapshotErr := backend.Snapshot(refreshCtx)
		if snapshotErr != nil {
			err = errors.Join(err, fmt.Errorf("refresh current state: %w", snapshotErr))
		}
		return resultMsg{id: id, snapshot: snapshot, hasSnapshot: snapshotErr == nil, err: err, success: success}
	}
	return tea.Batch(run, waitProgress(ctx, updates), m.spinner.Tick)
}

func (m *Model) refresh() tea.Cmd {
	m.id++
	m.opCtx, m.cancel = context.WithCancel(m.ctx)
	m.busy, m.failure, m.message = true, "", "Refreshing…"
	return tea.Batch(m.snapshotCmd(m.id, m.opCtx), m.spinner.Tick)
}

func (m *Model) openForm(kind string) tea.Cmd {
	m.form, m.name = kind, ""
	m.input.Reset()
	m.input.EchoMode = textinput.EchoNormal
	switch kind {
	case "name":
		m.input.Placeholder = "subscription name"
	case "bin":
		m.input.Placeholder = "/absolute/path/to/mihomo"
	}
	m.input.Focus()
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case parentCanceledMsg:
		if m.busy {
			m.quitting = true
			m.cancel()
			m.message = "Canceling; waiting for cleanup before exit…"
		} else {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
	case resultMsg:
		if msg.id != m.id {
			return m, nil
		}
		if m.cancel != nil {
			m.cancel()
		}
		m.busy = false
		if msg.hasSnapshot {
			m.snapshot = msg.snapshot
		}
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				m.message = "Operation canceled; current state refreshed"
				m.failure = clean(msg.err.Error())
			} else {
				m.failure = clean(msg.err.Error())
				m.message = "Operation failed"
			}
		} else {
			m.message, m.failure = msg.success, ""
		}
		m.clampCursor()
		if m.quitting {
			return m, tea.Quit
		}
	case nextProgressMsg:
		if msg.id == m.id && m.busy {
			m.message = clean(msg.text)
			cmd = waitProgress(msg.ctx, msg.updates)
		}
	case spinner.TickMsg:
		if m.busy {
			m.spinner, cmd = m.spinner.Update(msg)
		}
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" || (m.form == "" && key == "q") {
			if m.busy {
				m.quitting = true
				m.cancel()
				m.message = "Canceling; waiting for cleanup before exit…"
			} else {
				return m, tea.Quit
			}
			break
		}
		if key == "esc" {
			if m.busy {
				m.cancel()
				m.message = "Canceling; waiting for cleanup…"
			} else if m.form != "" {
				m.form = ""
				m.input.Blur()
				m.message = "Input canceled"
			} else {
				m.expanded = ""
				m.clampCursor()
			}
			break
		}
		if m.busy {
			break
		}
		if m.form != "" {
			if key == "enter" {
				value := strings.TrimSpace(m.input.Value())
				if value == "" {
					m.failure = "A value is required"
					break
				}
				switch m.form {
				case "name":
					m.name, m.form = value, "source"
					m.input.Reset()
					m.input.Placeholder = "HTTPS URL or absolute YAML path"
					m.input.EchoMode = textinput.EchoPassword
					m.failure = ""
				case "source":
					name, source := m.name, value
					m.form = ""
					m.input.Reset()
					m.input.Blur()
					cmd = m.begin("Fetching and validating subscription…", "Subscription saved; select it in Profiles to apply", func(ctx context.Context, _ func(string)) error { return m.backend.Add(ctx, name, source) })
				case "bin":
					m.form = ""
					m.input.Reset()
					m.input.Blur()
					cmd = m.begin("Validating local Mihomo…", "Mihomo installed; press s to start", func(ctx context.Context, p func(string)) error { return m.backend.Install(ctx, value, p) })
				}
			} else {
				m.input, cmd = m.input.Update(msg)
			}
			break
		}
		switch key {
		case "tab", "1", "2", "3":
			if key == "tab" {
				m.tab = (m.tab + 1) % 3
			} else {
				m.tab = int(key[0] - '1')
			}
			m.cursor = 0
			m.viewport.GotoTop()
		case "i":
			if !m.snapshot.Installed {
				cmd = m.begin("Downloading verified Mihomo…", "Mihomo installed; press s to start", func(ctx context.Context, p func(string)) error { return m.backend.Install(ctx, "", p) })
			}
		case "b":
			if !m.snapshot.Installed {
				cmd = m.openForm("bin")
			}
		case "a":
			if m.snapshot.Installed {
				cmd = m.openForm("name")
			} else {
				m.failure = "Install Mihomo before adding a subscription"
			}
		case "s", "x", "r":
			action := map[string]string{"s": "start", "x": "stop", "r": "restart"}[key]
			if m.snapshot.Installed {
				cmd = m.begin("Service: "+action+"…", "Service "+action+" completed", func(ctx context.Context, _ func(string)) error { return m.backend.Control(ctx, action) })
			} else {
				m.failure = "Mihomo is not installed"
			}
		case "g", "ctrl+r":
			cmd = m.refresh()
		case "down", "j":
			m.cursor++
			m.clampCursor()
			m.followCursor()
		case "up", "k":
			m.cursor--
			m.clampCursor()
			m.followCursor()
		case "enter":
			if m.tab == 1 && m.snapshot.Installed {
				profiles := m.profiles()
				name := profiles[m.cursor]
				cmd = m.begin("Validating and applying configuration…", "Configuration applied", func(ctx context.Context, _ func(string)) error { return m.backend.Apply(ctx, name) })
			} else if m.tab == 2 {
				rows := m.rows()
				if len(rows) > 0 {
					item := rows[m.cursor]
					if item.node == "" {
						if m.expanded == item.group {
							m.expanded = ""
						} else {
							m.expanded = item.group
						}
						m.clampCursor()
					} else if item.selectable {
						cmd = m.begin("Switching selected node…", "Node selected", func(ctx context.Context, _ func(string)) error { return m.backend.Select(ctx, item.group, item.node) })
					} else {
						m.failure = "This group chooses nodes automatically; only Selector groups can be switched"
					}
				}
			}
		case "pgup", "pgdown", "home", "end":
			m.viewport, cmd = m.viewport.Update(msg)
		}
	}
	m.layout()
	return m, cmd
}

func (m Model) profiles() []string {
	profiles := []string{"default"}
	seen := map[string]bool{"default": true}
	for _, name := range m.snapshot.Profiles {
		if !seen[name] {
			profiles = append(profiles, name)
			seen[name] = true
		}
	}
	return profiles
}

func (m Model) rows() []row {
	var rows []row
	for _, group := range m.snapshot.Groups {
		rows = append(rows, row{group: group.Name, label: fmt.Sprintf("%s [%s] → %s", clean(group.Name), clean(group.Type), clean(group.Now))})
		if m.expanded == group.Name {
			for _, node := range group.All {
				mark := "  "
				if node == group.Now {
					mark = "✓ "
				}
				rows = append(rows, row{group: group.Name, node: node, label: "    " + mark + clean(node), selectable: group.Type == "Selector"})
			}
		}
	}
	return rows
}

func (m *Model) clampCursor() {
	count := 0
	switch m.tab {
	case 1:
		count = len(m.profiles())
	case 2:
		count = len(m.rows())
	}
	m.cursor = max(0, min(m.cursor, max(0, count-1)))
}

func (m *Model) followCursor() {
	// Lists have one introductory line before their selectable rows.
	line := m.cursor + 1
	if line < m.viewport.YOffset {
		m.viewport.SetYOffset(line)
	}
	if line >= m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(line - m.viewport.Height + 1)
	}
}

func (m *Model) layout() {
	m.viewport.Width = max(1, m.width)
	m.viewport.Height = max(1, m.height-5)
	m.input.Width = max(1, min(76, m.width-4))
	m.viewport.SetContent(m.content())
}

func (m Model) content() string {
	if m.form != "" {
		label := map[string]string{"name": "Subscription name", "source": "Subscription source (hidden)", "bin": "Local Mihomo binary"}[m.form]
		return label + "\n\n" + m.input.View() + "\n\nEnter continue · Esc cancel"
	}
	switch m.tab {
	case 1:
		if !m.snapshot.Installed {
			return "Install Mihomo first: i download · b local binary"
		}
		lines := []string{"Enter validates and applies; a adds a subscription"}
		for i, name := range m.profiles() {
			mark := "  "
			if i == m.cursor {
				mark = "> "
			}
			active := ""
			if name == m.snapshot.Applied {
				active = " [applied]"
			}
			lines = append(lines, mark+clean(name)+active)
		}
		return strings.Join(lines, "\n")
	case 2:
		lines := []string{"Enter expands a group or selects a node"}
		if m.snapshot.APIError != "" {
			lines = append(lines, "API unavailable: "+clean(m.snapshot.APIError))
		}
		rows := m.rows()
		if len(rows) == 0 {
			lines = append(lines, "No proxy groups available. Start Mihomo, then press g.")
		}
		for i, item := range rows {
			mark := "  "
			if i == m.cursor {
				mark = "> "
			}
			lines = append(lines, mark+item.label)
		}
		return strings.Join(lines, "\n")
	default:
		if !m.snapshot.Installed {
			lines := []string{"Mihomo is not installed.", "", "i  Download official Mihomo (fixed release + SHA256)", "b  Install a local binary", "", "A valid default configuration is created during installation.", "No subscription is required to start."}
			if m.snapshot.ServiceError != "" {
				lines = append(lines, "", "Service unavailable: "+clean(m.snapshot.ServiceError))
			}
			return strings.Join(lines, "\n")
		}
		lines := []string{"Mihomo: " + clean(m.snapshot.Version), "Service: " + clean(m.snapshot.Service.State), "Controller: " + clean(m.snapshot.Controller), "Applied profile: " + clean(m.snapshot.Applied), "", "s start · x stop · r restart", "2 Profiles: add and apply subscriptions", "3 Nodes: view groups and select nodes"}
		if m.snapshot.ServiceError != "" {
			lines = append(lines, "", "Service unavailable: "+clean(m.snapshot.ServiceError))
		} else if !m.snapshot.Service.Linger {
			lines = append(lines, "", "Linger is disabled: the service may stop after logout.", "For background sessions: loginctl enable-linger $(id -un)")
		}
		if m.snapshot.APIError != "" {
			lines = append(lines, "", "API unavailable: "+clean(m.snapshot.APIError))
		}
		return strings.Join(lines, "\n")
	}
}

func (m Model) View() string {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69")).Render("lazyclash")
	tabs := []string{"1 Overview", "2 Profiles", "3 Nodes"}
	tabs[m.tab] = "[" + tabs[m.tab] + "]"
	status := m.message
	if m.busy {
		status = m.spinner.View() + " " + status
	}
	if m.failure != "" {
		status = "Error: " + m.failure
	}
	help := "Tab panels · ↑↓ select · Enter apply/select · PgUp/PgDn scroll"
	if m.form != "" {
		help = "Enter continue · Esc cancel input · Ctrl+C quit"
	} else if m.busy {
		help = "Esc cancel · q/Ctrl+C cancel, clean up and quit"
	}
	footer := "i/b install · a add · s/x/r service · g refresh · q quit"
	lines := []string{header, strings.Join(tabs, "  "), m.viewport.View(), status, help, footer}
	if m.height < 6 {
		lines = []string{header + " " + status, "Esc cancel · q quit"}
		lines = lines[:min(len(lines), m.height)]
	}
	for i, line := range lines {
		if strings.Contains(line, "\n") {
			rows := strings.Split(line, "\n")
			for j := range rows {
				rows[j] = ansi.Truncate(rows[j], m.width, "…")
			}
			lines[i] = strings.Join(rows, "\n")
		} else {
			lines[i] = ansi.Truncate(line, m.width, "…")
		}
	}
	return strings.Join(lines, "\n")
}

// Strip terminal control sequences even when they arrive in proxy node names.
// Removing only ESC would leave malicious OSC payloads visible; ansi.Strip also
// removes the sequences themselves before ordinary control characters are cut.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}
