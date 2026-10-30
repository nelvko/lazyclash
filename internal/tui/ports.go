package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

type portsState struct {
	open, cancelling, applied bool
	step, focus, returnFocus  string
	controls                  []string
	mixed, controller         textinput.Model
	current                   app.RuntimeSettings
	plan                      app.RuntimeSettingsPlan
	result                    app.RuntimeSettingsResult
	err, refreshError         string
	scroll                    int
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
		v.CharLimit = 16
		styleInput(&v, inputBackground, false)
		return v
	}
	return portsState{mixed: input(), controller: input(), focus: "ports.mixed"}
}

func (m *Model) openPorts() tea.Cmd {
	if !m.snapshot.Installed {
		m.failure = "Install Mihomo before changing runtime ports"
		return nil
	}
	p := newPortsState()
	p.open, p.returnFocus, p.reveal = true, m.focus, true
	m.ports = p
	return m.loadPorts()
}

func (m *Model) loadPorts() tea.Cmd {
	ctx := m.newJob("ports-load")
	id, backend := m.id, m.backend
	m.ports.step, m.ports.err, m.ports.cancelling = "loading", "", false
	m.message = "Loading managed runtime settings…"
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
			err = fmt.Errorf("%s must be a whole number between 1024 and 65535", label)
			input.Err = err
			return 0, err
		}
		input.Err = nil
		return port, nil
	}
	mixed, mixedErr := parse(&p.mixed, "Proxy port")
	controller, controllerErr := parse(&p.controller, "Control API port")
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
		p.err = "Proxy port and control API port must differ"
		p.controller.Err = errors.New(p.err)
		p.focus, p.reveal = "ports.controller", true
		return nil
	}
	ctx := m.newJob("ports-prepare")
	id, backend := m.id, m.backend
	settings := app.RuntimeSettings{MixedPort: mixed, ControllerPort: controller}
	p.err, p.cancelling = "", false
	p.step, p.focus, p.scroll = "preparing", "ports.cancel", 0
	m.message = "Checking runtime ports, configuration and current service…"
	return tea.Batch(func() tea.Msg {
		plan, err := backend.PrepareRuntimeSettings(ctx, settings)
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
	m.message = "Applying runtime settings…"
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
		p.err = clean(msg.err.Error())
	}
	if msg.hasSnapshot {
		m.snapshot = msg.snapshot
		m.snapshotReady = true
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
			p.scroll = 0
		}
	case "apply":
		p.result, p.step, p.focus = msg.result, "result", "ports.done"
		p.applied = msg.err == nil
		if p.applied {
			p.current = msg.result.Settings
			m.message = "Runtime settings applied"
		} else {
			m.message = "Runtime settings application finished; review the result"
		}
		if msg.refreshError != nil {
			p.refreshError = "Could not refresh the dashboard; refresh again after closing this dialog."
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
	}
	if strings.HasPrefix(m.message, "Loading managed") || strings.HasPrefix(m.message, "Checking runtime") {
		m.message = ""
	}
}

func (m *Model) portsActivate(id string) tea.Cmd {
	if m.busy {
		if id == "ports.cancel" {
			m.cancelTask(false)
		}
		return nil
	}
	switch id {
	case "ports.cancel", "ports.done":
		m.closePorts()
	case "ports.review":
		return m.preparePorts()
	case "ports.apply":
		return m.applyPorts()
	case "ports.back":
		m.ports.step, m.ports.focus, m.ports.scroll, m.ports.reveal = "edit", "ports.mixed", 0, true
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
	switch k.String() {
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
			if k.String() == "shift+tab" {
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
	case "pgup":
		p.scroll = max(0, p.scroll-5)
	case "pgdown":
		p.scroll += 5
	default:
		return m.updatePortsInput(k)
	}
	return nil
}

func (m *Model) portsMouse(event tea.MouseMsg) tea.Cmd {
	k := event.Mouse()
	if _, wheel := event.(tea.MouseWheelMsg); wheel {
		switch k.Button {
		case tea.MouseWheelUp:
			m.ports.scroll = max(0, m.ports.scroll-3)
		case tea.MouseWheelDown:
			m.ports.scroll += 3
		}
		return nil
	}
	m.hover = ""
	for _, hit := range m.targets {
		if k.X < hit.X || k.X >= hit.X+hit.W || k.Y < hit.Y || k.Y >= hit.Y+hit.H {
			continue
		}
		m.hover = hit.ID
		if _, clicked := event.(tea.MouseClickMsg); clicked && k.Button == tea.MouseLeft {
			m.ports.focus = hit.ID
			return m.portsActivate(hit.ID)
		}
		break
	}
	return nil
}

func (m *Model) portsDocument(width int) (wizardDocument, []wizardButton, string) {
	p := &m.ports
	d := wizardDocument{width: width, hover: m.hover}
	buttons := []wizardButton{{"ports.cancel", "Cancel"}}
	subtitle := "Edit ports"
	switch p.step {
	case "loading":
		subtitle = "Loading"
		d.line(m.spinner.View() + " Loading current managed ports…")
	case "preparing":
		subtitle = "Checking changes"
		d.label("Checking runtime settings")
		d.pair("Proxy port", p.mixed.Value())
		d.pair("Control API port", p.controller.Value())
		d.line(m.spinner.View() + " Checking port availability, configuration and current service…")
		d.line("No change is applied until you review and confirm the resulting plan.")
	case "edit":
		d.label("Local listeners")
		d.line("Only loopback port settings change. The managed API key stays private and unchanged.")
		d.space()
		d.field("ports.mixed", "Proxy port · HTTP and SOCKS", &p.mixed, p.focus)
		d.space()
		d.field("ports.controller", "Control API port", &p.controller, p.focus)
		d.space()
		d.line("Use distinct ports between 1024 and 65535. Review checks availability before anything is applied.")
		buttons = append(buttons, wizardButton{"ports.review", "Review changes"})
	case "review":
		subtitle = "Review changes"
		d.label("Reviewed port changes")
		d.pair("Proxy port", fmt.Sprintf("%d → %d", p.plan.Current.MixedPort, p.plan.Proposed.MixedPort))
		d.pair("Control API port", fmt.Sprintf("%d → %d", p.plan.Current.ControllerPort, p.plan.Proposed.ControllerPort))
		d.pair("Listen address", p.plan.ControllerHost+" (loopback only)")
		d.pair("Current profile", snapshotProfileName(m.snapshot, p.plan.AppliedProfile))
		d.pair("API key", "Existing managed key retained; never displayed")
		d.space()
		label := "Apply settings"
		if !p.plan.Changed {
			d.line("No changes; the current settings already match.")
			label = "Keep current"
		} else if p.plan.Active {
			d.line(themed("Restart required: active connections will be interrupted.", warning, raised, false))
			d.line("Apply stops and restarts this managed core, then reconnects the control API. On failure, recovery attempts to restore the prior configuration and service state.")
			label = "Apply & restart"
		} else {
			d.line("The service is stopped and will remain stopped. These ports take effect at the next explicit start.")
		}
		d.space()
		d.line("Profiles, system proxy, DNS, TUN and autostart settings are preserved.")
		buttons = []wizardButton{{"ports.back", "Back"}, {"ports.apply", label}}
	case "applying":
		subtitle = "Applying"
		d.label("Applying runtime settings")
		if p.cancelling {
			d.line(themed("Cancelling / Restoring previous state…", warning, raised, false))
		} else {
			d.line(m.spinner.View() + " Updating configuration and checking the managed service…")
		}
		d.line("Cancellation waits for the application task and any required recovery before leaving.")
	case "result":
		subtitle = "Result"
		buttons = []wizardButton{{"ports.done", "Back to Settings"}}
		if p.applied {
			d.line(themed("Runtime settings applied", success, raised, false))
			d.pair("Proxy port", strconv.Itoa(p.result.Settings.MixedPort))
			d.pair("Control API", p.result.Controller)
			if p.result.Restarted {
				d.line("The managed core restarted and its control API passed readiness checks.")
			} else if !m.snapshot.Service.Active && p.refreshError == "" {
				d.line("The service remains stopped. Start it explicitly when ready.")
			} else {
				d.line("No service restart was required.")
			}
		} else {
			d.line(themed("Runtime settings did not complete", errorColor, raised, false))
			d.line("Review the error and current service state before retrying. Retry prepares a new plan and asks for confirmation again.")
			buttons = append(buttons, wizardButton{"ports.retry", "Review / Retry"})
		}
		if p.refreshError != "" {
			d.line(themed(p.refreshError, warning, raised, false))
		}
	case "load-error":
		subtitle = "Unavailable"
		d.line("Current runtime settings are unavailable.")
		buttons = append(buttons, wizardButton{"ports.retry", "Retry"})
	}
	if p.err != "" {
		d.space()
		d.line(themed(p.err, errorColor, raised, false))
	}
	if p.cancelling && p.step != "applying" {
		d.line(themed("Cancelling; waiting for the current check to finish safely…", warning, raised, false))
	}
	return d, buttons, subtitle
}

func (m *Model) renderPorts(base string) (string, []hitRegion) {
	p := &m.ports
	w, h, x, y := m.width, m.height, 0, 0
	modal := w >= 110 && h >= 30
	if modal {
		w, h = min(78, w-12), min(28, h-4)
		x, y = (m.width-w)/2, (m.height-h)/2
	}
	if w < 34 || h < 10 {
		label := "Please enlarge terminal · Esc Back · Ctrl+C Quit"
		if m.busy {
			label = "Working / Cleaning up · Esc Cancel · Ctrl+C Quit"
		}
		return overlay("", themed(cellCrop(label, w), text, raised, false), 0, 0, m.width, m.height), nil
	}
	inner := w - 6
	doc, buttons, subtitle := m.portsDocument(inner)
	p.controls = nil
	for _, hit := range doc.targets {
		p.controls = append(p.controls, hit.ID)
	}
	var footer []string
	var footerHits []hitRegion
	line := ""
	for _, button := range buttons {
		p.controls = append(p.controls, button.ID)
		primary := button.ID == "ports.review" || button.ID == "ports.apply" || button.ID == "ports.retry" || button.ID == "ports.done" && p.applied
		value := dashboardButton(button.ID, button.Label, dashboardOptions{Focus: p.focus, Hover: m.hover}, primary)
		value = themed(value, text, raised, false)
		width := min(inner, ansi.StringWidth(value))
		if line != "" && ansi.StringWidth(line)+2+width > inner {
			footer = append(footer, line)
			line = ""
		}
		if line != "" {
			line += themed("  ", text, raised, false)
		}
		footerHits = append(footerHits, hitRegion{ID: button.ID, X: ansi.StringWidth(line), Y: len(footer), W: width, H: 1})
		line += cellCrop(value, inner)
	}
	footer = append(footer, line)
	footerY := h - len(footer) - 3
	bodyY, bodyH := 5, max(1, footerY-6)
	if p.reveal {
		for _, hit := range doc.targets {
			if hit.ID == p.focus {
				if hit.Y < p.scroll {
					p.scroll = hit.Y
				}
				if hit.Y+hit.H > p.scroll+bodyH {
					p.scroll = hit.Y + hit.H - bodyH
				}
			}
		}
		p.reveal = false
	}
	p.scroll = max(0, min(p.scroll, max(0, len(doc.lines)-bodyH)))
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
	putRows(rows, 3, 1, themed("Runtime settings", text, raised, false), w)
	putRows(rows, 3, 2, themed(subtitle, muted, raised, false), w)
	putRows(rows, 2, 3, themed(strings.Repeat("─", w-4), divider, raised, false), w)
	var hits []hitRegion
	for i := 0; i < bodyH && i+p.scroll < len(doc.lines); i++ {
		putRows(rows, 3, i+bodyY, themed(cellPad(doc.lines[i+p.scroll], inner), text, raised, false), w)
	}
	for _, hit := range doc.targets {
		hit.X, hit.Y = 3, bodyY+hit.Y-p.scroll
		if clipped, ok := clipRegion(hit, 3, bodyY, inner, bodyH); ok {
			clipped.X, clipped.Y = clipped.X+x, clipped.Y+y
			hits = append(hits, clipped)
		}
	}
	putRows(rows, 2, footerY-1, themed(strings.Repeat("─", w-4), divider, raised, false), w)
	for i, value := range footer {
		putRows(rows, 3, footerY+i, themed(value, text, raised, false), w)
	}
	for _, hit := range footerHits {
		hit.X, hit.Y = hit.X+x+3, hit.Y+y+footerY
		hits = append(hits, hit)
	}
	help := "Tab Focus · Enter Review / Apply · Esc Back · Ctrl+C Quit"
	if m.busy {
		help = "Esc Cancel · wait for safe recovery before leaving"
	}
	putRows(rows, 3, h-2, helpText(cellCrop(help, inner), raised, false), w)
	if len(doc.lines) > bodyH {
		putRows(rows, w-3, bodyY+bodyH-1, themed("↕", info, raised, false), w)
	}
	if !modal {
		base = ""
	}
	return overlay(base, strings.Join(rows, "\n"), x, y, m.width, m.height), hits
}
