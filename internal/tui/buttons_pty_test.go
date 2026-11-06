package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// TestButtonsPTYFixture is an opt-in terminal harness for the actual shared
// button view. It uses no backend, files other than the report, or services.
func TestButtonsPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_BUTTONS_PTY_REPORT")
	if report == "" {
		t.Skip("explicit button PTY report path required")
	}
	m := buttonsPTYModel{width: 120, height: 32, selected: "global", focus: "global", profile: colorprofile.ANSI256, actions: map[string]int{}}
	if os.Getenv("COLORTERM") == "truecolor" {
		m.profile = colorprofile.TrueColor
	}
	m.profile = themeProfile(os.Environ(), m.profile)
	m.chinese = os.Getenv("LAZYCLASH_BUTTONS_PTY_LANGUAGE") == "zh-Hans"
	result, err := tea.NewProgram(m, tea.WithoutSignalHandler()).Run()
	if err != nil {
		t.Fatal(err)
	}
	final := result.(buttonsPTYModel)
	raw, err := json.Marshal(map[string]any{"selected": final.selected, "actions": final.actions})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

type buttonsPTYModel struct {
	width, height           int
	selected, focus, hover  string
	busy, disabled, chinese bool
	profile                 colorprofile.Profile
	actions                 map[string]int
}

func (m buttonsPTYModel) Init() tea.Cmd { return nil }

func (m buttonsPTYModel) labels() []string {
	if m.chinese {
		return []string{"规则", "全局", "直连", "应用设置", "重启", "删除配置", "确认删除", "查看详情", "不可操作"}
	}
	return []string{"Rule", "Global", "Direct", "Apply settings", "Restart", "Delete profile", "Delete forever", "Details", "Unavailable"}
}

var buttonsPTYIDs = []string{"rule", "global", "direct", "apply", "restart", "delete", "confirm", "details", "unavailable"}

func (m buttonsPTYModel) layout() ([]string, []hitRegion) {
	lines := []string{"LazyClash / buttons", ""}
	labels := m.labels()
	modeWidth := 6
	for _, label := range labels[:3] {
		modeWidth = max(modeWidth, ansi.StringWidth(label))
	}
	var targets []hitRegion
	row, x := "", 0
	for index, id := range buttonsPTYIDs {
		if index == 3 {
			lines = append(lines, row, "", "Actions")
			row, x = "", 0
		}
		o := buttonOptions{Surface: panel, Focused: m.focus == id, Hovered: m.hover == id,
			Disabled: m.disabled || id == "unavailable", Busy: m.busy,
			Plain: m.profile <= colorprofile.ANSI}
		if index < 3 {
			o.Selectable, o.Selected, o.Width = true, id == m.selected, modeWidth
		}
		switch id {
		case "apply":
			o.Kind = buttonPrimary
		case "delete":
			o.Kind = buttonDanger
		case "confirm":
			o.Kind = buttonDangerConfirm
		case "details":
			o.Kind = buttonText
		}
		button := renderButton(labels[index], o)
		width := ansi.StringWidth(button)
		if x > 0 && x+width > m.width {
			lines = append(lines, row)
			row, x = "", 0
		}
		targets = append(targets, hitRegion{ID: id, X: x, Y: len(lines), W: min(width, m.width), H: 1})
		row += cellCrop(button, m.width)
		x += width
	}
	lines = append(lines, row, "", fmt.Sprintf("selected=%s focus=%s", m.selected, m.focus),
		fmt.Sprintf("hover=%s busy=%t disabled=%t", m.hover, m.busy, m.disabled),
		"q quit | b busy | d disable")
	return lines, targets
}

func (m buttonsPTYModel) activate(id string) buttonsPTYModel {
	if m.busy || m.disabled || id == "unavailable" {
		return m
	}
	m.focus = id
	if id == "rule" || id == "global" || id == "direct" {
		m.selected = id
	} else {
		m.actions[id]++
	}
	return m
}

func (m buttonsPTYModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
	case tea.ColorProfileMsg:
		m.profile = themeProfile(os.Environ(), v.Profile)
	case tea.KeyPressMsg:
		switch v.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "b":
			m.busy = !m.busy
		case "d":
			m.disabled = !m.disabled
		case "enter", "space":
			m = m.activate(m.focus)
		case "tab", "right", "shift+tab", "left":
			for i, id := range buttonsPTYIDs[:8] {
				if id == m.focus {
					delta := 1
					if v.String() == "shift+tab" || v.String() == "left" {
						delta = -1
					}
					m.focus = buttonsPTYIDs[(i+delta+8)%8]
					break
				}
			}
		}
	case tea.MouseMsg:
		_, targets := m.layout()
		point := v.Mouse()
		id := ""
		for _, target := range targets {
			if point.X >= target.X && point.X < target.X+target.W && point.Y == target.Y {
				id = target.ID
				break
			}
		}
		if _, motion := v.(tea.MouseMotionMsg); motion {
			m.hover = id
		}
		if _, click := v.(tea.MouseClickMsg); click && point.Button == tea.MouseLeft && id != "" {
			m = m.activate(id)
		}
	}
	return m, nil
}

func (m buttonsPTYModel) View() tea.View {
	lines, _ := m.layout()
	for i := range lines {
		lines[i] = themed(cellPad(lines[i], m.width), text, panel, false)
	}
	for len(lines) < m.height {
		lines = append(lines, themed(strings.Repeat(" ", m.width), text, panel, false))
	}
	view := tea.NewView(terminalTheme(strings.Join(lines[:min(len(lines), m.height)], "\n"), m.profile))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeAllMotion
	return view
}
