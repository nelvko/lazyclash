package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
)

type portsState struct {
	open, cancelling, applied bool
	kind                      string
	access                    accessDraft
	tunOriginal, tunEnabled   bool
	step, focus, returnFocus  string
	controls                  []string
	mixed                     textinput.Model
	current                   app.RuntimeSettings
	plan                      app.RuntimeSettingsPlan
	result                    app.RuntimeSettingsResult
	err, refreshError         string
	viewport                  viewport.Model
	reveal                    bool
}

type portsMsg struct {
	id           uint64
	kind         string
	settings     app.RuntimeSettings
	addresses    []app.BindAddressOption
	auth         app.ProxyAuthSettings
	plan         app.RuntimeSettingsPlan
	result       app.RuntimeSettingsResult
	snapshot     app.Snapshot
	hasSnapshot  bool
	err          error
	refreshError error
}

func newPortsState() portsState {
	input := textinput.New()
	input.SetVirtualCursor(false)
	input.CharLimit = 16
	styleInput(&input, inputBackground, false)
	view := viewport.New()
	view.SoftWrap = false
	return portsState{mixed: input, focus: "ports.mixed", viewport: view}
}

func (m *Model) openNetworkConfig(kind string) tea.Cmd {
	if !m.snapshot.Installed {
		m.failure = m.tr("ports.require_install")
		return nil
	}
	p := newPortsState()
	p.kind, p.open, p.returnFocus, p.reveal = kind, true, m.focus, true
	if kind == "tun" {
		p.step, p.focus = "edit", "ports.cancel"
		p.tunOriginal, p.tunEnabled = m.snapshot.TUN, m.snapshot.TUN
		if m.canConfigureHostTUN() {
			p.focus = "tun.policy"
		}
		m.ports = p
		return nil
	}
	m.ports = p
	return m.loadPorts()
}

func (m *Model) loadPorts() tea.Cmd {
	ctx := m.newJob("ports-load")
	id, backend, kind := m.id, m.backend, m.ports.kind
	m.ports.step, m.ports.err, m.ports.cancelling = "loading", "", false
	m.message = m.tr("ports.loading_message")
	return tea.Batch(func() tea.Msg {
		if kind == "auth" {
			users, err := backend.GetProxyAuth(ctx)
			return portsMsg{id: id, kind: "load", auth: users, err: err}
		}
		s, err := backend.GetRuntimeSettings(ctx)
		result := portsMsg{id: id, kind: "load", settings: s, err: err}
		if kind == "bind" && err == nil {
			result.addresses = backend.BindAddressOptions(ctx)
		}
		return result
	}, m.spinner.Tick)
}

func (m *Model) terminalPortProposal() (app.RuntimeSettings, bool) {
	p := &m.ports
	mixed, err := strconv.Atoi(strings.TrimSpace(p.mixed.Value()))
	if err != nil || mixed < 1 || mixed > 65535 {
		return m.invalidPortField("ports.mixed", m.tr("ports.validation.range", m.tr("ports.proxy_port")))
	}
	s := p.current
	s.MixedPort = mixed
	endpoints := s.Endpoints(accessControllerAddress(s), mixed)
	for i, ep := range endpoints {
		for _, other := range endpoints[:i] {
			if ep.Conflicts(other) {
				return m.invalidPortField("ports.mixed", m.tr("ports.validation.distinct"))
			}
		}
	}
	p.mixed.Err = nil
	return s, true
}

func (m *Model) preparePorts() tea.Cmd {
	p := &m.ports
	backend := m.backend
	if p.kind == "auth" {
		prefixes, err := m.proxyAuthPrefixes()
		if err != nil {
			p.access.sections[proxyAuthBypass] = true
			p.focus, p.reveal = proxyAuthPrefixes, true
			return nil
		}
		if !m.proxyAuthChanged(prefixes) {
			return nil
		}
		users, valid := m.proxyAuthUsers()
		if !valid {
			return nil
		}
		original := p.access.auth.current
		return m.checkPorts(func(ctx context.Context) (app.RuntimeSettingsPlan, error) {
			return backend.PrepareProxyAuthRules(ctx, original, users, prefixes)
		})
	}
	var settings app.RuntimeSettings
	var valid bool
	if p.accessEditor() {
		settings, valid = m.accessProposal()
	} else {
		settings, valid = m.terminalPortProposal()
	}
	if !valid {
		return nil
	}
	p.normalizeProxyZero()
	original := p.current
	terminalOnly, locale := p.kind == "terminal", m.locale
	return m.checkPorts(func(ctx context.Context) (app.RuntimeSettingsPlan, error) {
		plan, err := backend.PrepareRuntimeSettingsEdit(ctx, original, settings)
		// The terminal editor owns only the mixed port. Never let a concurrent
		// controller-port change turn the hidden, loaded value into a proposal.
		if err == nil && terminalOnly && plan.Current.ControllerPort != plan.Proposed.ControllerPort {
			err = errors.New(locale.T("network.terminalConfig.changed"))
		}
		return plan, err
	})
}

func (m *Model) checkPorts(prepare func(context.Context) (app.RuntimeSettingsPlan, error)) tea.Cmd {
	ctx := m.newJob("ports-prepare")
	id, p := m.id, &m.ports
	p.err, p.cancelling = "", false
	p.step, p.focus = "preparing", "ports.cancel"
	p.viewport.GotoTop()
	m.message = m.tr("ports.checking_message")
	return tea.Batch(func() tea.Msg {
		plan, err := prepare(ctx)
		return portsMsg{id: id, kind: "prepare", plan: plan, err: err}
	}, m.spinner.Tick)
}

func (m *Model) applyPorts() tea.Cmd {
	if m.busy || m.ports.step != "review" {
		return nil
	}
	ctx := m.newJob("ports-apply")
	id, backend, plan := m.id, m.backend, m.ports.plan
	m.ports.step, m.ports.err, m.ports.cancelling = "applying", "", false
	m.ports.refreshError, m.ports.applied = "", false
	m.ports.focus = "ports.cancel"
	m.message = m.tr("ports.applying_message")
	return tea.Batch(func() tea.Msg {
		result, err := backend.ApplyRuntimeSettings(ctx, plan)
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		snapshot, refreshError := backend.Snapshot(refreshCtx)
		return portsMsg{id: id, kind: "apply", result: result, snapshot: snapshot, hasSnapshot: refreshError == nil, err: err, refreshError: refreshError}
	}, m.spinner.Tick)
}

func (m *Model) acceptPorts(msg portsMsg) {
	if msg.id != m.id || !m.ports.open {
		return
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.busy, m.job = false, ""
	m.message = ""
	p := &m.ports
	p.cancelling, p.reveal = false, true
	p.err = ""
	if msg.err != nil {
		p.err = clean(m.localizeError(msg.err))
	}
	if msg.hasSnapshot {
		m.adoptSnapshot(msg.snapshot)
	}
	switch msg.kind {
	case "load":
		p.step, p.focus = "edit", p.firstField()
		if msg.err == nil && p.kind == "auth" {
			m.initProxyAuthDraft(msg.auth)
		} else if msg.err == nil {
			p.current = msg.settings
			p.mixed.SetValue(strconv.Itoa(msg.settings.MixedPort))
			if p.accessEditor() {
				m.initAccessDraft()
			}
			if p.kind == "bind" {
				m.initBindMenu(msg.addresses)
			}
		} else {
			p.step, p.focus = "load-error", "ports.retry"
		}
		for _, area := range p.access.areas {
			if area.Err != nil {
				p.step, p.focus, p.err = "load-error", "ports.retry", m.tr("access.listTooLong")
			}
		}
	case "prepare":
		p.step, p.focus = "edit", p.firstField()
		if msg.err != nil {
			m.revealControlError(msg.err)
			m.revealProxyBindError(msg.err)
		}
		if msg.err == nil && p.accessEditor() && !msg.plan.Changed {
			p.err = m.tr("access.unchanged")
		} else if msg.err == nil {
			p.plan, p.step, p.focus = msg.plan, "review", "ports.apply"
			p.viewport.GotoTop()
		}
	case "apply":
		p.result, p.step, p.focus = msg.result, "result", "ports.done"
		// Changed is set after the transaction commits. Endpoint publication
		// and the dashboard observation can still fail independently.
		p.applied = msg.err == nil || msg.result.Changed
		if p.applied {
			p.current = msg.result.Settings
			if p.accessEditor() {
				p.access = accessDraft{}
				p.plan = app.RuntimeSettingsPlan{}
			}
			m.message = m.tr("ports.applied")
		} else {
			m.message = m.tr("ports.finished")
		}
		if msg.refreshError != nil {
			p.refreshError = m.tr("ports.refresh_failed") + " " + m.localizeError(msg.refreshError)
		}
		if !msg.hasSnapshot && m.snapshotReady {
			m.markOperationStateUnknown()
		}
		m.notification.operation.Context = m.tr("notification.ports")
		switch p.kind {
		case "auth":
			m.notification.operation.Context = m.tr("settings.proxyAuth")
		case "bind", "rules":
			m.notification.operation.Context = m.tr("access." + p.kind + "Title")
		}
		m.notification.critical = diagnostic.RequiresAttention(msg.err) || msg.refreshError != nil || msg.result.Changed && msg.err != nil
		if (m.notification.critical || m.notification.retainDialog) && (msg.err != nil || msg.refreshError != nil) {
			var summary []string
			if p.applied {
				summary = append(summary, m.message)
			}
			if msg.err != nil {
				reason := p.err
				if p.applied {
					reason = m.tr("ports.endpoint_failed")
				}
				summary = append(summary, reason)
			}
			if msg.refreshError != nil {
				summary = append(summary, m.tr("notification.state_unknown"))
			}
			m.failure = strings.Join(summary, " · ")
			m.notification.details = m.locale.Error(errors.Join(msg.err, msg.refreshError))
		}
		if !p.applied && !m.notification.critical {
			m.revealProxyBindError(msg.err)
		}
	}
}

func (m *Model) closePorts() {
	if m.busy {
		m.cancelTask(false)
		return
	}
	m.ports.open = false
	m.ports.access = accessDraft{}
	m.ports.plan = app.RuntimeSettingsPlan{}
	m.focus = m.ports.returnFocus
	if !m.snapshot.Installed {
		m.focus = "install"
	} else if m.ports.kind == "activation" {
		m.syncNetworkActionFocus()
	}
	if m.ports.step == "loading" || m.ports.step == "preparing" {
		m.message = ""
	}
}

func (m *Model) portsActivate(id string) tea.Cmd {
	if handled, cmd := m.clearInputField(id); handled {
		return cmd
	}
	if m.ports.cancelling && id == "ports.cancel" {
		return nil
	}
	if m.busy {
		if id == "ports.cancel" {
			m.cancelTask(false)
		}
		return nil
	}
	if m.ports.kind == "copyProxy" && strings.HasPrefix(id, "ports.copyUser.") {
		return m.chooseProxyCopyAccount(id)
	}
	if m.ports.kind == "tun" && strings.HasPrefix(id, "tun.") {
		return m.networkTUNActivate(id)
	}
	if m.ports.accessEditor() && m.ports.step == "edit" {
		p := &m.ports
		if p.kind == "auth" && m.activateProxyAuth(id) {
			return nil
		}
		if p.kind == "bind" && m.activateBindControl(id) {
			return textinput.Blink
		}
		switch id {
		case "ports.tlsSection", "ports.corsSection", "ports.advancedSection":
			p.access.sections[id] = !p.access.sections[id]
			p.focus, p.reveal = id, true
			return nil
		case "ports.private":
			p.access.private = !p.access.private
			p.focus, p.reveal, p.err = id, true, ""
			return nil
		case "ports.rulesReset":
			m.resetNetworkRules()
			return nil
		}
		if area := p.textarea(id); area != nil {
			p.focus, p.reveal = id, true
			return area.Focus()
		}
		if p.field(id) != nil {
			p.focus, p.reveal = id, true
			return textinput.Blink
		}
	}
	switch id {
	case "terminal.copyActivation":
		if command := pendingActivationCommand(m.snapshot.TerminalProxy); m.ports.kind == "activation" && command != "" {
			m.ports.step = "copy-requested"
			return tea.SetClipboard(command)
		}
	case "ports.cancel", "ports.done":
		m.closePorts()
	case "ports.review":
		return m.preparePorts()
	case "ports.apply":
		return m.applyPorts()
	case "ports.back":
		m.ports.step, m.ports.focus, m.ports.reveal = "edit", m.ports.firstField(), true
		m.ports.viewport.GotoTop()
	case "ports.retry":
		if m.ports.step == "load-error" {
			return m.loadPorts()
		}
		m.ports.step = "edit"
		return m.preparePorts()
	case "ports.mixed":
		m.ports.focus, m.ports.reveal = id, true
		return textinput.Blink
	}
	return nil
}

func (m *Model) updatePortsInput(msg tea.Msg) tea.Cmd {
	if m.busy || m.ports.step != "edit" {
		return nil
	}
	if area := m.ports.textarea(m.ports.focus); area != nil {
		p := &m.ports
		// Bubbles clamps pasted text to 10,000 lines. Decline the whole
		// paste instead of applying a silently truncated access policy.
		if paste, ok := msg.(tea.PasteMsg); ok && area.LineCount()-strings.Count(area.SelectedText(), "\n")+strings.Count(paste.Content, "\n") > 10000 {
			p.err = m.tr("access.listTooLong")
			return nil
		}
		before := area.Value()
		var cmd tea.Cmd
		*area, cmd = area.Update(msg)
		if area.Value() != before {
			p.err, p.reveal = "", true
		}
		return cmd
	}
	input := m.ports.field(m.ports.focus)
	if input == nil {
		return nil
	}
	before := input.Value()
	position := input.Position()
	var cmd tea.Cmd
	*input, cmd = input.Update(msg)
	// The wildcard is a complete address. Typing or pasting into it starts
	// a new address, including when Bubbles delivers a clipboard paste.
	if m.ports.focus == bindAddressID && before == "*" && len(input.Value()) > 1 {
		if position == 0 {
			input.SetValue(strings.TrimSuffix(input.Value(), "*"))
		} else {
			input.SetValue(strings.TrimPrefix(input.Value(), "*"))
		}
		input.CursorEnd()
	}
	if input.Value() != before {
		if m.ports.focus == bindAddressID {
			m.ports.access.bindMenu.dismiss()
		}
		input.Err, m.ports.err, m.ports.plan = nil, "", app.RuntimeSettingsPlan{}
		if m.ports.focus == m.ports.access.bindField {
			m.ports.access.bindField, m.ports.access.bindMessage = "", ""
		}
		m.ports.reveal = true
	}
	return cmd
}

func (p *portsState) editingProxyPorts() bool {
	return p.open && p.kind == "proxy" && p.step == "edit"
}

func (m *Model) portsKey(k tea.KeyPressMsg) tea.Cmd {
	if m.ports.editingProxyPorts() && m.tooltip.key(k.String(), m.ports.focus) {
		if m.tooltip.open != "" && m.tooltip.open == m.tooltip.offscreen.ID {
			m.ports.reveal = true
		}
		return nil
	}
	p := &m.ports
	if m.busy {
		if k.String() == "esc" {
			m.cancelTask(false)
		}
		return nil
	}
	key := k.String()
	if p.kind != "" && key != "esc" && len(p.controls) == 0 {
		return nil
	}
	if p.kind == "bind" && p.step == "edit" && m.bindMenuKey(key) {
		return nil
	}
	if p.textarea(p.focus) != nil && p.step == "edit" && (key == "enter" || key == "up" || key == "down") {
		return m.updatePortsInput(k)
	}
	if p.kind != "" {
		switch key {
		case "down":
			key = "tab"
		case "up":
			key = "shift+tab"
		case "space", " ", "left", "right":
			if p.focus == "tun.policy" || p.focus == "ports.private" || strings.HasSuffix(p.focus, "Section") {
				return m.portsActivate(p.focus)
			}
		}
	}
	switch key {
	case "esc":
		if p.step == "review" && !p.accessEditor() {
			return m.portsActivate("ports.back")
		}
		m.closePorts()
	case "tab", "shift+tab":
		p.normalizeProxyZero()
		index := 0
		for i, id := range p.controls {
			if id == p.focus {
				index = i
			}
		}
		if len(p.controls) > 0 {
			delta := 1
			if key == "shift+tab" {
				delta = -1
			}
			p.focus = p.controls[(index+delta+len(p.controls))%len(p.controls)]
			p.reveal = true
		}
	case "enter":
		if p.step == "edit" && p.field(p.focus) != nil {
			return m.preparePorts()
		}
		return m.portsActivate(p.focus)
	case "pgup", "pgdown":
		p.viewport, _ = p.viewport.Update(k)
	default:
		return m.updatePortsInput(k)
	}
	return nil
}

func (m *Model) portsMouse(event tea.MouseMsg) tea.Cmd {
	k := event.Mouse()
	if _, wheel := event.(tea.MouseWheelMsg); wheel {
		if menu := &m.ports.access.bindMenu; menu.open && (k.Button == tea.MouseWheelUp || k.Button == tea.MouseWheelDown) {
			if k.Button == tea.MouseWheelUp {
				menu.move(-1)
			} else {
				menu.move(1)
			}
			m.ports.reveal = true
			return nil
		}
		m.ports.viewport, _ = m.ports.viewport.Update(event)
		return nil
	}
	m.hover = ""
	for _, hit := range m.targets {
		if k.X < hit.X || k.X >= hit.X+hit.W || k.Y < hit.Y || k.Y >= hit.Y+hit.H {
			continue
		}
		m.hover = hit.ID
		if hit.tip.text != "" {
			return nil
		}
		if _, clicked := event.(tea.MouseClickMsg); clicked && k.Button == tea.MouseLeft {
			if m.ports.access.bindMenu.open && hit.ID != bindAddressID && hit.ID != bindMenuID && !strings.HasPrefix(hit.ID, bindOptionPrefix) {
				m.ports.access.bindMenu.dismiss()
			}
			if hit.ID != m.ports.focus {
				m.ports.normalizeProxyZero()
			}
			if handled, cmd := m.clearInputField(hit.ID); handled {
				return cmd
			}
			m.ports.focus = hit.ID
			return m.portsActivate(hit.ID)
		}
		break
	}
	return nil
}

func (m *Model) portsDocument(width int) (wizardDocument, []wizardButton, string) {
	p := &m.ports
	if p.kind == "tun" {
		return m.networkTUNDocument(width)
	}
	d := wizardDocument{locale: m.locale, width: width, hover: m.hover, plain: m.buttonPlain(), busy: m.busy}
	if p.kind == "activation" {
		buttons := m.terminalActivationDocument(&d)
		return d, buttons, ""
	}
	buttons := []wizardButton{{"ports.cancel", m.tr("ports.cancel")}}
	if p.kind == "copyProxy" {
		m.proxyCopyAccounts(&d)
		return d, buttons, ""
	}
	subtitle := m.tr("ports.edit")
	switch p.step {
	case "loading":
		subtitle = m.tr("ports.loading")
		d.line(m.spinner.View() + m.tr("ports.loading_current"))
	case "preparing":
		subtitle = m.tr("ports.checking")
		d.label(m.tr("ports.checking_title"))
		if p.accessEditor() {
			d.line(m.spinner.View() + m.tr("ports.checking_detail"))
			break
		}
		d.pair(m.tr("ports.proxy_port"), p.mixed.Value())
		d.line(m.spinner.View() + m.tr("ports.checking_detail"))
		d.line(m.tr("ports.review_boundary"))
	case "edit":
		if p.accessEditor() {
			d.hover = ""
			if p.kind == "proxy" {
				_, reviewable := m.proxyPortProposal()
				d.disabled = func(id string) bool { return id == "ports.review" && !reviewable }
			}
			m.accessEdit(&d)
			if p.kind == "rules" {
				buttons = append([]wizardButton{{"ports.rulesReset", m.tr("access.rulesReset")}}, buttons...)
			}
			buttons = append(buttons, wizardButton{"ports.review", m.tr("ports.review")})
			break
		}
		m.terminalConfigFields(&d)
		buttons = append(buttons, wizardButton{"ports.review", m.tr("network.terminalConfig.save")})
	case "review":
		subtitle = m.tr("ports.review")
		if p.accessEditor() {
			d.line(themed(m.tr("ports.reviewed"), accent, modalBackground, false))
			m.accessReview(&d)
		} else {
			d.label(m.tr("ports.reviewed"))
			d.pair(m.tr("ports.proxy_port"), fmt.Sprintf("%d → %d", p.plan.Current.MixedPort, p.plan.Proposed.MixedPort))
		}
		d.space()
		label := m.tr("ports.apply")
		if !p.plan.Changed {
			d.line(m.tr("ports.unchanged"))
			label = m.tr("ports.keep_current")
		} else if p.plan.Active {
			d.line(themed(m.tr("ports.restart_warning"), warning, modalBackground, false))
			d.line(m.tr("ports.restart_boundary"))
			label = m.tr("ports.apply_restart")
		} else {
			d.line(m.tr("ports.stopped_boundary"))
		}
		d.space()
		buttons = []wizardButton{{"ports.back", m.tr("ports.back")}}
		if p.accessEditor() {
			buttons = append(buttons, wizardButton{"ports.cancel", m.tr("ports.cancel")})
		} else {
			d.line(m.tr("ports.preserved"))
		}
		buttons = append(buttons, wizardButton{"ports.apply", label})
	case "applying":
		subtitle = m.tr("ports.applying")
		d.label(m.tr("ports.applying_title"))
		if p.cancelling {
			d.line(themed(m.tr("ports.restoring"), warning, modalBackground, false))
		} else {
			d.line(m.spinner.View() + m.tr("ports.updating"))
		}
		d.line(m.tr("ports.cancel_boundary"))
	case "result":
		subtitle = m.tr("ports.result")
		buttons = []wizardButton{{"ports.done", m.tr("ports.done")}}
		if p.applied {
			d.line(themed(m.tr("ports.applied"), success, modalBackground, false))
			if !p.accessEditor() {
				d.pair(m.tr("ports.proxy_port"), strconv.Itoa(p.result.Settings.MixedPort))
			}
			if p.result.Restarted {
				d.line(m.tr("ports.restarted"))
			} else if !m.snapshot.Service.Active && p.refreshError == "" {
				d.line(m.tr("ports.stopped"))
			} else {
				d.line(m.tr("ports.no_restart"))
			}
		} else {
			d.line(themed(m.tr("ports.incomplete"), errorColor, modalBackground, false))
			d.line(m.tr("ports.retry_boundary"))
			buttons = append(buttons, wizardButton{"ports.retry", m.tr("ports.review_retry")})
		}
		if p.refreshError != "" {
			d.line(themed(p.refreshError, warning, modalBackground, false))
		}
	case "load-error":
		subtitle = m.tr("ports.unavailable")
		d.line(m.tr("ports.settings_unavailable"))
		buttons = append(buttons, wizardButton{"ports.retry", m.tr("ports.retry")})
	}
	if p.cancelling && p.step != "applying" {
		d.line(themed(m.tr("ports.cancelling"), warning, modalBackground, false))
	}
	return d, buttons, subtitle
}

// If Tab would have reached the zero's clear button, that control disappears
// along with the value. Skip it in this same navigation event.
func (p *portsState) normalizeProxyZero() {
	if p.kind != "proxy" || p.step != "edit" {
		return
	}
	if input := p.field(p.focus); input != nil {
		raw := strings.TrimSpace(input.Value())
		if raw != "" && strings.Trim(raw, "0") == "" {
			input.SetValue("")
			p.controls = slices.DeleteFunc(p.controls, func(id string) bool { return id == p.focus+".clear" })
		}
	}
}

func (m *Model) renderPorts(base string) (string, []hitRegion) {
	p := &m.ports
	w, h := m.width, m.height
	modal := m.canFloatDocument()
	access := p.accessEditor() || p.kind == "copyProxy"
	if p.kind != "" {
		// Network configurations float over the entire dashboard, including
		// the sidebar. Even the narrow layout keeps a visible gutter.
		modal = true
		preferredWidth := min(66, max(34, w*3/5))
		if access {
			preferredWidth = 78
			if p.kind == "copyProxy" {
				preferredWidth = 48
			}
			if p.kind == "proxy" || p.kind == "rules" {
				preferredWidth = 66
			}
			if p.kind == "bind" {
				preferredWidth = p.bindDialogWidth()
			}
		}
		if p.kind == "activation" {
			command := pendingActivationCommand(m.snapshot.TerminalProxy)
			preferredWidth = max(preferredWidth, min(80, ansi.StringWidth(clean(command))+2*modalBodyInset))
		}
		w = min(preferredWidth, max(0, w-4))
		h -= 2
	} else if modal {
		w, h = min(78, w-12), h-4
	}
	if w < 34 || h < 10 {
		p.controls = nil
		label := m.tr("ports.compact_help")
		if m.busy {
			label = m.tr("ports.compact_busy")
		}
		if p.kind == "" {
			return m.renderModalNotice(label)
		}
		label = cellCrop(label, max(1, m.width-2))
		surface := m.newModalSurface(ansi.StringWidth(label), 1)
		surface.put(0, 0, label)
		return surface.render(base), nil
	}
	inset := modalBodyInset
	if p.kind == "proxy" && w < 54 {
		inset = modalPaddingX
	}
	inner := w - 2*inset
	doc, buttons, subtitle := m.portsDocument(inner)
	if (p.kind == "tun" || p.kind == "activation") && len(buttons) == 1 {
		p.focus = buttons[0].ID
	}
	p.controls = nil
	for _, hit := range doc.targets {
		if hit.tip.text == "" && !strings.HasPrefix(hit.ID, bindOptionPrefix) {
			p.controls = append(p.controls, hit.ID)
		}
	}
	var footer []string
	var footerHits []hitRegion
	leadingWidth := 0
	leadingID := "ports.back"
	if p.kind == "rules" && p.step == "edit" {
		leadingID = "ports.rulesReset"
	}
	line := ""
	for _, button := range buttons {
		blocked := m.busy && button.ID != "ports.cancel" || p.cancelling && button.ID == "ports.cancel" || doc.disabled != nil && doc.disabled(button.ID)
		if !blocked {
			p.controls = append(p.controls, button.ID)
		}
		primary := button.ID == "terminal.copyActivation" || button.ID == "tun.save" || button.ID == "ports.review" || button.ID == "ports.apply" || button.ID == "ports.retry" || button.ID == "ports.done" && p.applied
		kind := buttonSecondary
		if primary {
			kind = buttonPrimary
		} else if access && button.ID == leadingID {
			kind = buttonText
		}
		minWidth := 0
		if button.ID == "terminal.copyActivation" {
			minWidth = max(ansi.StringWidth(m.tr("terminal.activationCopy")), ansi.StringWidth(m.tr("terminal.activationCopyRequested")))
		}
		value := renderButton(button.Label, buttonOptions{Kind: kind, Surface: modalBackground, Width: minWidth,
			Compact: p.kind == "rules" && inner < 40,
			Focused: p.focus == button.ID, Hovered: !access && m.hover == button.ID,
			Disabled: blocked && !m.busy, Busy: blocked && m.busy, Plain: m.buttonPlain()})
		width := min(inner, ansi.StringWidth(value))
		if access && button.ID == leadingID {
			leadingWidth = width
		}
		if line != "" && ansi.StringWidth(line)+width > inner {
			footer = append(footer, line)
			line = ""
		}
		if !blocked {
			footerHits = append(footerHits, hitRegion{ID: button.ID, X: ansi.StringWidth(line), Y: len(footer), W: width, H: 1})
		}
		line += cellCrop(value, inner)
	}
	footer = append(footer, line)
	if p.kind != "" {
		for row, value := range footer {
			offset := max(0, inner-ansi.StringWidth(value))
			if row == 0 && leadingWidth > 0 {
				footer[row] = ansi.Cut(value, 0, leadingWidth) + strings.Repeat(" ", offset) + ansi.Cut(value, leadingWidth, inner)
			} else {
				footer[row] = strings.Repeat(" ", offset) + value
			}
			for i := range footerHits {
				if footerHits[i].Y == row && !(leadingWidth > 0 && footerHits[i].ID == leadingID) {
					footerHits[i].X += offset
				}
			}
		}
	}
	bodyY := 5
	if access {
		bodyY = 4
		if h < 24 {
			bodyY = 3
		}
	} else if p.kind == "activation" {
		bodyY = 3
	} else if h < 30 {
		bodyY = 2
	}
	var errors []string
	if p.err != "" {
		errors = strings.Split(wrapText(p.err, inner), "\n")
	}
	// These dialogs share the dashboard's contextual key hints.
	footerInset := 3
	if p.kind == "activation" || access {
		footerInset = 1
	}
	if modal {
		h = min(h, bodyY+len(doc.lines)+len(errors)+len(footer)+footerInset+1)
	}
	footerY := h - len(footer) - footerInset
	// Validation stays next to the fixed actions instead of disappearing below
	// the editor. Keep at least a complete field in the scrolling body; unusually
	// long diagnostics also remain available in full at the end of that body.
	errorHeight := min(len(errors), max(1, (footerY-bodyY-1)/2))
	if len(errors) > errorHeight {
		doc.space()
		doc.line(themed(p.err, errorColor, modalBackground, false))
		errors = append([]string(nil), errors[:errorHeight]...)
		errors[len(errors)-1] = cellCrop(errors[len(errors)-1], max(1, inner-2)) + " …"
	}
	errorY := footerY - 1 - errorHeight
	bodyH := max(1, errorY-bodyY)
	p.viewport.SetWidth(inner)
	p.viewport.SetHeight(bodyH)
	p.viewport.SetContentLines(doc.lines)
	if p.reveal {
		for _, hit := range doc.targets {
			if hit.ID == strings.TrimSuffix(p.focus, ".clear") {
				// Keep the field and any wrapped feedback visible together.
				top, bottom := max(0, hit.Y-1), hit.Y+hit.H
				bottom = max(bottom, min(doc.focusedRowEnd, hit.Y+bodyH))
				if hit.ID == bindAddressID && p.access.bindMenu.open {
					// On very short screens prioritize the whole highlighted option.
					bottom = max(bottom, doc.focusedRowEnd)
				}
				if top < p.viewport.YOffset() {
					p.viewport.SetYOffset(top)
				}
				if bottom > p.viewport.YOffset()+bodyH {
					p.viewport.SetYOffset(bottom - bodyH)
				}
			}
		}
		p.reveal = false
	}
	p.viewport.SetYOffset(p.viewport.YOffset())
	surface := m.newModalSurface(w, h)
	title := "ports.title"
	if p.kind == "activation" {
		title = "terminal.activationTitle"
	} else if access {
		title = "access." + p.kind + "Title"
	} else if p.kind != "" {
		title = "network." + p.kind + "Config.title"
	}
	if access {
		if p.step == "edit" {
			subtitle = ""
		}
		surface.accessHeader(m.tr(title), subtitle, inset)
	} else {
		surface.put(inset, 1, cellCrop(m.tr(title), inner))
		if bodyY == 5 {
			surface.put(inset, 2, themed(subtitle, muted, modalBackground, false))
			surface.divider(3)
		}
	}
	var hits []hitRegion
	for i, line := range strings.Split(p.viewport.View(), "\n") {
		if i >= bodyH {
			break
		}
		surface.put(inset, i+bodyY, cellPad(line, inner))
	}
	for _, hit := range doc.targets {
		bodyHit := hit
		hit.X, hit.Y = hit.X+inset, bodyY+hit.Y-p.viewport.YOffset()
		if clipped, ok := clipRegion(hit, inset, bodyY, inner, bodyH); ok {
			hits = append(hits, surface.screen(clipped))
		} else if bodyHit.tip.text != "" && bodyHit.tip.owner == p.focus {
			m.tooltip.offscreen = bodyHit
		}
	}
	for i, line := range errors {
		surface.put(inset, errorY+i, themed(cellPad(line, inner), errorColor, modalBackground, false))
	}
	surface.divider(footerY - 1)
	for i, value := range footer {
		surface.put(inset, footerY+i, value)
	}
	for _, hit := range footerHits {
		hit.X, hit.Y = hit.X+inset, hit.Y+footerY
		hits = append(hits, surface.screen(hit))
	}
	if p.kind != "activation" && !access {
		surface.put(inset, h-2, helpText(cellCrop(m.portsContextHelp(), inner), modalBackground, false))
	}
	if len(doc.lines) > bodyH {
		surface.put(w-inset, bodyY+bodyH-1, themed("↕", info, modalBackground, false))
	}
	return surface.render(base), hits
}
