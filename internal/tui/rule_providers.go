package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type ruleProviderBackend interface {
	RuleProviders(context.Context) (app.RuleProviderSnapshot, error)
	UpdateRuleProvider(context.Context, string) error
}

type ruleProviderUpdate struct {
	id     uint64
	state  string
	err    error
	cancel context.CancelFunc
}

type ruleProvidersState struct {
	snapshot                    app.RuleProviderSnapshot
	search                      textinput.Model
	selected                    string
	selectedIndex               int
	open                        bool
	viewport                    viewport.Model
	controls, rowTargets        []hitRegion
	bounds, bodyBounds          hitRegion
	editing, active, pending    bool
	received                    bool
	generation, request, nextID uint64
	cancel                      context.CancelFunc
	err                         error
	updates                     map[string]ruleProviderUpdate
	queue                       []string
}

type ruleProvidersResultMsg struct {
	generation, request uint64
	snapshot            app.RuleProviderSnapshot
	err                 error
}

type ruleProvidersTickMsg struct{ generation, request uint64 }

type ruleProviderUpdateResultMsg struct {
	generation, update uint64
	name               string
	err                error
}

func newRuleProvidersState(locale i18n.Localizer) ruleProvidersState {
	search := textinput.New()
	search.CharLimit = 512
	search.Placeholder = locale.T("rules.providers.search_placeholder")
	styleInput(&search, inputBackground, false)
	return ruleProvidersState{search: search, viewport: viewport.New(), updates: make(map[string]ruleProviderUpdate)}
}

func (m Model) ruleProvidersVisible() bool {
	return m.page == "rules" && m.ruleProviders.open && !m.busy && !m.quitting && !m.sidebarOpen && !m.helpOpen && !m.ports.open && !m.wizard.open && !m.wizard.networkOpen && m.form == "" && !m.notification.panel.open && m.ctx.Err() == nil
}

func (m *Model) openRuleProviders() tea.Cmd {
	m.rules.editing = false
	m.rules.search.Blur()
	m.stopRules()
	m.ruleProviders.open = true
	m.ruleProviders.viewport.GotoTop()
	m.sidebarOpen = false
	m.focus, m.hover = "rules.providers-search", ""
	return m.syncRuleProviders()
}

func (m *Model) closeRuleProviders() {
	m.stopRuleProviders()
	m.ruleProviders.open = false
	m.ruleProviders.controls, m.ruleProviders.rowTargets = nil, nil
	m.focus, m.hover = "rules.providers", ""
}

func (m *Model) stopRuleProviders() {
	p := &m.ruleProviders
	if !p.active && !p.pending && !p.editing && p.cancel == nil && len(p.queue) == 0 && len(p.updates) == 0 {
		return
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	for _, update := range p.updates {
		if update.cancel != nil {
			update.cancel()
		}
	}
	if p.active || p.pending || len(p.queue) > 0 || p.running() > 0 {
		p.generation++
	}
	p.active, p.pending, p.editing = false, false, false
	p.search.Blur()
	p.updates, p.queue = make(map[string]ruleProviderUpdate), nil
}

func (p ruleProvidersState) running() int {
	count := 0
	for _, update := range p.updates {
		if update.state == "updating" {
			count++
		}
	}
	return count
}

func (m *Model) syncRuleProviders() tea.Cmd {
	if !m.ruleProvidersVisible() {
		m.stopRuleProviders()
		return nil
	}
	p := &m.ruleProviders
	if p.active {
		return nil
	}
	p.active = true
	p.generation++
	// A reopened panel must observe the current core before its old names can
	// become mutation targets. Query and selection survive; cached rows do not.
	p.received, p.err, p.snapshot = false, nil, app.RuleProviderSnapshot{}
	if _, ok := m.backend.(ruleProviderBackend); !ok {
		p.received = true
		p.snapshot = app.RuleProviderSnapshot{State: "unsupported"}
		return nil
	}
	return m.ruleProvidersCmd()
}

func (m *Model) ruleProvidersCmd() tea.Cmd {
	p := &m.ruleProviders
	backend, ok := m.backend.(ruleProviderBackend)
	if !ok || p.pending || !p.active || !m.ruleProvidersVisible() {
		return nil
	}
	p.request++
	ctx, cancel := context.WithTimeout(m.ctx, 8*time.Second)
	p.cancel, p.pending = cancel, true
	generation, request := p.generation, p.request
	return func() tea.Msg {
		snapshot, err := backend.RuleProviders(ctx)
		return ruleProvidersResultMsg{generation: generation, request: request, snapshot: snapshot, err: err}
	}
}

func (m *Model) acceptRuleProviders(result ruleProvidersResultMsg) tea.Cmd {
	p := &m.ruleProviders
	if !p.active || !p.pending || result.generation != p.generation || result.request != p.request {
		return nil
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.pending, p.received, p.err = false, true, result.err
	p.snapshot = result.snapshot
	if result.err != nil {
		p.snapshot = app.RuleProviderSnapshot{State: "unavailable", Error: result.err.Error(), Diagnostic: result.err}
	}
	// Missing or unavailable sources cannot keep queued work from an older
	// observation. Running responses are rejected unless their update ID remains.
	for name, update := range p.updates {
		if _, ok := m.ruleProviderByName(name); !ok {
			if update.cancel != nil {
				update.cancel()
			}
			delete(p.updates, name)
		}
	}
	m.reconcileRuleProviders()
	generation, request := p.generation, p.request
	return tea.Batch(m.pumpRuleProviderUpdates(), tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return ruleProvidersTickMsg{generation: generation, request: request}
	}))
}

func (m *Model) retryRuleProviders() tea.Cmd {
	p := &m.ruleProviders
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.pending = false
	return m.ruleProvidersCmd()
}

func (m Model) filteredRuleProviders() []mihomo.RuleProvider {
	p := m.ruleProviders
	if !p.received || p.snapshot.State != "ready" {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(p.search.Value()))
	if query == "" {
		return p.snapshot.Providers
	}
	var filtered []mihomo.RuleProvider
	for _, provider := range p.snapshot.Providers {
		value := strings.Join([]string{provider.Name, provider.Type, provider.VehicleType, provider.Behavior, provider.Format}, " ")
		if strings.Contains(strings.ToLower(value), query) {
			filtered = append(filtered, provider)
		}
	}
	return filtered
}

func (m Model) ruleProviderByName(name string) (mihomo.RuleProvider, bool) {
	if m.ruleProviders.received && m.ruleProviders.snapshot.State == "ready" {
		for _, provider := range m.ruleProviders.snapshot.Providers {
			if provider.Name == name {
				return provider, true
			}
		}
	}
	return mihomo.RuleProvider{}, false
}

func (m *Model) reconcileRuleProviders() {
	p := &m.ruleProviders
	list := m.filteredRuleProviders()
	for index, provider := range list {
		if provider.Name == p.selected {
			previous := p.selectedIndex
			p.selectedIndex = index
			if previous != index && m.focus == "rules.provider."+p.selected {
				m.revealRuleProvider()
			}
			return
		}
	}
	p.selected = ""
	if len(list) > 0 {
		p.selectedIndex = max(0, min(p.selectedIndex, len(list)-1))
		p.selected = list[p.selectedIndex].Name
	}
	if strings.HasPrefix(m.focus, "rules.provider.") {
		m.focus = "rules.providers-search"
		if p.selected != "" {
			m.focus = "rules.provider." + p.selected
			m.revealRuleProvider()
		}
	}
}

func (m *Model) startRuleProviderUpdates(names []string) tea.Cmd {
	p := &m.ruleProviders
	if !p.active || !m.ruleProvidersVisible() {
		return nil
	}
	if p.updates == nil {
		p.updates = make(map[string]ruleProviderUpdate)
	}
	for _, name := range names {
		provider, ok := m.ruleProviderByName(name)
		if !ok || strings.EqualFold(provider.VehicleType, "Inline") {
			continue
		}
		if update := p.updates[name]; update.state == "queued" || update.state == "updating" {
			continue
		}
		p.nextID++
		p.updates[name] = ruleProviderUpdate{id: p.nextID, state: "queued"}
		p.queue = append(p.queue, name)
	}
	return m.pumpRuleProviderUpdates()
}

func (m *Model) pumpRuleProviderUpdates() tea.Cmd {
	p := &m.ruleProviders
	backend, ok := m.backend.(ruleProviderBackend)
	if !ok || !p.active || !m.ruleProvidersVisible() {
		return nil
	}
	var commands []tea.Cmd
	for len(p.queue) > 0 && p.running() < 3 {
		name := p.queue[0]
		p.queue = p.queue[1:]
		update, exists := p.updates[name]
		if !exists || update.state != "queued" {
			continue
		}
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		update.state, update.cancel = "updating", cancel
		p.updates[name] = update
		generation, id := p.generation, update.id
		commands = append(commands, func() tea.Msg {
			return ruleProviderUpdateResultMsg{generation: generation, update: id, name: name, err: backend.UpdateRuleProvider(ctx, name)}
		})
	}
	return tea.Batch(commands...)
}

func (m *Model) acceptRuleProviderUpdate(result ruleProviderUpdateResultMsg) tea.Cmd {
	p := &m.ruleProviders
	update, exists := p.updates[result.name]
	if !p.active || result.generation != p.generation || !exists || update.id != result.update || update.state != "updating" {
		return nil
	}
	if update.cancel != nil {
		update.cancel()
	}
	update.cancel, update.err, update.state = nil, result.err, "success"
	if result.err != nil {
		update.state = "failed"
	}
	p.updates[result.name] = update
	next := m.pumpRuleProviderUpdates()
	if p.running() == 0 && len(p.queue) == 0 {
		return tea.Batch(next, m.retryRuleProviders())
	}
	return next
}

func (m *Model) focusRuleProvidersSearch() tea.Cmd {
	m.ruleProviders.editing = true
	m.focus = "rules.providers-search"
	m.ruleProviders.viewport.GotoTop()
	return m.ruleProviders.search.Focus()
}

func (m *Model) ruleProvidersSearchKey(key tea.KeyPressMsg) tea.Cmd {
	p := &m.ruleProviders
	switch key.String() {
	case "enter", "esc":
		p.editing = false
		p.search.Blur()
		m.reconcileRuleProviders()
		m.focus = "rules.providers-search"
		if p.selected != "" {
			m.focus = "rules.provider." + p.selected
			m.revealRuleProvider()
		}
		return nil
	case "tab", "shift+tab":
		p.editing = false
		p.search.Blur()
		delta := 1
		if key.String() == "shift+tab" {
			delta = -1
		}
		m.cycleRuleProvidersFocus(delta)
		return nil
	}
	var command tea.Cmd
	p.search, command = p.search.Update(key)
	p.viewport.GotoTop()
	m.reconcileRuleProviders()
	return command
}

func (m *Model) ruleProvidersKey(key tea.KeyPressMsg) (bool, tea.Cmd) {
	if !m.ruleProviders.open {
		return false, nil
	}
	if m.ruleProviders.editing {
		return true, m.ruleProvidersSearchKey(key)
	}
	switch key.String() {
	case "/":
		return true, m.focusRuleProvidersSearch()
	case "esc", "q":
		m.closeRuleProviders()
		return true, nil
	case "tab", "shift+tab":
		delta := 1
		if key.String() == "shift+tab" {
			delta = -1
		}
		m.cycleRuleProvidersFocus(delta)
		return true, nil
	case "enter", "space", " ":
		return true, m.ruleProvidersActivate(m.focus)
	case "g", "ctrl+r":
		return true, m.retryRuleProviders()
	case "u":
		return true, m.startRuleProviderUpdates([]string{m.ruleProviders.selected})
	case "up", "k", "down", "j", "home", "end", "pgup", "pgdown":
		list := m.filteredRuleProviders()
		if len(list) == 0 {
			return true, nil
		}
		index := m.ruleProviders.selectedIndex
		switch key.String() {
		case "up", "k":
			index--
		case "down", "j":
			index++
		case "home":
			index = 0
		case "end":
			index = len(list) - 1
		case "pgup":
			index -= max(1, m.ruleProviders.viewport.Height()/4)
		case "pgdown":
			index += max(1, m.ruleProviders.viewport.Height()/4)
		}
		index = max(0, min(index, len(list)-1))
		m.ruleProviders.selectedIndex, m.ruleProviders.selected = index, list[index].Name
		m.focus = "rules.provider." + list[index].Name
		m.revealRuleProvider()
		return true, nil
	}
	return true, nil
}

func (m *Model) revealRuleProvider() {
	p := &m.ruleProviders
	for _, target := range p.rowTargets {
		if target.ID != m.focus && target.ID != "rules.provider."+p.selected {
			continue
		}
		if target.H > p.viewport.Height() || target.Y < p.viewport.YOffset() {
			p.viewport.SetYOffset(target.Y)
		} else if target.Y+target.H > p.viewport.YOffset()+p.viewport.Height() {
			p.viewport.SetYOffset(target.Y + target.H - p.viewport.Height())
		}
		return
	}
}

func (m *Model) cycleRuleProvidersFocus(delta int) {
	p := &m.ruleProviders
	if len(p.controls) == 0 {
		return
	}
	index := -1
	for i, target := range p.controls {
		if target.ID == m.focus {
			index = i
			break
		}
	}
	index = (index + delta + len(p.controls)) % len(p.controls)
	m.focus = p.controls[index].ID
	p.editing = false
	p.search.Blur()
	if strings.HasPrefix(m.focus, "rules.provider.") {
		m.ruleProvidersActivate(m.focus)
	} else if strings.HasPrefix(m.focus, "rules.provider-update.") {
		name := strings.TrimPrefix(m.focus, "rules.provider-update.")
		for i, provider := range m.filteredRuleProviders() {
			if provider.Name == name {
				p.selectedIndex, p.selected = i, name
				break
			}
		}
	}
	if strings.HasPrefix(m.focus, "rules.provider.") || strings.HasPrefix(m.focus, "rules.provider-update.") {
		m.revealRuleProvider()
	}
}

func (m *Model) ruleProvidersMouse(event tea.MouseMsg) tea.Cmd {
	p, point := &m.ruleProviders, event.Mouse()
	inside := func(hit hitRegion) bool {
		return point.X >= hit.X && point.X < hit.X+hit.W && point.Y >= hit.Y && point.Y < hit.Y+hit.H
	}
	if !inside(p.bounds) {
		m.hover = ""
		return nil
	}
	if _, wheel := event.(tea.MouseWheelMsg); wheel {
		if inside(p.bodyBounds) {
			p.viewport, _ = p.viewport.Update(event)
		}
		m.hover = ""
		return nil
	}
	m.hover = ""
	for _, hit := range m.targets {
		if !inside(hit) {
			continue
		}
		m.hover = hit.ID
		if _, click := event.(tea.MouseClickMsg); click && point.Button == tea.MouseLeft {
			m.focus = hit.ID
			return m.ruleProvidersActivate(hit.ID)
		}
		break
	}
	return nil
}

func (m *Model) ruleProvidersActivate(id string) tea.Cmd {
	if id != "rules.providers-search" {
		m.ruleProviders.editing = false
		m.ruleProviders.search.Blur()
	}
	switch {
	case id == "rules.providers-search":
		return m.focusRuleProvidersSearch()
	case id == "rules.providers-back":
		m.closeRuleProviders()
	case id == "rules.providers-reload":
		return m.retryRuleProviders()
	case id == "rules.providers-all":
		if !m.ruleProvidersCanUpdateAll() {
			return nil
		}
		var names []string
		for _, provider := range m.ruleProviders.snapshot.Providers {
			names = append(names, provider.Name)
		}
		return m.startRuleProviderUpdates(names)
	case strings.HasPrefix(id, "rules.provider-update."):
		name := strings.TrimPrefix(id, "rules.provider-update.")
		for index, provider := range m.filteredRuleProviders() {
			if provider.Name == name {
				m.ruleProviders.selectedIndex, m.ruleProviders.selected = index, name
				break
			}
		}
		return m.startRuleProviderUpdates([]string{name})
	case strings.HasPrefix(id, "rules.provider."):
		name := strings.TrimPrefix(id, "rules.provider.")
		for index, provider := range m.filteredRuleProviders() {
			if provider.Name == name {
				m.ruleProviders.selectedIndex, m.ruleProviders.selected = index, name
				m.focus = id
				break
			}
		}
	}
	return nil
}
