package tui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

type portsState struct {
	open, cancelling, applied bool
	kind                      string // Empty for Settings → Ports; otherwise an overview network dialog.
	tunOriginal, tunEnabled   bool
	step, focus, returnFocus  string
	controls                  []string
	mixed, controller         textinput.Model
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
	plan         app.RuntimeSettingsPlan
	result       app.RuntimeSettingsResult
	snapshot     app.Snapshot
	hasSnapshot  bool
	err          error
	refreshError error
}

func newPortsState() portsState {
	input := func() textinput.Model {
		v := textinput.New()
		v.SetVirtualCursor(false)
		v.CharLimit = 16
		styleInput(&v, inputBackground, false)
		return v
	}
	view := viewport.New()
	view.SoftWrap = false
	return portsState{mixed: input(), controller: input(), focus: "ports.mixed", viewport: view}
}

func (m *Model) openPorts() tea.Cmd {
	return m.openNetworkConfig("")
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
	id, backend := m.id, m.backend
	m.ports.step, m.ports.err, m.ports.cancelling = "loading", "", false
	m.message = m.tr("ports.loading_message")
	return tea.Batch(func() tea.Msg {
		s, err := backend.GetRuntimeSettings(ctx)
		return portsMsg{id: id, kind: "load", settings: s, err: err}
	}, m.spinner.Tick)
}

func (m *Model) preparePorts() tea.Cmd {
	p := &m.ports
	parse := func(input *textinput.Model, label string) (int, error) {
		port, err := strconv.Atoi(strings.TrimSpace(input.Value()))
		if err != nil || port < 1024 || port > 65535 {
			err = errors.New(m.tr("ports.validation.range", label))
			input.Err = err
			return 0, err
		}
		input.Err = nil
		return port, nil
	}
	mixed, mixedErr := parse(&p.mixed, m.tr("ports.proxy_port"))
	controller, controllerErr := parse(&p.controller, m.tr("ports.controller_port"))
	if mixedErr != nil || controllerErr != nil {
		p.err = errors.Join(mixedErr, controllerErr).Error()
		if mixedErr != nil {
			p.focus = "ports.mixed"
		} else {
			p.focus = "ports.controller"
		}
		p.reveal = true
		return nil
	}
	if mixed == controller {
		p.err = m.tr("ports.validation.distinct")
		if p.kind == "terminal" {
			p.mixed.Err, p.focus = errors.New(p.err), "ports.mixed"
		} else {
			p.controller.Err, p.focus = errors.New(p.err), "ports.controller"
		}
		p.reveal = true
		return nil
	}
	ctx := m.newJob("ports-prepare")
	id, backend := m.id, m.backend
	settings := app.RuntimeSettings{MixedPort: mixed, ControllerPort: controller}
	terminalOnly, locale := p.kind == "terminal", m.locale
	p.err, p.cancelling = "", false
	p.step, p.focus = "preparing", "ports.cancel"
	p.viewport.GotoTop()
	m.message = m.tr("ports.checking_message")
	return tea.Batch(func() tea.Msg {
		plan, err := backend.PrepareRuntimeSettings(ctx, settings)
		// The terminal editor owns only the mixed port. Never let a concurrent
		// controller-port change turn the hidden, loaded value into a proposal.
		if err == nil && terminalOnly && plan.Current.ControllerPort != plan.Proposed.ControllerPort {
			err = errors.New(locale.T("network.terminalConfig.changed"))
		}
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
		p.step, p.focus = "edit", "ports.mixed"
		if msg.err == nil {
			p.current = msg.settings
			p.mixed.SetValue(strconv.Itoa(msg.settings.MixedPort))
			p.controller.SetValue(strconv.Itoa(msg.settings.ControllerPort))
		} else {
			p.step, p.focus = "load-error", "ports.retry"
		}
	case "prepare":
		p.step, p.focus = "edit", "ports.mixed"
		if msg.err == nil {
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
		m.notification.critical = notificationCritical(msg.err) || msg.refreshError != nil || msg.result.Changed && msg.err != nil
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
	}
}

func (m *Model) closePorts() {
	if m.busy {
		m.cancelTask(false)
		return
	}
	m.ports.open = false
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
	if m.ports.kind == "tun" && strings.HasPrefix(id, "tun.") {
		return m.networkTUNActivate(id)
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
		m.ports.step, m.ports.focus, m.ports.reveal = "edit", "ports.mixed", true
		m.ports.viewport.GotoTop()
	case "ports.retry":
		if m.ports.step == "load-error" {
			return m.loadPorts()
		}
		m.ports.step = "edit"
		return m.preparePorts()
	case "ports.mixed", "ports.controller":
		m.ports.focus, m.ports.reveal = id, true
		return textinput.Blink
	}
	return nil
}

func (m *Model) updatePortsInput(msg tea.Msg) tea.Cmd {
	if m.busy || m.ports.step != "edit" {
		return nil
	}
	var input *textinput.Model
	switch m.ports.focus {
	case "ports.mixed":
		input = &m.ports.mixed
	case "ports.controller":
		input = &m.ports.controller
	default:
		return nil
	}
	before := input.Value()
	var cmd tea.Cmd
	*input, cmd = input.Update(msg)
	if input.Value() != before {
		input.Err, m.ports.err, m.ports.plan = nil, "", app.RuntimeSettingsPlan{}
		m.ports.reveal = true
	}
	return cmd
}

func (m *Model) portsKey(k tea.KeyPressMsg) tea.Cmd {
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
	if p.kind != "" {
		switch key {
		case "down":
			key = "tab"
		case "up":
			key = "shift+tab"
		case "space", " ", "left", "right":
			if p.focus == "tun.policy" {
				return m.portsActivate(p.focus)
			}
		}
	}
	switch key {
	case "esc":
		if p.step == "review" {
			return m.portsActivate("ports.back")
		}
		m.closePorts()
	case "tab", "shift+tab":
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
		if p.step == "edit" && (p.focus == "ports.mixed" || p.focus == "ports.controller") {
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
		m.ports.viewport, _ = m.ports.viewport.Update(event)
		return nil
	}
	m.hover = ""
	for _, hit := range m.targets {
		if k.X < hit.X || k.X >= hit.X+hit.W || k.Y < hit.Y || k.Y >= hit.Y+hit.H {
			continue
		}
		m.hover = hit.ID
		if _, clicked := event.(tea.MouseClickMsg); clicked && k.Button == tea.MouseLeft {
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
	subtitle := m.tr("ports.edit")
	switch p.step {
	case "loading":
		subtitle = m.tr("ports.loading")
		d.line(m.spinner.View() + m.tr("ports.loading_current"))
	case "preparing":
		subtitle = m.tr("ports.checking")
		d.label(m.tr("ports.checking_title"))
		d.pair(m.tr("ports.proxy_port"), p.mixed.Value())
		if p.kind == "" {
			d.pair(m.tr("ports.controller_port"), p.controller.Value())
		}
		d.line(m.spinner.View() + m.tr("ports.checking_detail"))
		d.line(m.tr("ports.review_boundary"))
	case "edit":
		if p.kind == "terminal" {
			m.terminalConfigFields(&d)
			buttons = append(buttons, wizardButton{"ports.review", m.tr("network.terminalConfig.save")})
			break
		}
		d.label(m.tr("ports.listeners"))
		proxy, controller := "--", "--"
		if p.current.MixedPort > 0 && p.current.MixedPort <= 65535 {
			proxy = net.JoinHostPort("127.0.0.1", strconv.Itoa(p.current.MixedPort))
		}
		if host, _, err := net.SplitHostPort(overviewControllerAddress(m.snapshot.Controller)); err == nil && p.current.ControllerPort > 0 && p.current.ControllerPort <= 65535 {
			controller = net.JoinHostPort(host, strconv.Itoa(p.current.ControllerPort))
		}
		d.pair(m.tr("ports.proxy_address"), proxy)
		d.pair(m.tr("ports.controller"), controller)
		d.line(m.tr("ports.edit_boundary"))
		d.space()
		d.field("ports.mixed", m.tr("ports.proxy_field"), &p.mixed, p.focus)
		d.space()
		d.field("ports.controller", m.tr("ports.controller_port"), &p.controller, p.focus)
		d.space()
		d.line(m.tr("ports.range_hint"))
		buttons = append(buttons, wizardButton{"ports.review", m.tr("ports.review")})
	case "review":
		subtitle = m.tr("ports.review")
		d.label(m.tr("ports.reviewed"))
		d.pair(m.tr("ports.proxy_port"), fmt.Sprintf("%d → %d", p.plan.Current.MixedPort, p.plan.Proposed.MixedPort))
		if p.kind == "" {
			d.pair(m.tr("ports.controller_port"), fmt.Sprintf("%d → %d", p.plan.Current.ControllerPort, p.plan.Proposed.ControllerPort))
			d.pair(m.tr("ports.listen_address"), m.tr("ports.loopback", p.plan.ControllerHost))
			d.pair(m.tr("ports.profile"), snapshotProfileName(m.snapshot, p.plan.AppliedProfile, m.locale))
			d.pair(m.tr("ports.api_key"), m.tr("ports.key_retained"))
		}
		d.space()
		label := m.tr("ports.apply")
		if !p.plan.Changed {
			d.line(m.tr("ports.unchanged"))
			label = m.tr("ports.keep_current")
		} else if p.plan.Active {
			d.line(themed(m.tr("ports.restart_warning"), warning, raised, false))
			d.line(m.tr("ports.restart_boundary"))
			label = m.tr("ports.apply_restart")
		} else {
			d.line(m.tr("ports.stopped_boundary"))
		}
		d.space()
		d.line(m.tr("ports.preserved"))
		buttons = []wizardButton{{"ports.back", m.tr("ports.back")}, {"ports.apply", label}}
	case "applying":
		subtitle = m.tr("ports.applying")
		d.label(m.tr("ports.applying_title"))
		if p.cancelling {
			d.line(themed(m.tr("ports.restoring"), warning, raised, false))
		} else {
			d.line(m.spinner.View() + m.tr("ports.updating"))
		}
		d.line(m.tr("ports.cancel_boundary"))
	case "result":
		subtitle = m.tr("ports.result")
		buttons = []wizardButton{{"ports.done", m.tr("ports.done")}}
		if p.applied {
			d.line(themed(m.tr("ports.applied"), success, raised, false))
			d.pair(m.tr("ports.proxy_port"), strconv.Itoa(p.result.Settings.MixedPort))
			if p.kind == "" {
				d.pair(m.tr("ports.controller"), p.result.Controller)
			}
			if p.result.Restarted {
				d.line(m.tr("ports.restarted"))
			} else if !m.snapshot.Service.Active && p.refreshError == "" {
				d.line(m.tr("ports.stopped"))
			} else {
				d.line(m.tr("ports.no_restart"))
			}
		} else {
			d.line(themed(m.tr("ports.incomplete"), errorColor, raised, false))
			d.line(m.tr("ports.retry_boundary"))
			buttons = append(buttons, wizardButton{"ports.retry", m.tr("ports.review_retry")})
		}
		if p.refreshError != "" {
			d.line(themed(p.refreshError, warning, raised, false))
		}
	case "load-error":
		subtitle = m.tr("ports.unavailable")
		d.line(m.tr("ports.settings_unavailable"))
		buttons = append(buttons, wizardButton{"ports.retry", m.tr("ports.retry")})
	}
	if p.cancelling && p.step != "applying" {
		d.line(themed(m.tr("ports.cancelling"), warning, raised, false))
	}
	return d, buttons, subtitle
}

func (m *Model) renderPorts(base string) (string, []hitRegion) {
	p := &m.ports
	w, h, x, y := m.width, m.height, 0, 0
	modal := w >= 110 && h >= 30
	if p.kind != "" {
		// Network configurations float over the entire dashboard, including
		// the sidebar. Even the narrow layout keeps a visible gutter.
		modal = true
		preferredWidth := min(66, max(34, w*3/5))
		if p.kind == "activation" {
			command := pendingActivationCommand(m.snapshot.TerminalProxy)
			preferredWidth = max(preferredWidth, min(80, ansi.StringWidth(clean(command))+6))
		}
		w = min(preferredWidth, max(0, w-4))
		h -= 2
		x = (m.width - w) / 2
	} else if modal {
		w, h = min(78, w-12), h-4
		x = (m.width - w) / 2
	}
	if w < 34 || h < 10 {
		p.controls = nil
		label := m.tr("ports.compact_help")
		if m.busy {
			label = m.tr("ports.compact_busy")
		}
		if p.kind != "" {
			label = cellCrop(label, max(1, m.width-2))
			return overlay(base, themed(label, text, raised, false), (m.width-ansi.StringWidth(label))/2, m.height/2, m.width, m.height), nil
		}
		return overlay("", themed(cellCrop(label, w), text, raised, false), 0, 0, m.width, m.height), nil
	}
	inner := w - 6
	doc, buttons, subtitle := m.portsDocument(inner)
	if (p.kind == "tun" || p.kind == "activation") && len(buttons) == 1 {
		p.focus = buttons[0].ID
	}
	p.controls = nil
	for _, hit := range doc.targets {
		p.controls = append(p.controls, hit.ID)
	}
	var footer []string
	var footerHits []hitRegion
	line := ""
	for _, button := range buttons {
		blocked := m.busy && button.ID != "ports.cancel" || p.cancelling && button.ID == "ports.cancel"
		if !blocked {
			p.controls = append(p.controls, button.ID)
		}
		primary := button.ID == "terminal.copyActivation" || button.ID == "tun.save" || button.ID == "ports.review" || button.ID == "ports.apply" || button.ID == "ports.retry" || button.ID == "ports.done" && p.applied
		kind := buttonSecondary
		if primary {
			kind = buttonPrimary
		}
		minWidth := 0
		if button.ID == "terminal.copyActivation" {
			minWidth = max(ansi.StringWidth(m.tr("terminal.activationCopy")), ansi.StringWidth(m.tr("terminal.activationCopyRequested")))
		}
		value := renderButton(button.Label, buttonOptions{Kind: kind, Surface: raised, Width: minWidth,
			Focused: p.focus == button.ID, Hovered: m.hover == button.ID,
			Disabled: blocked && !m.busy, Busy: blocked && m.busy, Plain: m.buttonPlain()})
		width := min(inner, ansi.StringWidth(value))
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
			footer[row] = strings.Repeat(" ", offset) + value
			for i := range footerHits {
				if footerHits[i].Y == row {
					footerHits[i].X += offset
				}
			}
		}
	}
	bodyY := 5
	if p.kind == "activation" {
		bodyY = 3
	} else if h < 30 {
		bodyY = 2
	}
	var errors []string
	if p.err != "" {
		errors = strings.Split(wrapText(p.err, inner), "\n")
	}
	// The activation dialog uses the dashboard's key hints. Its fixed footer
	// only needs the bottom border after the buttons.
	footerInset := 3
	if p.kind == "activation" {
		footerInset = 1
	}
	if modal {
		h = min(h, bodyY+len(doc.lines)+len(errors)+len(footer)+footerInset+1)
		y = (m.height - h) / 2
	}
	footerY := h - len(footer) - footerInset
	// Validation stays next to the fixed actions instead of disappearing below
	// the editor. Keep at least a complete field in the scrolling body; unusually
	// long diagnostics also remain available in full at the end of that body.
	errorHeight := min(len(errors), max(1, (footerY-bodyY-1)/2))
	if len(errors) > errorHeight {
		doc.space()
		doc.line(themed(p.err, errorColor, raised, false))
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
				// Include the field label as well as its entire three-row frame.
				top, bottom := max(0, hit.Y-1), hit.Y+hit.H
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
	rows := make([]string, h)
	for i := range rows {
		rows[i] = themed(strings.Repeat(" ", w), text, raised, false)
	}
	putRows(rows, 0, 0, themed("┌"+strings.Repeat("─", w-2)+"┐", border, raised, false), w)
	putRows(rows, 0, h-1, themed("└"+strings.Repeat("─", w-2)+"┘", border, raised, false), w)
	for i := 1; i < h-1; i++ {
		putRows(rows, 0, i, themed("│", border, raised, false), w)
		putRows(rows, w-1, i, themed("│", border, raised, false), w)
	}
	title := "ports.title"
	if p.kind == "activation" {
		title = "terminal.activationTitle"
	} else if p.kind != "" {
		title = "network." + p.kind + "Config.title"
	}
	putRows(rows, 3, 1, themed(cellCrop(m.tr(title), inner), text, raised, false), w)
	if bodyY == 5 {
		putRows(rows, 3, 2, themed(subtitle, muted, raised, false), w)
		putRows(rows, 2, 3, themed(strings.Repeat("─", w-4), divider, raised, false), w)
	}
	var hits []hitRegion
	for i, line := range strings.Split(p.viewport.View(), "\n") {
		if i >= bodyH {
			break
		}
		putRows(rows, 3, i+bodyY, themed(cellPad(line, inner), text, raised, false), w)
	}
	for _, hit := range doc.targets {
		hit.X, hit.Y = hit.X+3, bodyY+hit.Y-p.viewport.YOffset()
		if clipped, ok := clipRegion(hit, 3, bodyY, inner, bodyH); ok {
			clipped.X, clipped.Y = clipped.X+x, clipped.Y+y
			hits = append(hits, clipped)
		}
	}
	for i, line := range errors {
		putRows(rows, 3, errorY+i, themed(cellPad(line, inner), errorColor, raised, false), w)
	}
	putRows(rows, 2, footerY-1, themed(strings.Repeat("─", w-4), divider, raised, false), w)
	for i, value := range footer {
		putRows(rows, 3, footerY+i, themed(value, text, raised, false), w)
	}
	for _, hit := range footerHits {
		hit.X, hit.Y = hit.X+x+3, hit.Y+y+footerY
		hits = append(hits, hit)
	}
	if p.kind != "activation" {
		putRows(rows, 3, h-2, helpText(cellCrop(m.portsContextHelp(), inner), raised, false), w)
	}
	if len(doc.lines) > bodyH {
		putRows(rows, w-3, bodyY+bodyH-1, themed("↕", info, raised, false), w)
	}
	if !modal {
		base = ""
	}
	return overlay(base, strings.Join(rows, "\n"), x, y, m.width, m.height), hits
}
