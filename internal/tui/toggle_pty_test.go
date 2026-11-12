package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

// This parent is a component fixture only: no backend, services or config.
// Its delayed result represents a read of actual state after an operation.
type toggleFixtureResult struct {
	generation uint64
	index      int
	state      toggleState
}

type toggleFixture struct {
	toggles       [2]toggleModel
	generations   [2]uint64
	calls         [2]int
	focus, stale  int
	width, height int
	profile       colorprofile.Profile
	english       bool
}

func newToggleFixture() toggleFixture {
	m := toggleFixture{width: 80, height: 24, profile: colorprofile.TrueColor}
	m.toggles[0].setState(toggleOff, "")
	m.toggles[1].setState(toggleOn, "")
	return m
}

func (m toggleFixture) Init() tea.Cmd { return nil }

func (m toggleFixture) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.ColorProfileMsg:
		m.profile = themeProfile(os.Environ(), msg.Profile)
	case toggleFixtureResult:
		if msg.generation != m.generations[msg.index] {
			m.stale++
		} else {
			m.toggles[msg.index].setState(msg.state, "")
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.focus = (m.focus + 1) % len(m.toggles)
		case "r":
			// An authoritative refresh supersedes an in-flight operation.
			m.generations[m.focus]++
			m.toggles[m.focus].setState(toggleOff, "")
		case "u", "s", "?":
			state := toggleUnavailable
			if msg.String() == "s" {
				state = toggleUnsupported
			} else if msg.String() == "?" {
				state = toggleUnknown
			}
			m.generations[m.focus]++
			m.toggles[m.focus].setState(state, "fixture observation")
		default:
			// Only the parent-selected control sees the activation key.
			intent, ok := m.toggles[m.focus].intent(msg, true)
			if !ok {
				break
			}
			index := m.focus
			m.generations[index]++
			m.calls[index]++
			m.toggles[index].setState(togglePending, "")
			result := toggleFixtureResult{index: index, generation: m.generations[index], state: toggleOff}
			if intent.On {
				result.state = toggleOn
			}
			return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return result })
		}
	}
	return m, nil
}

func (m toggleFixture) View() tea.View {
	labels := toggleLabels{}
	if m.english {
		labels = toggleLabels{On: "On", Off: "Off", Pending: "Switching…", Unavailable: "Unavailable", Unsupported: "Unsupported", Unknown: "Unknown"}
	}
	lines := []string{"LazyClash / Toggle component", ""}
	for _, state := range []toggleState{toggleOn, toggleOff, togglePending, toggleUnavailable, toggleUnsupported, toggleUnknown} {
		toggle := toggleModel{}
		toggle.setState(state, "")
		lines = append(lines, fmt.Sprintf("%d ", state)+renderToggle(toggle, toggleOptions{Labels: labels, Width: max(1, m.width-2), RightAligned: true, Surface: panel}))
	}
	lines = append(lines, "")
	for i, toggle := range m.toggles {
		lines = append(lines, fmt.Sprintf("%d ", i)+renderToggle(toggle, toggleOptions{Labels: labels, Width: max(1, m.width-2), RightAligned: true, Focused: m.focus == i}))
	}
	lines = append(lines,
		fmt.Sprintf("focus=%d calls=%d,%d stale=%d", m.focus, m.calls[0], m.calls[1], m.stale),
		fmt.Sprintf("state=%d,%d last=%d,%d", m.toggles[0].state, m.toggles[1].state, m.toggles[0].lastConfirmed, m.toggles[1].lastConfirmed),
		"Enter/Space toggle; Tab focus; r refresh",
		"u unavailable; s unsupported; ? unknown; q quit")
	rows := make([]string, max(0, m.height))
	for i := range rows {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		rows[i] = themed(cellPad(line, m.width), text, bg, false)
	}
	view := tea.NewView(terminalTheme(strings.Join(rows, "\n"), m.profile))
	view.AltScreen = true
	return view
}

func TestToggleParentRoutesIntentAndRejectsStaleResult(t *testing.T) {
	m := newToggleFixture()
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	m = next.(toggleFixture)
	if cmd == nil || m.calls != [2]int{1, 0} || m.toggles[0].state != togglePending || m.toggles[0].lastConfirmed != toggleOff || m.toggles[1].state != toggleOn {
		t.Fatal("parent did not start a single pending operation on the selected control")
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(toggleFixture)
	if cmd != nil || m.calls != [2]int{1, 0} {
		t.Fatal("pending allowed a duplicate operation")
	}
	generation := m.generations[0]
	next, _ = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = next.(toggleFixture)
	next, _ = m.Update(toggleFixtureResult{index: 0, generation: generation, state: toggleOn})
	m = next.(toggleFixture)
	if m.stale != 1 || m.toggles[0].state != toggleOff || m.toggles[0].lastConfirmed != toggleOff {
		t.Fatal("old result overrode a newer authoritative observation")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(toggleFixture)
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(toggleFixture)
	if cmd == nil || m.calls != [2]int{1, 1} || m.toggles[1].state != togglePending || m.toggles[1].lastConfirmed != toggleOn {
		t.Fatal("focus routing affected the wrong control or confirmed the target early")
	}
	next, _ = m.Update(toggleFixtureResult{index: 1, generation: m.generations[1], state: toggleOff})
	m = next.(toggleFixture)
	if m.toggles[1].state != toggleOff || m.toggles[1].lastConfirmed != toggleOff {
		t.Fatal("fresh authoritative result was not adopted")
	}
}

func TestTogglePTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_TOGGLE_PTY_REPORT")
	if report == "" {
		t.Skip("explicit toggle PTY report path required")
	}
	m := newToggleFixture()
	m.english = os.Getenv("LAZYCLASH_LANG") == "en"
	m.profile = themeProfile(os.Environ(), colorprofile.ANSI256)
	if os.Getenv("COLORTERM") == "truecolor" {
		m.profile = themeProfile(os.Environ(), colorprofile.TrueColor)
	}
	result, err := tea.NewProgram(m, tea.WithoutSignalHandler()).Run()
	if err != nil {
		t.Fatal(err)
	}
	final := result.(toggleFixture)
	raw, err := json.Marshal(map[string]any{"calls": final.calls, "stale": final.stale, "states": [2]toggleState{final.toggles[0].state, final.toggles[1].state}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
