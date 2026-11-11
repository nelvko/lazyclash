package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) tunAction(enabled bool) tea.Cmd {
	if m.busy || m.quitting {
		return nil
	}
	if !m.tunAvailable() {
		return nil
	}
	if enabled {
		m.form, m.tunFocus = "tun-confirm", "tun.cancel"
		m.failure = ""
		m.input.Blur()
		return nil
	}
	return m.applyTUN(false)
}

func (m *Model) tunAvailable() bool {
	if !m.snapshot.Capabilities.TUN.Supported {
		m.prepareNotification("tun", m.tr("tun.title"))
		m.failure = m.tr("dashboard.control.tunHint")
		return false
	}
	if issue := runtimeControlIssue(m.snapshot, m.locale); issue != "" {
		m.prepareNotification("tun", m.tr("tun.title"))
		m.failure = issue
		return false
	}
	return true
}

func (m *Model) applyTUN(enabled bool) tea.Cmd {
	return m.begin(m.tr("tun.applying"), m.tr("tun.saved"), func(ctx context.Context) error {
		return m.backend.SetTUN(ctx, enabled)
	}, notificationOperation{Key: "tun", Context: m.tr("tun.title")})
}

func (m *Model) tunConfirmationKey(key tea.KeyPressMsg) tea.Cmd {
	prefix := "tun."
	if m.form == "terminal-confirm" {
		prefix = "terminal."
	}
	switch key.String() {
	case "esc":
		return m.tunConfirmationAction(prefix + "cancel")
	case "tab", "shift+tab", "left", "right", "h", "l":
		if m.tunFocus == prefix+"confirm" {
			m.tunFocus = prefix + "cancel"
		} else {
			m.tunFocus = prefix + "confirm"
		}
	case "y", "Y":
		if m.form == "terminal-confirm" {
			return m.tunConfirmationAction(prefix + "confirm")
		}
	case "n", "N":
		if m.form == "terminal-confirm" {
			return m.tunConfirmationAction(prefix + "cancel")
		}
	case "enter":
		return m.tunConfirmationAction(m.tunFocus)
	}
	return nil
}

func (m *Model) tunConfirmationAction(id string) tea.Cmd {
	if m.form == "terminal-confirm" {
		return m.terminalConfirmationAction(id)
	}
	if m.form != "tun-confirm" || m.busy || m.quitting {
		return nil
	}
	if id != "tun.cancel" && id != "tun.confirm" {
		return nil
	}
	if id == "tun.confirm" {
		if _, targets := m.renderTUNConfirmation(""); len(targets) == 0 {
			return nil
		}
	}
	m.form, m.tunFocus = "", ""
	if id == "tun.cancel" {
		return nil
	}
	if !m.tunAvailable() {
		return nil
	}
	return m.applyTUN(true)
}

func (m *Model) renderTUNConfirmation(base string) (string, []hitRegion) {
	width := min(76, m.width)
	if width < 24 || m.height < 9 {
		return overlay("", m.tr("profiles.enlarge"), 0, 0, m.width, m.height), nil
	}
	inner := width - 4
	title, confirm, prefix := m.tr("tun.confirm_title"), m.tr("tun.enable"), "tun."
	details := []string{m.tr("tun.confirm_host"), m.tr("tun.confirm_restart")}
	if m.form == "terminal-confirm" {
		title, confirm, prefix = m.tr("terminal.installTitle"), m.tr("terminal.install"), "terminal."
		details = []string{m.tr("terminal.installDetail", m.snapshot.TerminalProxy.Shell), m.tr("terminal.installNotice")}
	}
	lines := strings.Split(wrapText(themed(title, text, raised, false), inner), "\n")
	lines = append(lines, "")
	for _, detail := range details {
		lines = append(lines, strings.Split(wrapText(themed(detail, warning, raised, false), inner), "\n")...)
	}
	lines = append(lines, "")
	buttonRow := len(lines)
	var targets []hitRegion
	line, column := "", 0
	for _, button := range []struct {
		id, label string
		kind      buttonKind
	}{{prefix + "cancel", m.tr("wizard.cancel"), buttonSecondary}, {prefix + "confirm", confirm, buttonDangerConfirm}} {
		value := renderButton(button.label, buttonOptions{Kind: button.kind, Surface: raised, Focused: m.tunFocus == button.id, Hovered: m.hover == button.id, Plain: m.buttonPlain()})
		buttonWidth := min(inner, ansi.StringWidth(value))
		if column > 0 && column+buttonWidth > inner {
			lines = append(lines, line)
			line, column = "", 0
			buttonRow++
		}
		targets = append(targets, hitRegion{ID: button.id, X: column + 2, Y: buttonRow + 1, W: buttonWidth, H: 1})
		line += cellCrop(value, inner)
		column += buttonWidth
	}
	lines = append(lines, line)
	height := len(lines) + 2
	if height > m.height {
		return overlay("", m.tr("profiles.enlarge"), 0, 0, m.width, m.height), nil
	}
	rows := []string{themed("┌"+strings.Repeat("─", width-2)+"┐", border, raised, false)}
	for _, line := range lines {
		rows = append(rows, themed("│ ", border, raised, false)+themed(cellPad(line, inner), text, raised, false)+themed(" │", border, raised, false))
	}
	rows = append(rows, themed("└"+strings.Repeat("─", width-2)+"┘", border, raised, false))
	x, y := (m.width-width)/2, (m.height-height)/2
	for i := range targets {
		targets[i].X += x
		targets[i].Y += y
	}
	return overlay(base, strings.Join(rows, "\n"), x, y, m.width, m.height), targets
}
