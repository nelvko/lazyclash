package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

type providerKind string

const (
	providerRules   providerKind = "rules"
	providerProxies providerKind = "proxies"
)

type ruleProviderBackend interface {
	RuleProviders(context.Context) (app.RuleProviderSnapshot, error)
	UpdateRuleProvider(context.Context, string) error
}

type proxyProviderBackend interface {
	ProxyProviders(context.Context) (app.ProxyProviderSnapshot, error)
	UpdateProxyProvider(context.Context, string) error
}

// providerRow is presentation metadata shared by both provider kinds. Raw
// provider payloads, URLs and node credentials never enter the panel.
type providerRow struct {
	Name, Type, VehicleType, Behavior, Format string
	Count                                     int
	UpdatedAt                                 time.Time
	updateable                                bool
}

type providerSnapshot struct {
	State      string
	Providers  []providerRow
	ObservedAt time.Time
	Error      string
	Diagnostic error
}

func ruleProviderSnapshot(raw app.RuleProviderSnapshot) providerSnapshot {
	s := providerSnapshot{State: raw.State, ObservedAt: raw.ObservedAt, Error: raw.Error, Diagnostic: raw.Diagnostic}
	for _, p := range raw.Providers {
		s.Providers = append(s.Providers, providerRow{Name: p.Name, Type: p.Type, VehicleType: p.VehicleType,
			Behavior: p.Behavior, Format: p.Format, Count: p.RuleCount, UpdatedAt: p.UpdatedAt,
			updateable: !strings.EqualFold(p.VehicleType, "Inline")})
	}
	return s
}

func proxyProviderSnapshot(raw app.ProxyProviderSnapshot) providerSnapshot {
	s := providerSnapshot{State: raw.State, ObservedAt: raw.ObservedAt, Error: raw.Error, Diagnostic: raw.Diagnostic}
	for _, p := range raw.Providers {
		s.Providers = append(s.Providers, providerRow{Name: p.Name, Type: p.Type, VehicleType: p.VehicleType,
			Count: p.ProxyCount, UpdatedAt: p.UpdatedAt,
			updateable: strings.EqualFold(p.Type, "Proxy") && (strings.EqualFold(p.VehicleType, "HTTP") || strings.EqualFold(p.VehicleType, "File"))})
	}
	return s
}

func (m Model) providersSupported(kind providerKind) bool {
	if kind == providerProxies {
		_, ok := m.backend.(proxyProviderBackend)
		return ok
	}
	_, ok := m.backend.(ruleProviderBackend)
	return ok
}

func (m Model) readProviders(ctx context.Context, kind providerKind) (providerSnapshot, error) {
	if kind == providerProxies {
		b := m.backend.(proxyProviderBackend)
		s, err := b.ProxyProviders(ctx)
		return proxyProviderSnapshot(s), err
	}
	b := m.backend.(ruleProviderBackend)
	s, err := b.RuleProviders(ctx)
	return ruleProviderSnapshot(s), err
}

func (m Model) updateProvider(ctx context.Context, kind providerKind, name string) error {
	if kind == providerProxies {
		return m.backend.(proxyProviderBackend).UpdateProxyProvider(ctx, name)
	}
	return m.backend.(ruleProviderBackend).UpdateRuleProvider(ctx, name)
}

func (m Model) providerSource() string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%s", m.snapshot.Generation, m.snapshot.Applied, m.snapshot.Service.PID, m.snapshot.RunningVersion)
}

func (m Model) providerTitle() string { return m.tr("providers.title." + string(m.providers.kind)) }
func (m Model) providerSearchPlaceholder() string {
	return m.tr("providers.search." + string(m.providers.kind))
}

type providerUpdate struct {
	id     uint64
	state  string
	err    error
	cancel context.CancelFunc
}

type providerPanelState struct {
	kind                        providerKind
	source                      string
	snapshot                    providerSnapshot
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
	updates                     map[string]providerUpdate
	queue                       []string
}

type providersResultMsg struct {
	kind                providerKind
	source              string
	generation, request uint64
	snapshot            providerSnapshot
	err                 error
}

type providersTickMsg struct {
	kind                providerKind
	source              string
	generation, request uint64
}

type providerUpdateResultMsg struct {
	kind               providerKind
	source             string
	generation, update uint64
	name               string
	err                error
}

func newProviderPanelState(locale i18n.Localizer) providerPanelState {
	search := textinput.New()
	search.SetVirtualCursor(false)
	search.CharLimit = 512
	search.Placeholder = locale.T("providers.search.rules")
	styleInput(&search, inputBackground, false)
	return providerPanelState{kind: providerRules, search: search, viewport: viewport.New(), updates: make(map[string]providerUpdate)}
}

func (m Model) providersVisible() bool {
	return m.page == string(m.providers.kind) && m.providers.open && !m.busy && !m.quitting && !m.sidebarOpen && !m.helpOpen && !m.ports.open && !m.wizard.open && !m.wizard.networkOpen && m.form == "" && !m.notification.panel.open && m.ctx.Err() == nil
}

func (m *Model) openProviders(kind providerKind) tea.Cmd {
	m.stopProviders()
	if m.providers.kind != kind {
		m.providers.search.Reset()
		m.providers.selected, m.providers.selectedIndex = "", 0
	}
	m.providers.kind = kind
	m.providers.search.Placeholder = m.providerSearchPlaceholder()
	m.rules.editing = false
	m.rules.search.Blur()
	m.stopRules()
	m.providers.open = true
	m.providers.viewport.GotoTop()
	m.sidebarOpen = false
	m.focus, m.hover = "providers.search", ""
	return m.syncProviders()
}

func (m *Model) closeProviders() {
	m.stopProviders()
	m.providers.open = false
	m.providers.controls, m.providers.rowTargets = nil, nil
	m.focus, m.hover = string(m.providers.kind)+".providers", ""
}

func (m *Model) stopProviders() {
	p := &m.providers
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
	p.updates, p.queue = make(map[string]providerUpdate), nil
}

func (p providerPanelState) running() int {
	count := 0
	for _, update := range p.updates {
		if update.state == "updating" {
			count++
		}
	}
	return count
}

func (m *Model) syncProviders() tea.Cmd {
	if !m.providersVisible() {
		m.stopProviders()
		return nil
	}
	p := &m.providers
	if p.active && p.source != m.providerSource() {
		m.stopProviders()
	}
	if p.active {
		return nil
	}
	p.source = m.providerSource()
	p.active = true
	p.generation++
	// A reopened panel must observe the current core before its old names can
	// become mutation targets. Query and selection survive; cached rows do not.
	p.received, p.err, p.snapshot = false, nil, providerSnapshot{}
	if !m.providersSupported(p.kind) {
		p.received = true
		p.snapshot = providerSnapshot{State: "unsupported"}
		return nil
	}
	return m.providersCmd()
}

func (m *Model) providersCmd() tea.Cmd {
	p := &m.providers
	if !m.providersSupported(p.kind) || p.pending || !p.active || !m.providersVisible() {
		return nil
	}
	p.request++
	ctx, cancel := context.WithTimeout(m.ctx, 8*time.Second)
	p.cancel, p.pending = cancel, true
	generation, request, kind, source := p.generation, p.request, p.kind, p.source
	return func() tea.Msg {
		snapshot, err := m.readProviders(ctx, kind)
		return providersResultMsg{kind: kind, source: source, generation: generation, request: request, snapshot: snapshot, err: err}
	}
}

func (m *Model) acceptProviders(result providersResultMsg) tea.Cmd {
	p := &m.providers
	if !p.active || !p.pending || result.kind != p.kind || result.source != p.source || p.source != m.providerSource() || result.generation != p.generation || result.request != p.request {
		return nil
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.pending, p.received, p.err = false, true, result.err
	p.snapshot = result.snapshot
	if result.err != nil {
		p.snapshot = providerSnapshot{State: "unavailable", Error: result.err.Error(), Diagnostic: result.err}
	}
	// Missing or unavailable sources cannot keep queued work from an older
	// observation. Running responses are rejected unless their update ID remains.
	for name, update := range p.updates {
		if _, ok := m.providerByName(name); !ok {
			if update.cancel != nil {
				update.cancel()
			}
			delete(p.updates, name)
		}
	}
	m.reconcileProviders()
	generation, request, kind, source := p.generation, p.request, p.kind, p.source
	return tea.Batch(m.pumpProviderUpdates(), tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return providersTickMsg{kind: kind, source: source, generation: generation, request: request}
	}))
}

func (m *Model) retryProviders() tea.Cmd {
	p := &m.providers
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.pending = false
	return m.providersCmd()
}

func (m Model) filteredProviders() []providerRow {
	p := m.providers
	if !p.received || p.snapshot.State != "ready" {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(p.search.Value()))
	if query == "" {
		return p.snapshot.Providers
	}
	var filtered []providerRow
	for _, provider := range p.snapshot.Providers {
		value := strings.Join([]string{provider.Name, provider.Type, provider.VehicleType, provider.Behavior, provider.Format}, " ")
		if strings.Contains(strings.ToLower(value), query) {
			filtered = append(filtered, provider)
		}
	}
	return filtered
}

func (m Model) providerByName(name string) (providerRow, bool) {
	if m.providers.received && m.providers.snapshot.State == "ready" {
		for _, provider := range m.providers.snapshot.Providers {
			if provider.Name == name {
				return provider, true
			}
		}
	}
	return providerRow{}, false
}

func (m *Model) reconcileProviders() {
	p := &m.providers
	list := m.filteredProviders()
	for index, provider := range list {
		if provider.Name == p.selected {
			previous := p.selectedIndex
			p.selectedIndex = index
			if previous != index && m.focus == "provider.row."+p.selected {
				m.revealProvider()
			}
			return
		}
	}
	p.selected = ""
	if len(list) > 0 {
		p.selectedIndex = max(0, min(p.selectedIndex, len(list)-1))
		p.selected = list[p.selectedIndex].Name
	}
	if strings.HasPrefix(m.focus, "provider.row.") {
		m.focus = "providers.search"
		if p.selected != "" {
			m.focus = "provider.row." + p.selected
			m.revealProvider()
		}
	}
}

func (m *Model) startProviderUpdates(names []string) tea.Cmd {
	p := &m.providers
	if !p.active || !m.providersVisible() || p.source != m.providerSource() {
		return nil
	}
	if p.updates == nil {
		p.updates = make(map[string]providerUpdate)
	}
	for _, name := range names {
		provider, ok := m.providerByName(name)
		if !ok || !provider.updateable {
			continue
		}
		if update := p.updates[name]; update.state == "queued" || update.state == "updating" {
			continue
		}
		p.nextID++
		p.updates[name] = providerUpdate{id: p.nextID, state: "queued"}
		p.queue = append(p.queue, name)
	}
	return m.pumpProviderUpdates()
}

func (m *Model) pumpProviderUpdates() tea.Cmd {
	p := &m.providers
	if !m.providersSupported(p.kind) || !p.active || !m.providersVisible() {
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
		generation, id, kind, source := p.generation, update.id, p.kind, p.source
		commands = append(commands, func() tea.Msg {
			return providerUpdateResultMsg{kind: kind, source: source, generation: generation, update: id, name: name, err: m.updateProvider(ctx, kind, name)}
		})
	}
	return tea.Batch(commands...)
}

func (m *Model) acceptProviderUpdate(result providerUpdateResultMsg) tea.Cmd {
	// A canceled request may already have changed the core. Observe that core
	// independently of whether the panel still accepts this row completion.
	// A response from a different applied profile or process cannot trigger it.
	var observed tea.Cmd
	if result.kind == providerProxies && result.source == m.providerSource() {
		observed = m.refreshCoreStatus()
	}
	p := &m.providers
	update, exists := p.updates[result.name]
	if !p.active || result.kind != p.kind || result.source != p.source || p.source != m.providerSource() || result.generation != p.generation || !exists || update.id != result.update || update.state != "updating" {
		return observed
	}
	if update.cancel != nil {
		update.cancel()
	}
	update.cancel, update.err, update.state = nil, result.err, "success"
	if result.err != nil {
		update.state = "failed"
	}
	p.updates[result.name] = update
	next := m.pumpProviderUpdates()
	if p.running() == 0 && len(p.queue) == 0 {
		return tea.Batch(observed, next, m.retryProviders())
	}
	return tea.Batch(observed, next)
}

func (m *Model) focusProvidersSearch() tea.Cmd {
	m.providers.editing = true
	m.focus = "providers.search"
	m.providers.viewport.GotoTop()
	return m.providers.search.Focus()
}

func (m *Model) providersSearchKey(key tea.KeyPressMsg) tea.Cmd {
	p := &m.providers
	switch key.String() {
	case "enter", "esc":
		p.editing = false
		p.search.Blur()
		m.reconcileProviders()
		m.focus = "providers.search"
		if p.selected != "" {
			m.focus = "provider.row." + p.selected
			m.revealProvider()
		}
		return nil
	case "tab", "shift+tab":
		p.editing = false
		p.search.Blur()
		delta := 1
		if key.String() == "shift+tab" {
			delta = -1
		}
		m.cycleProvidersFocus(delta)
		return nil
	}
	var command tea.Cmd
	p.search, command = p.search.Update(key)
	p.viewport.GotoTop()
	m.reconcileProviders()
	return command
}

func (m *Model) providersKey(key tea.KeyPressMsg) (bool, tea.Cmd) {
	if !m.providers.open {
		return false, nil
	}
	if m.providers.editing {
		return true, m.providersSearchKey(key)
	}
	switch key.String() {
	case "/":
		return true, m.focusProvidersSearch()
	case "esc", "q":
		m.closeProviders()
		return true, nil
	case "tab", "shift+tab":
		delta := 1
		if key.String() == "shift+tab" {
			delta = -1
		}
		m.cycleProvidersFocus(delta)
		return true, nil
	case "enter", "space", " ":
		return true, m.providersActivate(m.focus)
	case "g", "ctrl+r":
		return true, m.retryProviders()
	case "u":
		return true, m.startProviderUpdates([]string{m.providers.selected})
	case "up", "k", "down", "j", "home", "end", "pgup", "pgdown":
		list := m.filteredProviders()
		if len(list) == 0 {
			return true, nil
		}
		index := m.providers.selectedIndex
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
			index -= max(1, m.providers.viewport.Height()/4)
		case "pgdown":
			index += max(1, m.providers.viewport.Height()/4)
		}
		index = max(0, min(index, len(list)-1))
		m.providers.selectedIndex, m.providers.selected = index, list[index].Name
		m.focus = "provider.row." + list[index].Name
		m.revealProvider()
		return true, nil
	}
	return true, nil
}

func (m *Model) revealProvider() {
	p := &m.providers
	for _, target := range p.rowTargets {
		if target.ID != m.focus && target.ID != "provider.row."+p.selected {
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

func (m *Model) cycleProvidersFocus(delta int) {
	p := &m.providers
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
	if strings.HasPrefix(m.focus, "provider.row.") {
		m.providersActivate(m.focus)
	} else if strings.HasPrefix(m.focus, "provider.update.") {
		name := strings.TrimPrefix(m.focus, "provider.update.")
		for i, provider := range m.filteredProviders() {
			if provider.Name == name {
				p.selectedIndex, p.selected = i, name
				break
			}
		}
	}
	if strings.HasPrefix(m.focus, "provider.row.") || strings.HasPrefix(m.focus, "provider.update.") {
		m.revealProvider()
	}
}

func (m *Model) providersMouse(event tea.MouseMsg) tea.Cmd {
	p, point := &m.providers, event.Mouse()
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
			return m.providersActivate(hit.ID)
		}
		break
	}
	return nil
}

func (m *Model) providersActivate(id string) tea.Cmd {
	if id != "providers.search" {
		m.providers.editing = false
		m.providers.search.Blur()
	}
	switch {
	case id == "providers.search":
		return m.focusProvidersSearch()
	case id == "providers.clear":
		m.providers.search.Reset()
		return m.focusProvidersSearch()
	case id == "providers.back":
		m.closeProviders()
	case id == "providers.reload":
		return m.retryProviders()
	case id == "providers.all":
		if !m.providersCanUpdateAll() {
			return nil
		}
		var names []string
		for _, provider := range m.providers.snapshot.Providers {
			names = append(names, provider.Name)
		}
		return m.startProviderUpdates(names)
	case strings.HasPrefix(id, "provider.update."):
		name := strings.TrimPrefix(id, "provider.update.")
		for index, provider := range m.filteredProviders() {
			if provider.Name == name {
				m.providers.selectedIndex, m.providers.selected = index, name
				break
			}
		}
		return m.startProviderUpdates([]string{name})
	case strings.HasPrefix(id, "provider.row."):
		name := strings.TrimPrefix(id, "provider.row.")
		for index, provider := range m.filteredProviders() {
			if provider.Name == name {
				m.providers.selectedIndex, m.providers.selected = index, name
				m.focus = id
				break
			}
		}
	}
	return nil
}
