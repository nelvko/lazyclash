package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func (m Model) ruleProvidersStatus() string {
	p := m.ruleProviders
	if _, ok := m.backend.(ruleProviderBackend); !ok {
		return m.tr("rules.providers.unsupported")
	}
	if !p.received {
		return m.tr("rules.providers.loading")
	}
	if p.snapshot.State == "ready" {
		status := m.tr("rules.providers.count", len(m.filteredRuleProviders()), len(p.snapshot.Providers))
		if p.pending {
			status += " · " + m.tr("rules.providers.refreshing")
		}
		if p.running() > 0 || len(p.queue) > 0 {
			status += " · " + m.tr("rules.providers.progress", p.running(), len(p.queue))
		}
		return status
	}
	switch p.snapshot.State {
	case "uninstalled", "stopped", "unsupported":
		return m.tr("rules." + p.snapshot.State)
	default:
		return m.tr("rules.unavailable")
	}
}

func (m Model) ruleProvidersToolbar(width, rows int) dashboardBlock {
	b := m.searchField("rules.providers-search", m.tr("rules.providers.search_placeholder"), m.ruleProviders.search, width, false)
	b.lines = append(b.lines, themed(cellCrop(m.ruleProvidersStatus(), width), muted, raised, false), "")
	b.lines = b.lines[:min(rows, len(b.lines))]
	visible := b.targets[:0]
	for _, target := range b.targets {
		if clipped, ok := clipRegion(target, 0, 0, width, len(b.lines)); ok {
			visible = append(visible, clipped)
		}
	}
	b.targets = visible
	return b
}

func (m Model) ruleProviderUpdateLabel(provider mihomo.RuleProvider) (string, bool) {
	if strings.EqualFold(provider.VehicleType, "Inline") {
		return "", false
	}
	label := m.tr("rules.providers.update")
	if strings.EqualFold(provider.VehicleType, "File") {
		label = m.tr("rules.providers.reload")
	}
	update := m.ruleProviders.updates[provider.Name]
	busy := update.state == "queued" || update.state == "updating"
	switch update.state {
	case "queued":
		label = m.tr("rules.providers.queued")
	case "updating":
		label = m.tr("rules.providers.updating")
	case "failed":
		label = m.tr("rules.providers.retry")
	}
	return label, !busy && !m.busy && m.ruleProviders.active && m.ruleProvidersVisible()
}

func (m Model) ruleProviderUpdateButton(provider mihomo.RuleProvider, width int) string {
	label, enabled := m.ruleProviderUpdateLabel(provider)
	// Keep a name cell, the row gutter and the gap beside a complete button.
	// Smaller rows retain keyboard updates instead of an invisible hit target.
	if label == "" || width < ansi.StringWidth(label)+8 {
		return ""
	}
	options := dashboardButtonOptions("rules.provider-update."+provider.Name, m.navigationOptions(), false, raised)
	options.Dim, options.Disabled = false, !enabled
	update := m.ruleProviders.updates[provider.Name]
	options.Busy = update.state == "queued" || update.state == "updating"
	return renderButton(label, options)
}

func (m Model) ruleProviderRows(provider mihomo.RuleProvider, width int) []string {
	id := "rules.provider." + provider.Name
	surface := raised
	if m.focus == id {
		surface = panel
	}
	if m.hover == id {
		surface = hoverBackground
	}
	marker, nextMarker, markerColor := "  ", "  ", focus
	if m.focus == id {
		marker, nextMarker, markerColor = "› ", "  ", focus
	}
	button := m.ruleProviderUpdateButton(provider, width)
	nameWidth := max(0, width-2)
	if button != "" {
		nameWidth = max(0, nameWidth-ansi.StringWidth(button)-1)
	}
	first := themed(marker, markerColor, surface, false) + themed(cellPad(clean(provider.Name), nameWidth), text, surface, false)
	if button != "" {
		first += themed(" ", text, surface, false) + button
	}
	metadata := []string{clean(provider.VehicleType), clean(provider.Behavior)}
	if provider.Format != "" {
		metadata = append(metadata, clean(provider.Format))
	}
	metadata = append(metadata, m.tr("rules.providers.entries", provider.RuleCount))
	second := strings.Join(metadata, " · ")
	when := m.tr("rules.providers.unknown_time")
	if !provider.UpdatedAt.IsZero() {
		when = provider.UpdatedAt.Local().Format("2006-01-02 15:04:05")
	}
	third, color := m.tr("rules.providers.updated_at", when), muted
	update := m.ruleProviders.updates[provider.Name]
	switch update.state {
	case "queued":
		third += " · " + m.tr("rules.providers.queued")
	case "updating":
		third += " · " + m.tr("rules.providers.updating")
	case "success":
		third += " · " + m.tr("rules.providers.success")
		color = success
	case "failed":
		third = m.tr("rules.providers.failed", m.localizeError(update.err))
		color = errorColor
	default:
		if strings.EqualFold(provider.VehicleType, "Inline") {
			third += " · " + m.tr("rules.providers.inline_readonly")
		}
	}
	prefix := themed(nextMarker, markerColor, surface, false)
	return []string{
		themed(cellPad(first, width), text, surface, false),
		themed(cellPad(prefix+themed(cellCrop(second, max(0, width-2)), muted, surface, false), width), text, surface, false),
		themed(cellPad(prefix+themed(cellCrop(clean(third), max(0, width-2)), color, surface, false), width), text, surface, false),
	}
}

func (m Model) ruleProvidersBodyWindow(width, start, height int) ruleBodyWindow {
	p := m.ruleProviders
	var b dashboardBlock
	add := func(value, color string) {
		b.lines = append(b.lines, strings.Split(wrapText(themed(clean(value), color, raised, false), max(1, width)), "\n")...)
	}
	if p.err != nil {
		add(m.localizeError(p.err), errorColor)
	} else if p.snapshot.Error != "" {
		add(m.locale.Diagnostic(p.snapshot.Error, p.snapshot.Diagnostic), errorColor)
	}
	if !p.received || p.snapshot.State != "ready" {
		add(m.ruleProvidersStatus(), muted)
		if p.received {
			options := dashboardButtonOptions("rules.providers-reload", m.navigationOptions(), false, raised)
			options.Dim, options.Disabled = false, p.pending || m.busy || !p.active
			label := renderButton(m.tr("rules.providers.retry"), options)
			if !options.Disabled {
				b.targets = append(b.targets, hitRegion{ID: "rules.providers-reload", Y: len(b.lines), W: min(width, ansi.StringWidth(label)), H: 1})
			}
			b.lines = append(b.lines, cellCrop(label, width))
		}
	}
	list := m.filteredRuleProviders()
	if p.received && p.snapshot.State == "ready" && len(list) == 0 {
		message := m.tr("rules.providers.empty")
		if strings.TrimSpace(p.search.Value()) != "" {
			message = m.tr("rules.providers.no_matches")
		}
		add(message, muted)
	}
	if len(list) == 0 {
		return ruleBodyWindow{dashboardBlock: b, total: len(b.lines), prefix: len(b.lines)}
	}
	prefix := len(b.lines)
	total := prefix + len(list)*4 - 1
	if height < 0 {
		start, height = 0, total
	} else {
		start = max(0, min(start, max(0, total-height)))
	}
	end := min(total, start+height)
	leading := b.lines
	b.lines = nil
	for index := max(0, start); index < min(prefix, end); index++ {
		b.lines = append(b.lines, leading[index])
	}
	for index, provider := range list {
		row := prefix + index*4
		label, enabled := m.ruleProviderUpdateLabel(provider)
		if enabled && width >= ansi.StringWidth(label)+8 {
			// Buttons have a two-cell focus gutter and one cell of face padding
			// on either side, in both color and plain modes. Geometry needs no ANSI.
			buttonWidth := ansi.StringWidth(label) + 4
			b.targets = append(b.targets, hitRegion{ID: "rules.provider-update." + provider.Name, X: max(0, width-buttonWidth), Y: row, W: buttonWidth, H: 1})
		}
		b.targets = append(b.targets, hitRegion{ID: "rules.provider." + provider.Name, Y: row, W: width, H: 3})
		if row >= end || row+4 <= start {
			continue
		}
		if row+3 > start {
			for offset, line := range m.ruleProviderRows(provider, width) {
				if row+offset >= start && row+offset < end {
					b.lines = append(b.lines, line)
				}
			}
		}
		if index < len(list)-1 && row+3 >= start && row+3 < end {
			b.lines = append(b.lines, themed(strings.Repeat("─", max(0, width)), divider, raised, false))
		}
	}
	return ruleBodyWindow{dashboardBlock: b, start: start, total: total, prefix: prefix}
}

// Shared by the page header and update-all action guards.
func (m Model) ruleProvidersCanUpdateAll() bool {
	if !m.ruleProvidersVisible() || !m.ruleProviders.active || !m.ruleProviders.received || m.ruleProviders.snapshot.State != "ready" || m.ruleProviders.running() > 0 || len(m.ruleProviders.queue) > 0 {
		return false
	}
	for _, provider := range m.ruleProviders.snapshot.Providers {
		update := m.ruleProviders.updates[provider.Name]
		if !strings.EqualFold(provider.VehicleType, "Inline") && update.state != "queued" && update.state != "updating" {
			return true
		}
	}
	return false
}

// The provider viewport is separate from the effective-rule list underneath it.
func (m *Model) renderRuleProviders(base string) (string, []hitRegion) {
	p := &m.ruleProviders
	w, h, x, y := m.width, m.height, 0, 0
	modal := w >= 110 && h >= 30
	if modal {
		w, h = min(96, w-12), min(34, h-4)
		x, y = (m.width-w)/2, (m.height-h)/2
	} else {
		base = ""
	}
	p.bounds = hitRegion{X: x, Y: y, W: w, H: h}
	p.controls, p.rowTargets = nil, nil
	p.bodyBounds = hitRegion{}
	if w <= 0 || h <= 0 {
		return base, nil
	}
	rows := make([]string, h)
	for i := range rows {
		rows[i] = themed(strings.Repeat(" ", w), text, raised, false)
	}
	button := func(id, label string, disabled, primary bool) string {
		kind := buttonSecondary
		if primary {
			kind = buttonPrimary
		}
		return renderButton(label, buttonOptions{Kind: kind, Surface: raised, Focused: m.focus == id,
			Hovered: m.hover == id, Disabled: disabled, Plain: m.buttonPlain()})
	}
	close := button("rules.providers-back", m.tr("rules.providers.close"), false, false)
	if w < 24 || h < 13 {
		putRows(rows, 0, 0, themed(cellCrop(m.tr("rules.providers.title"), w), text, raised, false), w)
		closeY := h - 1
		putRows(rows, 0, closeY, cellCrop(close, w), w)
		hits := []hitRegion{{ID: "rules.providers-back", X: x, Y: y + closeY, W: min(w, ansi.StringWidth(close)), H: 1}}
		p.controls = hits
		return overlay(base, strings.Join(rows, "\n"), x, y, m.width, m.height), hits
	}
	inner := w - 6
	for i := 1; i < h-1; i++ {
		putRows(rows, 0, i, themed("│", border, raised, false), w)
		putRows(rows, w-1, i, themed("│", border, raised, false), w)
	}
	putRows(rows, 0, 0, themed("╭"+strings.Repeat("─", w-2)+"╮", border, raised, false), w)
	putRows(rows, 0, h-1, themed("╰"+strings.Repeat("─", w-2)+"╯", border, raised, false), w)
	allEnabled := m.ruleProvidersCanUpdateAll()
	all := button("rules.providers-all", m.tr("rules.providers.update_all"), !allEnabled, true)
	allWidth := min(inner, ansi.StringWidth(all))
	putRows(rows, 3, 1, themed(cellCrop(m.tr("rules.providers.title"), max(1, inner-allWidth-2)), text, raised, false), w)
	putRows(rows, w-3-allWidth, 1, all, w)
	var hits []hitRegion
	if allEnabled {
		hit := hitRegion{ID: "rules.providers-all", X: x + w - 3 - allWidth, Y: y + 1, W: allWidth, H: 1}
		hits = append(hits, hit)
		p.controls = append(p.controls, hit)
	}
	resizeSearchInput(&p.search, searchInputWidth(inner))
	search := m.ruleProvidersToolbar(inner, 4)
	for i, line := range search.lines {
		putRows(rows, 3, 3+i, themed(cellPad(line, inner), text, raised, false), w)
	}
	var clearTarget hitRegion
	for _, target := range search.targets {
		target.X, target.Y = target.X+x+3, target.Y+y+3
		hits = append(hits, target)
		if target.ID == "rules.providers-clear" {
			clearTarget = target
		} else {
			p.controls = append(p.controls, target)
		}
	}
	if clearTarget.ID != "" {
		p.controls = append(p.controls, clearTarget)
	}
	putRows(rows, 2, 7, themed(strings.Repeat("─", w-4), divider, raised, false), w)
	bodyY, bodyH := 8, max(1, h-12)
	p.bodyBounds = hitRegion{X: x + 3, Y: y + bodyY, W: inner, H: bodyH}
	resized := p.viewport.Width() != inner || p.viewport.Height() != bodyH
	p.viewport.SetWidth(inner)
	p.viewport.SetHeight(bodyH)
	doc := m.ruleProvidersBodyWindow(inner, 0, -1)
	p.rowTargets = doc.targets
	p.viewport.SetContent(strings.Join(doc.lines, "\n"))
	if resized && (strings.HasPrefix(m.focus, "rules.provider.") || strings.HasPrefix(m.focus, "rules.provider-update.")) {
		m.revealRuleProvider()
	}
	for i, line := range strings.Split(p.viewport.View(), "\n") {
		if i >= bodyH {
			break
		}
		putRows(rows, 3, bodyY+i, themed(cellPad(line, inner), text, raised, false), w)
	}
	for _, target := range doc.targets {
		target.X += p.bodyBounds.X
		target.Y += p.bodyBounds.Y - p.viewport.YOffset()
		p.controls = append(p.controls, target)
		if clipped, ok := clipRegion(target, p.bodyBounds.X, p.bodyBounds.Y, inner, bodyH); ok {
			hits = append(hits, clipped)
		}
	}
	putRows(rows, 2, h-4, themed(strings.Repeat("─", w-4), divider, raised, false), w)
	closeWidth := min(inner, ansi.StringWidth(close))
	putRows(rows, w-3-closeWidth, h-2, close, w)
	closeHit := hitRegion{ID: "rules.providers-back", X: x + w - 3 - closeWidth, Y: y + h - 2, W: closeWidth, H: 1}
	hits, p.controls = append(hits, closeHit), append(p.controls, closeHit)
	if room := inner - closeWidth - 2; room > 0 {
		putRows(rows, 3, h-2, helpText(cellCrop(m.tr("rules.providers.modal_help"), room), raised, false), w)
	}
	if p.viewport.TotalLineCount() > bodyH {
		putRows(rows, w-2, bodyY+bodyH-1, themed("↕", info, raised, false), w)
	}
	return overlay(base, strings.Join(rows, "\n"), x, y, m.width, m.height), hits
}
