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
	switch key.String() {
	case "esc":
		return m.tunConfirmationAction("tun.cancel")
	case "tab", "shift+tab", "left", "right", "h", "l":
		if m.tunFocus == "tun.confirm" {
			m.tunFocus = "tun.cancel"
		} else {
			m.tunFocus = "tun.confirm"
		}
	case "enter":
		return m.tunConfirmationAction(m.tunFocus)
	}
	return nil
}

func (m *Model) tunConfirmationAction(id string) tea.Cmd {
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
		return m.renderModalNotice(m.tr("profiles.enlarge"))
	}
	inner := width - 2*modalPaddingX
	lines := strings.Split(wrapText(themed(m.tr("tun.confirm_title"), text, modalBackground, false), inner), "\n")
	lines = append(lines, "")
	for _, detail := range []string{m.tr("tun.confirm_host"), m.tr("tun.confirm_restart")} {
		lines = append(lines, strings.Split(wrapText(themed(detail, warning, modalBackground, false), inner), "\n")...)
	}
	lines = append(lines, "")
	buttonRow := len(lines)
	var targets []hitRegion
	line, column := "", 0
	for _, button := range []struct {
		id, label string
		kind      buttonKind
	}{{"tun.cancel", m.tr("wizard.cancel"), buttonSecondary}, {"tun.confirm", m.tr("tun.enable"), buttonDangerConfirm}} {
		value := renderButton(button.label, buttonOptions{Kind: button.kind, Surface: modalBackground, Focused: m.tunFocus == button.id, Hovered: m.hover == button.id, Plain: m.buttonPlain()})
		buttonWidth := min(inner, ansi.StringWidth(value))
		if column > 0 && column+buttonWidth > inner {
			lines = append(lines, line)
			line, column = "", 0
			buttonRow++
		}
		targets = append(targets, hitRegion{ID: button.id, X: column + modalPaddingX, Y: buttonRow + 1, W: buttonWidth, H: 1})
		line += cellCrop(value, inner)
		column += buttonWidth
	}
	lines = append(lines, line)
	height := len(lines) + 2
	if height > m.height {
		return m.renderModalNotice(m.tr("profiles.enlarge"))
	}
	surface := m.newModalSurface(width, height)
	for i, line := range lines {
		surface.put(modalPaddingX, i+1, line)
	}
	for i, target := range targets {
		targets[i] = surface.screen(target)
	}
	return surface.render(base), targets
}
