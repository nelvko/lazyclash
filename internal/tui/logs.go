package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

const (
	retainedLogLimit = 200
	maxLogScrollRows = 1 << 20
)

type logsState struct {
	entries                []mihomo.LogEntry
	search                 textinput.Model
	editing, paused, ended bool
	active, pending        bool
	generation             uint64
	cancel                 context.CancelFunc
	ctx                    context.Context
	updates                <-chan logsResultMsg
	err                    string
	observedAt             time.Time
	offset                 int // visible rows back from the newest entry
	gap                    bool
}

type logsResultMsg struct {
	generation uint64
	entries    []mihomo.LogEntry
	done       bool
	err        error
}

func newLogsState() logsState {
	search := textinput.New()
	search.CharLimit = 256
	search.Placeholder = "Level or redacted message"
	styleInput(&search, bg, false)
	return logsState{search: search}
}

func (m *Model) stopLogs() {
	if m.logs.cancel != nil {
		m.logs.cancel()
		m.logs.cancel = nil
	}
	if m.logs.active || m.logs.pending {
		m.logs.generation++
		m.logs.gap = true
	}
	m.logs.active, m.logs.pending = false, false
}

func (m Model) logsVisible() bool {
	return m.page == "logs" && !m.busy && !m.quitting && !m.helpOpen && !m.wizard.open && !m.wizard.networkOpen && m.form == "" && m.ctx.Err() == nil
}

// One owner reads a bounded channel; the stream blocks at that boundary until
// the next command can drain it. Cancellation releases either side promptly.
func waitLogs(ctx context.Context, updates <-chan logsResultMsg) tea.Cmd {
	return func() tea.Msg {
		var result logsResultMsg
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-updates:
			if !ok {
				return nil
			}
			result = msg
		}
		for n := 1; n < 32 && !result.done; n++ {
			select {
			case msg, ok := <-updates:
				if !ok {
					return result
				}
				result.entries = append(result.entries, msg.entries...)
				result.done, result.err = msg.done, msg.err
			default:
				return result
			}
		}
		return result
	}
}

func (m *Model) syncLogs() tea.Cmd {
	if !m.logsVisible() {
		m.stopLogs()
		return nil
	}
	if m.logs.active || m.logs.paused || m.logs.ended {
		return nil
	}
	if m.logs.gap {
		m.appendLogs([]mihomo.LogEntry{{Time: time.Now(), Level: "gap", Message: "Capture resumed; logs while inactive were not collected."}})
		m.logs.gap = false
	}
	m.logs.generation++
	id := m.logs.generation
	ctx, cancel := context.WithCancel(m.ctx)
	updates := make(chan logsResultMsg, 32)
	m.logs.active, m.logs.pending, m.logs.err = true, true, ""
	m.logs.cancel, m.logs.ctx, m.logs.updates = cancel, ctx, updates
	backend := m.backend
	run := func() tea.Msg {
		defer close(updates)
		err := backend.StreamLogs(ctx, func(entry mihomo.LogEntry) {
			select {
			case updates <- logsResultMsg{generation: id, entries: []mihomo.LogEntry{entry}}:
			case <-ctx.Done():
			}
		})
		select {
		case updates <- logsResultMsg{generation: id, done: true, err: err}:
		case <-ctx.Done():
		}
		return nil
	}
	return tea.Batch(run, waitLogs(ctx, updates))
}

func (m *Model) appendLogs(entries []mihomo.LogEntry) {
	for _, entry := range entries {
		// The application owns secret redaction; sanitize terminal controls again
		// at the display boundary. A faulty backend cannot grow the UI unbounded.
		entry.Level = cellCrop(clean(entry.Level), 16)
		message := clean(entry.Message)
		if len(message) > 4096 {
			message = string([]rune(message)[:min(1024, len([]rune(message)))]) + "…"
		}
		entry.Message = message
		m.logs.entries = append(m.logs.entries, entry)
		m.logs.observedAt = entry.Time
	}
	if len(m.logs.entries) > retainedLogLimit {
		copy(m.logs.entries, m.logs.entries[len(m.logs.entries)-retainedLogLimit:])
		m.logs.entries = m.logs.entries[:retainedLogLimit]
	}
}

func (m *Model) acceptLogs(msg logsResultMsg) tea.Cmd {
	if !m.logs.active || msg.generation != m.logs.generation {
		return nil
	}
	m.appendLogs(msg.entries)
	if !m.logs.paused {
		m.logs.offset = 0
	}
	if msg.done {
		if m.logs.cancel != nil {
			m.logs.cancel()
			m.logs.cancel = nil
		}
		m.logs.active, m.logs.pending, m.logs.ended = false, false, true
		if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
			m.logs.err = cellCrop(clean(msg.err.Error()), 4096)
		}
		return nil
	}
	return waitLogs(m.logs.ctx, m.logs.updates)
}

func (m *Model) pauseLogs() {
	m.stopLogs()
	m.logs.paused = true
}

func (m *Model) resumeLogs() tea.Cmd {
	interrupted := m.logs.paused || (m.logs.ended && len(m.logs.entries) > 0)
	m.stopLogs()
	m.logs.paused, m.logs.ended = false, false
	if interrupted {
		m.logs.gap = true
	}
	m.logs.offset, m.scroll = 0, 0
	// Reconnecting must also refresh the service summary when the new core is
	// quiet; waiting for its first event would leave a previous Stopped header.
	return tea.Batch(m.syncLogs(), m.refreshCoreStatus())
}

func (m *Model) logsActivate(id string) tea.Cmd {
	switch id {
	case "logs.follow":
		if m.logs.active {
			m.pauseLogs()
			return nil
		}
		return m.resumeLogs()
	case "logs.retry":
		return m.resumeLogs()
	case "logs.search":
		m.pauseLogs()
		m.logs.editing, m.focus, m.scroll = true, "logs.search", 0
		return m.logs.search.Focus()
	}
	return nil
}

func (m *Model) logsSearchKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "enter", "esc", "tab", "shift+tab":
		m.logs.editing = false
		m.logs.search.Blur()
		m.focus = "logs.list"
		return nil
	}
	var cmd tea.Cmd
	m.logs.search, cmd = m.logs.search.Update(k)
	m.scroll = 0
	return cmd
}

func (m *Model) logsKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "/":
		return true, m.logsActivate("logs.search")
	case "g", "ctrl+r":
		return true, m.logsActivate("logs.retry")
	case "f":
		return true, m.logsActivate("logs.follow")
	case "space", " ":
		if m.focus == "logs.list" {
			return true, m.logsActivate("logs.follow")
		}
	case "up", "k", "pgup", "home":
		m.pauseLogs()
		if k.String() == "home" {
			m.logs.offset = maxLogScrollRows
			return true, nil
		}
		step := 1
		if k.String() == "pgup" {
			step = min(maxLogScrollRows, max(1, m.height/2))
		}
		m.logs.offset = min(maxLogScrollRows, max(0, min(maxLogScrollRows, m.logs.offset))+step)
		return true, nil
	case "down", "j", "pgdown":
		m.pauseLogs()
		step := 1
		if k.String() == "pgdown" {
			step = min(maxLogScrollRows, max(1, m.height/2))
		}
		m.logs.offset = max(0, min(maxLogScrollRows, m.logs.offset)-step)
		return true, nil
	case "end":
		return true, m.resumeLogs()
	}
	return false, nil
}

// logsScroll is used by the page's mouse wheel. Positive delta scrolls down.
func (m *Model) logsScroll(delta int) {
	m.pauseLogs()
	delta = max(-maxLogScrollRows, min(maxLogScrollRows, delta))
	current := max(0, min(maxLogScrollRows, m.logs.offset))
	m.logs.offset = max(0, min(maxLogScrollRows, current-delta))
}

func (m Model) logsBody(width int) (string, []hitRegion) {
	var lines []string
	var hits []hitRegion
	add := func(value, color string) {
		lines = append(lines, strings.Split(wrapText(themed(value, color, bg, false), max(1, width)), "\n")...)
	}
	button := func(id, label string) {
		value := dashboardButton(id, label, dashboardOptions{Focus: m.focus, Hover: m.hover}, false)
		hits = append(hits, hitRegion{ID: id, Y: len(lines), W: min(width, ansi.StringWidth(value)), H: 1})
		lines = append(lines, cellCrop(value, width))
	}
	add("Mihomo logs", text)
	status, color := "Capture ended · retained entries are last observed", muted
	if m.logs.active {
		status, color = "Capturing live events · no historical replay", info
		if len(m.logs.entries) == 0 {
			status = "Waiting for live events · no historical replay"
		}
	} else if m.logs.paused {
		status = "Paused · logs while paused are not collected"
	} else if !m.logs.ended {
		status = "Opening live log capture…"
	}
	add(status, color)
	add("Only this session; at most 200 redacted entries retained in memory.", muted)
	if !m.logs.observedAt.IsZero() {
		add("Last observed locally: "+m.logs.observedAt.Local().Format("15:04:05"), muted)
	}
	follow := "Pause capture (f)"
	if !m.logs.active {
		follow = "Resume capture (f)"
	}
	button("logs.follow", follow)
	button("logs.retry", "Reconnect / Retry (g)")
	if m.logs.editing {
		add("Search retained entries · Enter / Esc finish", info)
		m.logs.search.SetWidth(max(1, width-2))
		lines = append(lines, cellCrop(m.logs.search.View(), width))
		hits = append(hits, hitRegion{ID: "logs.search", Y: len(lines) - 1, W: width, H: 1})
	} else {
		button("logs.search", "Search retained entries (/): "+clean(m.logs.search.Value()))
	}
	if m.logs.err != "" {
		add("Capture unavailable: "+m.logs.err, errorColor)
	}
	lines = append(lines, "")
	query := strings.ToLower(strings.TrimSpace(m.logs.search.Value()))
	count := 0
	var rows []string
	for _, entry := range m.logs.entries {
		if query != "" && !strings.Contains(strings.ToLower(entry.Level+" "+entry.Message), query) {
			continue
		}
		count++
		levelColor := muted
		switch strings.ToLower(entry.Level) {
		case "error", "fatal", "panic":
			levelColor = errorColor
		case "warn", "warning", "gap":
			levelColor = warning
		case "info":
			levelColor = info
		}
		timestamp := "--:--:--"
		if !entry.Time.IsZero() {
			timestamp = entry.Time.Local().Format("15:04:05")
		}
		prefix := themed(timestamp+" ", muted, bg, false) + themed(strings.ToUpper(entry.Level)+" ", levelColor, bg, false)
		rows = append(rows, strings.Split(wrapText(prefix+themed(entry.Message, text, bg, false), max(1, width)), "\n")...)
	}
	if count == 0 {
		message := "No events captured yet. Start Mihomo or retry the capture."
		if query != "" {
			message = "No retained entries match this search."
		}
		rows = append(rows, strings.Split(wrapText(themed(message, muted, bg, false), max(1, width)), "\n")...)
	}
	height := dashboardGeometry(dashboardOptions{Width: m.width, Height: m.height}).height
	capacity := max(1, height-len(lines)-1)
	tail := max(0, len(rows)-capacity)
	start := max(0, min(tail, tail-max(0, m.logs.offset)))
	end := min(len(rows), start+capacity)
	hits = append(hits, hitRegion{ID: "logs.list", Y: len(lines), W: width, H: max(1, end-start)})
	lines = append(lines, rows[start:end]...)
	if len(rows) > capacity {
		add(fmt.Sprintf("Rows %d–%d / %d · ↑↓ scroll pauses · End resumes", start+1, end, len(rows)), muted)
	}
	return strings.Join(lines, "\n"), hits
}
