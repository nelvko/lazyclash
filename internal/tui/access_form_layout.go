package tui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) accessInputBlock(id string, width int, compact bool) dashboardBlock {
	p := &m.ports
	input := p.field(id)
	if p.focus == id {
		input.Focus()
	} else {
		input.Blur()
	}
	*input = prepareInputField(*input, inputFieldTextWidth(width, 0, true), modalBackground, input.Err != nil)
	options := inputFieldOptions{
		ID: id, ClearID: id + ".clear", Width: width, Compact: compact, Surface: modalBackground,
		Focused: p.focus == id, Invalid: input.Err != nil, ClearFocused: p.focus == id+".clear",
	}
	if p.kind == "proxy" {
		options.ClearGlyph = "×"
	}
	return renderInputField(*input, options)
}

func (m *Model) accessFieldBlock(id, label string, width int) dashboardBlock {
	field := dashboardBlock{lines: strings.Split(wrapText(themed(label, text, modalBackground, false), width), "\n")}
	field.append(m.accessInputBlock(id, width, false))
	return field
}

// Place full field blocks on one row. The clear button and native cursor
// follow the same column offset as the input's visible frame.
func appendAccessFields(d *wizardDocument, gap int, fields ...dashboardBlock) {
	var parts []string
	x, y := 0, len(d.lines)
	for i, field := range fields {
		if i > 0 {
			parts = append(parts, strings.Repeat(" ", gap))
			x += gap
		}
		for _, hit := range field.targets {
			hit.X += x
			hit.Y += y
			d.targets = append(d.targets, hit)
		}
		s := strings.Join(field.lines, "\n")
		parts = append(parts, s)
		x += lipgloss.Width(s)
	}
	d.lines = append(d.lines, strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, parts...), "\n")...)
}

func (m *Model) accessEdit(d *wizardDocument) {
	switch m.ports.kind {
	case "auth":
		m.proxyAuthEdit(d)
	case "proxy":
		d.line(themed(m.tr("access.proxyHint"), muted, modalBackground, false))
		d.space()
		fields := m.ports.visibleProxyFields()
		framed := d.width >= 48 && m.height >= 36
		m.accessPortGroup(d, m.tr("access.standard"), fields[:3], framed)
		if len(fields) > 3 {
			d.space()
			if framed {
				d.space()
			}
			m.accessPortGroup(d, m.tr("access.transparent"), fields[3:], framed)
		}
		if m.height >= 22 {
			d.space()
		}
	case "control":
		m.accessControlFields(d)
	}
}

// Disclosure headings and fixed groups share the same rule. The whole
// disclosure row, including its rule, is a mouse target.
func (m *Model) accessSection(d *wizardDocument, title, id string) bool {
	expanded := m.ports.access.sections[id]
	color := accent
	if id != "" {
		marker := "▸ "
		if expanded {
			marker = "▾ "
		}
		title = marker + title
		if m.ports.focus == id {
			title = "› " + title
			color = focus
		} else {
			title = "  " + title
		}
		d.targets = append(d.targets, hitRegion{ID: id, Y: len(d.lines), W: d.width, H: 1})
	}
	line := themed(lipgloss.NewStyle().Bold(true).Render(title), color, modalBackground, false)
	if n := d.width - ansi.StringWidth(title); n > 2 {
		line += themed("  "+strings.Repeat("─", n-2), divider, modalBackground, false)
	}
	d.lines = append(d.lines, cellCrop(line, d.width))
	return expanded
}

func (m *Model) proxyPortTooltip(id string) tooltipBinding {
	key := accessKey(id) + ".tooltip"
	switch id {
	case "ports.mixed", "ports.socks":
	case "ports.redir", "ports.tproxy":
		if !m.ports.proxyPortPlatformKnown() {
			key += ".unknownPlatform"
		}
	default:
		return tooltipBinding{}
	}
	return tooltipBinding{owner: id, text: m.tr(key)}
}

func (m *Model) accessPortGroup(d *wizardDocument, title string, ids []string, framed bool) {
	m.accessSection(d, title, "")
	if m.height >= 22 {
		d.space()
	}
	const gap = 1
	inputWidth := min(15, d.width/2-1)
	if framed {
		inputWidth = 18
	}
	captionWidth := d.width - inputWidth - gap
	for i, id := range ids {
		label := m.tr(accessKey(id))
		if framed {
			label = "  " + label
		}
		var caption dashboardBlock
		if tip := m.proxyPortTooltip(id); tip.text != "" {
			caption = tip.label(label, dashboardOptions{tooltips: &m.tooltip}, modalBackground, captionWidth)
		} else {
			caption.lines = strings.Split(wrapText(label, captionWidth), "\n")
		}
		caption.lines[0] = cellPad(caption.lines[0], captionWidth)
		var aligned dashboardBlock
		if framed {
			aligned.blank() // Align the label and tooltip with the value inside the frame.
		}
		aligned.append(caption)
		appendAccessFields(d, gap, aligned, m.accessInputBlock(id, inputWidth, !framed))
		if message := m.ports.proxyFieldError(id); message != "" {
			d.lines = append(d.lines, strings.Split(lipgloss.NewStyle().Width(d.width).Align(lipgloss.Right).Foreground(lipgloss.Color(errorColor)).Render(message), "\n")...)
		}
		if id == strings.TrimSuffix(m.ports.focus, ".clear") {
			d.focusedRowEnd = len(d.lines)
		}
		if !framed && m.height >= 26 && i+1 < len(ids) {
			d.space()
		}
	}
}

func (m *Model) accessControlFields(d *wizardDocument) {
	p := &m.ports
	field := func(id string, width int) dashboardBlock {
		return m.accessFieldBlock(id, m.tr(accessKey(id)), width)
	}
	pair := func(left, right string) {
		const gap = 2
		if d.width >= 58 {
			width := (d.width - gap) / 2
			appendAccessFields(d, gap, field(left, width), field(right, d.width-width-gap))
		} else {
			appendAccessFields(d, 0, field(left, d.width))
			appendAccessFields(d, 0, field(right, d.width))
		}
	}
	section := func(id string) bool {
		d.space()
		return m.accessSection(d, m.tr(accessKey(id)), id)
	}
	m.accessSection(d, m.tr("access.baseSection"), "")
	pair("ports.address", "ports.secret")
	if section("ports.tlsSection") {
		appendAccessFields(d, 0, field("ports.https", d.width))
		appendAccessFields(d, 0, field("ports.cert", d.width))
		appendAccessFields(d, 0, field("ports.key", d.width))
	}
	if section("ports.corsSection") {
		d.label(m.tr("access.origins"))
		v := &p.access.origins
		edge := border
		if p.focus == "ports.origins" {
			v.Focus()
			edge = accent
		} else {
			v.Blur()
		}
		v.EndOfBufferCharacter = ' '
		base := lipgloss.NewStyle().Foreground(lipgloss.Color(text)).Background(lipgloss.Color(modalBackground))
		state := textarea.StyleState{Base: base, Text: base, CursorLine: base, Placeholder: base.Foreground(lipgloss.Color(muted))}
		v.SetStyles(textarea.Styles{Focused: state, Blurred: state, Cursor: textarea.CursorStyle{Color: lipgloss.Color(focus)}})
		v.SetWidth(d.width - 4)
		lines := inputFrame(strings.Split(v.View(), "\n"), d.width, edge, modalBackground)
		d.targets = append(d.targets, hitRegion{ID: "ports.origins", Y: len(d.lines), W: d.width, H: len(lines)})
		d.lines = append(d.lines, lines...)
		check := "[ ] "
		if p.access.private {
			check = "[x] "
		}
		d.control("ports.private", check+m.tr("access.private"), p.focus)
	}
	if section("ports.advancedSection") {
		appendAccessFields(d, 0, field("ports.unix", d.width))
		d.pair(m.tr("access.pipe"), m.tr("access.windowsOnly"))
		pair("ports.mark", "ports.doh")
		d.line(themed(m.tr("access.localHint"), muted, modalBackground, false))
	}
}
