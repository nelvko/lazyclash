package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

// The model and its feedback never retain the URL. Only the terminal's
// clipboard command captures it, after the explicit request has been checked.
type proxyCopyMsg struct {
	id       uint64
	accounts app.ProxyAuthSettings
	write    tea.Cmd
	template bool
	err      error
}

func proxyCopyAccountID(index int) string { return "ports.copyUser." + strconv.Itoa(index) }

func (m *Model) proxyCopyAccounts(d *wizardDocument) {
	for i, name := range m.ports.access.auth.current.Usernames {
		id, y := proxyCopyAccountID(i), len(d.lines)
		color, marker := text, "  "
		if id == m.ports.focus {
			color, marker = focus, "› "
		}
		lines := strings.Split(wrapText(clean(name), d.width-4), "\n")
		for j, line := range lines {
			if j == 0 {
				d.lines = append(d.lines, themed(marker+cellPad(line, d.width-4)+" ›", color, modalBackground, false))
			} else {
				d.lines = append(d.lines, themed("  "+line, color, modalBackground, false))
			}
		}
		d.targets = append(d.targets, hitRegion{ID: id, Y: y, W: d.width, H: len(lines)})
	}
}

func (m *Model) copyProxyAddress() tea.Cmd {
	if !m.pageSelectionMenuAvailable("settings") || !m.snapshot.Installed {
		return nil
	}
	return m.requestProxyCopy(nil, "")
}

func (m *Model) chooseProxyCopyAccount(id string) tea.Cmd {
	index, err := strconv.Atoi(strings.TrimPrefix(id, "ports.copyUser."))
	accounts := m.ports.access.auth.current
	if err != nil || index < 0 || index >= len(accounts.Usernames) {
		return nil
	}
	m.closePorts()
	return m.requestProxyCopy(&accounts, accounts.Usernames[index])
}

func (m *Model) requestProxyCopy(selected *app.ProxyAuthSettings, name string) tea.Cmd {
	ctx := m.newJob("settings.copyProxyAddress", notificationOperation{Key: "settings.copyProxyAddress", Context: m.tr("access.copyAddress")})
	id, backend, locale := m.id, m.backend, m.locale
	return func() tea.Msg {
		reply := proxyCopyMsg{id: id}
		accounts, username := selected, name
		if accounts == nil {
			saved, err := backend.GetProxyAuth(ctx)
			if err != nil || len(saved.Usernames) != 1 {
				reply.accounts, reply.err = saved, err
				return reply
			}
			accounts, username = &saved, saved.Usernames[0]
		}
		address, err := backend.GetProxyAddress(ctx, *accounts, username)
		if err != nil {
			reply.err = err
			return reply
		}
		reply.template = strings.Contains(address, "<LAN ")
		reply.write = tea.SetClipboard(localizedProxyAddress(address, locale))
		return reply
	}
}

func (m *Model) acceptProxyCopy(msg proxyCopyMsg) tea.Cmd {
	if msg.id != m.id || m.job != "settings.copyProxyAddress" {
		return nil
	}
	cancelled := m.opCtx.Err() != nil
	m.abortLookup()
	if m.quitting {
		return tea.Quit
	}
	if cancelled || m.page != "settings" || m.ports.open {
		return nil
	}
	if msg.err != nil {
		m.failure = m.localizeError(msg.err)
		return nil
	}
	if msg.write != nil {
		m.message = m.tr("terminal.activationCopyRequested")
		if msg.template {
			m.message = m.tr("access.copyTemplateRequested")
		}
		return msg.write
	}
	if len(msg.accounts.Usernames) == 0 {
		m.failure = m.tr("access.authEmpty")
		return nil
	}
	p := newPortsState()
	p.kind, p.open, p.step = "copyProxy", true, "choose"
	p.returnFocus, p.focus, p.reveal = "settings.copyProxyAddress", proxyCopyAccountID(0), true
	p.access.auth.current = msg.accounts
	m.ports = p
	return nil
}
