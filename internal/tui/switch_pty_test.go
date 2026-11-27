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
	"github.com/charmbracelet/x/ansi"
)

// The fixture backend returns an observation after a delay. A failed operation
// returns the unchanged observation, and an external refresh supersedes it.
type switchObservation struct {
	index, generation int
	on, failed        bool
}

type switchFixture struct {
	controls         [2]Switch
	generation       [2]int
	calls            [2]int
	focus, stale     int
	failures         int
	failNext         bool
	width, height    int
	profile          colorprofile.Profile
	pending, unknown string
}

func newSwitchFixture(english bool) switchFixture {
	m := switchFixture{width: 80, height: 24, profile: colorprofile.TrueColor, pending: "切换中…", unknown: "状态未知"}
	m.controls[1] = Switch{Variant: Status, On: true, OnLabel: "运行中", OffLabel: "已停止"}
	if english {
		m.controls[1].OnLabel, m.controls[1].OffLabel = "Running", "Stopped"
		m.pending, m.unknown = "Switching…", "Unknown"
	}
	return m
}

func (m switchFixture) Init() tea.Cmd { return nil }

func (m *switchFixture) request(index int, desired bool) tea.Cmd {
	control := &m.controls[index]
	m.focus = index
	m.calls[index]++
	m.generation[index]++
	result := switchObservation{index, m.generation[index], desired, m.failNext}
	if m.failNext {
		result.on = control.On
	}
	m.failNext = false
	control.Pending, control.Message = true, m.pending
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return result })
}

func (m switchFixture) face(index int) string {
	return m.controls[index].View(switchOptions{Surface: panel, Focused: index == m.focus, Plain: m.profile == colorprofile.ASCII})
}

const switchFixtureFirstRow = 2

func (m switchFixture) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.ColorProfileMsg:
		m.profile = themeProfile(os.Environ(), msg.Profile)
	case switchObservation:
		if msg.generation != m.generation[msg.index] {
			m.stale++
			break
		}
		control := &m.controls[msg.index]
		control.On, control.Pending, control.Message = msg.on, false, ""
		if msg.failed {
			m.failures++
		}
	case tea.KeyPressMsg:
		control := &m.controls[m.focus]
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.focus = (m.focus + 1) % len(m.controls)
		case "f":
			m.failNext = true
		case "r", "?":
			m.generation[m.focus]++
			control.On, control.Pending = false, false
			control.Disabled = msg.String() == "?"
			control.Message = ""
			if control.Disabled {
				control.Message = m.unknown
			}
		default:
			if on, ok := control.KeyIntent(msg, true); ok {
				cmd := m.request(m.focus, on)
				return m, cmd
			}
		}
	case tea.MouseClickMsg:
		index := msg.Y - switchFixtureFirstRow
		if msg.Button == tea.MouseLeft && index >= 0 && index < len(m.controls) && msg.X >= m.width-ansi.StringWidth(m.face(index)) && msg.X < m.width {
			if on, ok := m.controls[index].Intent(); ok {
				cmd := m.request(index, on)
				return m, cmd
			}
		}
	}
	return m, nil
}

func (m switchFixture) View() tea.View {
	lines := []string{"LazyClash / Switch", ""}
	for index, label := range []string{"Slider", "Status"} {
		face := m.face(index)
		lines = append(lines, cellPad(label, max(0, m.width-ansi.StringWidth(face)))+face)
	}
	lines = append(lines, "",
		fmt.Sprintf("focus=%d calls=%d,%d", m.focus, m.calls[0], m.calls[1]),
		fmt.Sprintf("actual=%t,%t", m.controls[0].On, m.controls[1].On),
		fmt.Sprintf("pending=%t,%t", m.controls[0].Pending, m.controls[1].Pending),
		fmt.Sprintf("stale=%d failures=%d", m.stale, m.failures))
	rows := make([]string, m.height)
	for i := range rows {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		rows[i] = themed(cellPad(line, m.width), text, bg, false)
	}
	view := tea.NewView(terminalTheme(strings.Join(rows, "\n"), m.profile))
	view.AltScreen, view.MouseMode = true, tea.MouseModeAllMotion
	return view
}

func TestSwitchPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_SWITCH_PTY_REPORT")
	if report == "" {
		t.Skip("explicit switch PTY report path required")
	}
	m := newSwitchFixture(os.Getenv("LAZYCLASH_LANG") == "en")
	result, err := tea.NewProgram(m, tea.WithoutSignalHandler()).Run()
	if err != nil {
		t.Fatal(err)
	}
	final := result.(switchFixture)
	raw, err := json.Marshal(map[string]any{"calls": final.calls, "stale": final.stale, "failures": final.failures})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
