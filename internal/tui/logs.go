package tui

import (
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

const (
	retainedLogLimit = 200
	maxLogScrollRows = 1 << 20
)

type logsState struct {
	entries               []mihomo.LogEntry
	search                textinput.Model
	editing, paused       bool
	active                bool
	err                   error
	observedAt            time.Time
	started, reconnecting bool
	recovered             bool
	frozen                []mihomo.LogEntry
	frozenFirst           uint64
	level                 int // local display filter: all, info, warning, error
	compact, holding      bool
	first                 uint64 // identity of the oldest retained entry
	anchor                logPosition
	unread                int
}

func newLogsState(locales ...i18n.Localizer) logsState {
	var locale i18n.Localizer
	if len(locales) > 0 {
		locale = locales[0]
	}
	search := textinput.New()
	search.SetVirtualCursor(false)
	search.CharLimit = 256
	search.Placeholder = locale.T("logs.placeholder")
	styleInput(&search, bg, false)
	return logsState{search: search}
}

func (m *Model) appendLogs(entries []mihomo.LogEntry) {
	if m.logs.holding || m.logs.paused {
		m.logs.unread = min(retainedLogLimit, m.logs.unread+len(entries))
	}
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
		m.logs.first += uint64(len(m.logs.entries) - retainedLogLimit)
		copy(m.logs.entries, m.logs.entries[len(m.logs.entries)-retainedLogLimit:])
		m.logs.entries = m.logs.entries[:retainedLogLimit]
	}
}

// Freeze an independent bounded display snapshot; the live session keeps
// accepting events even when its ring evicts entries shown in this snapshot.
func (m *Model) pauseLogs() {
	if m.logs.paused {
		return
	}
	m.logsScroll(0)
	m.logs.frozen = append([]mihomo.LogEntry(nil), m.logs.entries...)
	m.logs.frozenFirst = m.logs.first
	m.logs.paused = true
}

func (m *Model) resumeLogs() tea.Cmd {
	m.logs.paused, m.logs.frozen = false, nil
	m.resetLogsView()
	return m.syncRuntime()
}

func (m *Model) reconnectLogs() tea.Cmd {
	m.logs.reconnecting, m.logs.recovered, m.logs.err = true, false, nil
	return tea.Batch(m.retryRuntime(), m.refreshCoreStatus())
}

func (m Model) logsDisplayEntries() ([]mihomo.LogEntry, uint64) {
	if m.logs.paused {
		return m.logs.frozen, m.logs.frozenFirst
	}
	return m.logs.entries, m.logs.first
}

func (m *Model) logsActivate(id string) tea.Cmd {
	if id != "logs.search" && id != "logs.reset-search" {
		m.logs.editing = false
		m.logs.search.Blur()
	}
	switch id {
	case "logs.follow":
		if !m.logs.paused {
			m.pauseLogs()
			return nil
		}
		return m.resumeLogs()
	case "logs.retry":
		return m.reconnectLogs()
	case "logs.search":
		m.logs.editing, m.focus, m.scroll = true, "logs.search", 0
		return m.logs.search.Focus()
	case "logs.level":
		m.logs.level = (m.logs.level + 1) % 4
		m.resetLogsView()
	case "logs.view":
		m.logs.compact = !m.logs.compact
	case "logs.clear":
		m.logs.first += uint64(len(m.logs.entries))
		m.logs.entries, m.logs.frozen = nil, nil
		m.logs.frozenFirst = m.logs.first
		m.logs.recovered = false
		m.logs.observedAt = time.Time{}
		m.resetLogsView()
	case "logs.reset-search":
		m.logs.search.Reset()
		m.resetLogsView()
		return m.logsActivate("logs.search")
	case "logs.latest":
		return m.resumeLogs()
	}
	return nil
}

func (m *Model) resetLogsView() {
	m.logs.holding, m.logs.unread, m.scroll = false, 0, 0
}

func (m *Model) logsSearchKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "enter", "esc", "tab", "shift+tab":
		m.logs.editing = false
		m.logs.search.Blur()
		m.focus = "logs.list"
		if k.String() == "tab" {
			m.focus = "logs.search"
			m.cycleFocus(1)
		} else if k.String() == "shift+tab" {
			m.focus = "logs.search"
			m.cycleFocus(-1)
		}
		return nil
	}
	before := m.logs.search.Value()
	var cmd tea.Cmd
	m.logs.search, cmd = m.logs.search.Update(k)
	if before != m.logs.search.Value() {
		m.resetLogsView()
	}
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
	case "l":
		return true, m.logsActivate("logs.level")
	case "v":
		return true, m.logsActivate("logs.view")
	case "ctrl+l":
		return true, m.logsActivate("logs.clear")
	case "space", " ":
		if m.focus == "logs.list" {
			return true, m.logsActivate("logs.follow")
		}
	case "up", "k", "pgup", "home", "down", "j", "pgdown":
		delta := 1
		if k.String() == "pgup" || k.String() == "pgdown" {
			delta = max(1, m.logsCapacity(dashboardBodyWidth(m.dashboardOptions()))-1)
		}
		if k.String() == "home" {
			delta = maxLogScrollRows
		}
		if k.String() == "up" || k.String() == "k" || k.String() == "pgup" || k.String() == "home" {
			delta = -delta
		}
		m.logsScroll(delta)
		return true, nil
	case "end":
		return true, m.logsActivate("logs.latest")
	}
	return false, nil
}

// Browsing pins an entry and its wrapped row without affecting capture,
// so newly appended entries, eviction, resize and layout changes cannot pull the
// reader to the tail. If the anchor expires, the oldest retained row is shown.
func (m *Model) logsScroll(delta int) {
	width := dashboardBodyWidth(m.dashboardOptions())
	doc := m.logDocument(width)
	view := m.logViewport(doc, width, m.logsCapacity(width))
	view.ScrollDown(max(0, min(maxLogScrollRows, delta)))
	view.ScrollUp(max(0, min(maxLogScrollRows, -delta)))
	m.logs.holding = true
	if len(doc.positions) > 0 {
		m.logs.anchor = doc.positions[min(view.YOffset(), len(doc.positions)-1)]
	}
	m.scroll = 0
}
