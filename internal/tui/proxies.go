package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// Bubbles supplies the editor, spinner and key bindings. The adaptive grouped
// list uses the shell's scrolling and hit regions rather than a second viewport.
type proxiesState struct {
	expanded, detail, detailGroup string
	search                        textinput.Model
	editing, sortMenu             bool
	sort                          string
	order                         map[string][]string
	history                       []proxyLocation
	detailReturn                  proxyLocation
	sortReturn                    proxyLocation
	source                        string
	mode                          string
	results                       map[string]proxyMeasurement
	generation                    uint64
	testing                       bool
	cancel                        context.CancelFunc
	testCtx                       context.Context
	queue                         []proxyTestTarget
	inflight                      map[string]bool
	completed, total              int
	testGroup, testStatus         string
	testStatusVisible             bool
	pendingGroup, pendingNode     string
}

type proxyLocation struct {
	group, focus, query string
	identity            pageViewState
	scroll              int
}
type proxyTestTarget struct{ group, node, path string }
type proxyMeasurement struct {
	result mihomo.ProxyDelay
	path   string
	err    error
}
type proxyDelayMsg struct {
	generation uint64
	target     proxyTestTarget
	result     mihomo.ProxyDelay
	err        error
}
type proxyTestStatusExpiredMsg struct{ generation uint64 }
type proxyDelayBackend interface {
	TestProxyDelay(context.Context, string, string) (mihomo.ProxyDelay, error)
}

func newProxiesState() proxiesState {
	input := textinput.New()
	input.CharLimit = 256
	input.Prompt = "/ "
	styleInput(&input, inputBackground, false)
	return proxiesState{search: input, sort: "default", order: make(map[string][]string), results: make(map[string]proxyMeasurement)}
}

func (m Model) proxiesReady() bool {
	return m.snapshotReady && m.snapshot.Installed && m.snapshot.Service.Active && m.snapshot.ServiceError == "" && m.snapshot.APIError == ""
}
func (m Model) proxiesNarrow() bool { return m.width < 100 }
func (m Model) proxyGroup(name string) (int, mihomo.Group, bool) {
	for i, g := range m.snapshot.Groups {
		if g.Name == name {
			return i, g, true
		}
	}
	return 0, mihomo.Group{}, false
}
func (m Model) proxyLocation() proxyLocation {
	loc := proxyLocation{group: m.proxies.expanded, focus: m.focus, query: m.proxies.search.Value(), scroll: m.scroll}
	if _, g, n, ok := m.proxyAtFocus(); ok && (strings.HasPrefix(m.focus, "node.") || strings.HasPrefix(m.focus, "group.") || strings.HasPrefix(m.focus, "proxies.reference.")) {
		loc.identity = pageViewState{group: g.Name, node: n, focus: m.focus}
	}
	return loc
}
func (m *Model) restoreProxyLocation(loc proxyLocation) {
	m.proxies.expanded = loc.group
	m.proxies.search.SetValue(loc.query)
	m.focus, m.scroll = loc.focus, loc.scroll
	if loc.identity.group != "" {
		if restored := m.restoredContentFocus(loc.identity); restored != "" {
			m.focus = restored
		} else {
			m.focus = "content"
		}
	}
	m.rememberPageView()
}
func (m Model) proxyAtFocus() (int, mihomo.Group, string, bool) {
	var gi, ni int
	nodeFocus := strings.Replace(m.focus, "proxies.reference.", "node.", 1)
	if _, err := fmt.Sscanf(nodeFocus, "node.%d.%d", &gi, &ni); err == nil && gi >= 0 && gi < len(m.snapshot.Groups) {
		g := m.snapshot.Groups[gi]
		if ni >= 0 && ni < len(g.All) {
			return gi, g, g.All[ni], true
		}
	}
	if strings.HasPrefix(m.focus, "group.") {
		gi, err := strconv.Atoi(strings.TrimPrefix(m.focus, "group."))
		if err == nil && gi >= 0 && gi < len(m.snapshot.Groups) {
			return gi, m.snapshot.Groups[gi], "", true
		}
	}
	gi, g, ok := m.proxyGroup(m.proxies.expanded)
	return gi, g, "", ok
}
func (m Model) proxyPath(name string) []string {
	var path []string
	seen := make(map[string]bool)
	for name != "" {
		path = append(path, name)
		if seen[name] {
			return append(path, m.tr("proxies.cycle"))
		}
		seen[name] = true
		_, g, group := m.proxyGroup(name)
		if !group || g.Type == "LoadBalance" || g.Type == "Relay" {
			break
		}
		name = g.Now
	}
	return path
}
func (m Model) proxyPathKey(name string) string { return strings.Join(m.proxyPath(name), "\x00") }
func (m Model) proxySummary(name string) string {
	path := m.proxyPath(name)
	if len(path) == 0 {
		return m.tr("proxies.no_selection")
	}
	return path[len(path)-1]
}
func (m Model) proxyNodeMeta(name string) string {
	if _, g, ok := m.proxyGroup(name); ok {
		summary := m.proxySummary(g.Now)
		if g.Type == "LoadBalance" || g.Type == "Relay" {
			summary = m.tr("proxies.distributed")
		}
		return m.tr("proxies.group_reference", summary)
	}
	return m.proxyMeta(name)
}
func (m Model) proxyMeta(name string) string {
	if _, g, ok := m.proxyGroup(name); ok {
		if g.Type == "LoadBalance" || g.Type == "Relay" {
			return g.Type + " · " + m.tr("proxies.distributed")
		}
		return g.Type + " · " + strings.Join(m.proxyPath(g.Now), " → ")
	}
	p, ok := m.snapshot.Proxies[name]
	if !ok {
		switch name {
		case "DIRECT":
			return "Direct"
		case "REJECT", "REJECT-DROP":
			return "Reject"
		}
		return m.tr("proxies.unknown_type")
	}
	meta := p.Type
	if p.UDP {
		meta += " · UDP"
	}
	return meta
}
func (m Model) proxyTestable(name string) bool {
	seen := make(map[string]bool)
	for _, n := range m.proxyPath(name) {
		p, exists := m.snapshot.Proxies[n]
		if seen[n] || n == "DIRECT" || n == "REJECT" || n == "REJECT-DROP" || p.Type == "Direct" || p.Type == "Reject" || p.Type == "RejectDrop" {
			return false
		}
		seen[n] = true
		if _, g, ok := m.proxyGroup(n); ok {
			if g.Type == "LoadBalance" || g.Type == "Relay" || g.Now == "" || indexOf(g.All, g.Now) < 0 {
				return false
			}
		} else if !exists {
			return false
		}
	}
	return true
}
func proxyResultKey(group, node string) string { return group + "\x00" + node }
func (m Model) proxyDelay(group, node string) proxyMeasurement {
	if r, ok := m.proxies.results[proxyResultKey(group, node)]; ok && r.path == m.proxyPathKey(node) {
		return r
	}
	p := m.snapshot.Proxies[node]
	// Core history does not identify the selected route at measurement time.
	// A group's old observation cannot describe its newly selected outbound.
	if p.IsGroup() {
		return proxyMeasurement{}
	}
	if len(p.History) > 0 {
		last := p.History[len(p.History)-1]
		state := "success"
		if last.Delay == 0 {
			state = "failed"
		}
		return proxyMeasurement{result: mihomo.ProxyDelay{State: state, Delay: last.Delay, Time: last.Time}}
	}
	return proxyMeasurement{}
}
func (m Model) visibleProxyGroups() []int {
	var out []int
	query := strings.ToLower(strings.TrimSpace(m.proxies.search.Value()))
	for i, g := range m.snapshot.Groups {
		if m.proxiesNarrow() && m.proxies.expanded != "" {
			if g.Name != m.proxies.expanded {
				continue
			}
		} else if !(len(m.proxies.history) > 0 && g.Name == m.proxies.expanded) && (m.snapshot.Mode.Actual == "global" && g.Name != "GLOBAL" || m.snapshot.Mode.Actual != "global" && g.Name == "GLOBAL") {
			continue
		}
		matches := query == "" || strings.Contains(strings.ToLower(g.Name), query)
		for _, node := range g.All {
			matches = matches || strings.Contains(strings.ToLower(node), query)
		}
		if matches {
			out = append(out, i)
		}
	}
	return out
}
func (m Model) proxyMembers(g mihomo.Group) []int {
	indices := make([]int, 0, len(g.All))
	query := strings.ToLower(strings.TrimSpace(m.proxies.search.Value()))
	for i, n := range g.All {
		if query == "" || strings.Contains(strings.ToLower(g.Name), query) || strings.Contains(strings.ToLower(n), query) {
			indices = append(indices, i)
		}
	}
	switch m.proxies.sort {
	case "name":
		sort.SliceStable(indices, func(i, j int) bool { return g.All[indices[i]] < g.All[indices[j]] })
	case "delay":
		// Freeze the order on an explicit sort action. Incoming samples never move
		// a row under the cursor; newly discovered members append in core order.
		ranks := make(map[string]int)
		for i, name := range m.proxies.order[g.Name] {
			ranks[name] = i + 1
		}
		sort.SliceStable(indices, func(i, j int) bool {
			a, b := ranks[g.All[indices[i]]], ranks[g.All[indices[j]]]
			return a > 0 && (b == 0 || a < b)
		})
	}
	return indices
}
func (m *Model) sortProxies(order string) {
	m.proxies.sort = order
	m.proxies.order = make(map[string][]string)
	if order == "delay" {
		for _, g := range m.snapshot.Groups {
			names := append([]string(nil), g.All...)
			value := func(n string) int {
				r := m.proxyDelay(g.Name, n).result
				if r.State == "success" {
					return r.Delay
				}
				return int(^uint(0) >> 1)
			}
			sort.SliceStable(names, func(i, j int) bool { return value(names[i]) < value(names[j]) })
			m.proxies.order[g.Name] = names
		}
	}
	m.revealContentFocus()
}
func (m *Model) openProxySort() {
	if !m.proxiesReady() || m.busy {
		return
	}
	m.proxies.sortReturn = m.proxyLocation()
	items := make([]menuItem, 0, 3)
	for _, order := range []string{"default", "delay", "name"} {
		label := m.tr("proxies.sort." + order)
		if order == m.proxies.sort {
			label = "✓ " + label
		}
		items = append(items, menuItem{ID: order, Label: label})
	}
	m.proxies.sortMenu = true
	m.proxies.search.Blur()
	m.proxies.editing = false
	m.focus = "proxies.sort"
	m.layout()
	x, y := dashboardGeometry(m.dashboardOptions()).x, dashboardGeometry(m.dashboardOptions()).y
	gi, _, _, expanded := m.proxyAtFocus()
	for _, hit := range m.targets {
		if expanded && hit.ID == fmt.Sprintf("group.%d", gi) {
			x, y = hit.X, hit.Y+1
		}
		if hit.ID == "proxies.sort" {
			x, y = hit.X, hit.Y+1
			break
		}
	}
	m.menu.show(items, x, y)
}
func (m *Model) proxyMenuResult(result menuResult) tea.Cmd {
	if !result.Closed {
		return nil
	}
	m.menu.dismiss()
	m.proxies.sortMenu = false
	if result.Action != "" && m.page == "proxies" {
		m.sortProxies(result.Action)
	}
	location := m.proxies.sortReturn
	if location.focus == "proxies.sort" && m.proxies.sort == "default" {
		location.identity = pageViewState{group: location.group}
	}
	m.restoreProxyLocation(location)
	m.revealContentFocus()
	return nil
}
func (m *Model) focusProxySearch() tea.Cmd {
	m.proxies.detail = ""
	m.proxies.expanded = ""
	m.proxies.history = nil
	m.proxies.editing = true
	m.focus, m.scroll = "proxies.search", 0
	return m.proxies.search.Focus()
}
func (m *Model) proxySearchKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "enter", "esc", "tab", "shift+tab":
		m.proxies.editing = false
		m.proxies.search.Blur()
		m.focus = "content"
		for _, hit := range m.contentTargets() {
			if listTarget(hit.ID) {
				m.focus = hit.ID
				break
			}
		}
		return nil
	}
	var cmd tea.Cmd
	m.proxies.search, cmd = m.proxies.search.Update(k)
	m.scroll = 0
	return cmd
}
func (m *Model) browseProxyReference(name string) {
	gi, _, ok := m.proxyGroup(name)
	if !ok {
		return
	}
	for _, loc := range m.proxies.history {
		if loc.group == name {
			return
		}
	}
	m.proxies.history = append(m.proxies.history, m.proxyLocation())
	m.proxies.expanded, m.proxies.detail = name, ""
	m.proxies.search.SetValue("")
	m.focus, m.scroll = fmt.Sprintf("group.%d", gi), 0
	if !m.proxiesNarrow() {
		_, hits := m.proxiesBody(dashboardBodyWidth(m.dashboardOptions()))
		for _, hit := range hits {
			if hit.ID == m.focus {
				m.scroll = hit.Y
				break
			}
		}
	}
	m.rememberPageView()
}
func (m *Model) proxyBack() bool {
	if m.proxies.detail != "" {
		loc := m.proxies.detailReturn
		m.proxies.detail = ""
		m.restoreProxyLocation(loc)
		return true
	}
	if n := len(m.proxies.history); n > 0 {
		loc := m.proxies.history[n-1]
		m.proxies.history = m.proxies.history[:n-1]
		m.restoreProxyLocation(loc)
		return true
	}
	if m.proxies.expanded != "" && m.proxiesNarrow() {
		gi, _, _ := m.proxyGroup(m.proxies.expanded)
		m.proxies.expanded = ""
		m.focus = fmt.Sprintf("group.%d", gi)
		m.revealContentFocus()
		return true
	}
	return false
}
func (m *Model) openProxyDetail(group, node string) {
	m.proxies.detailReturn = m.proxyLocation()
	m.proxies.detail, m.proxies.detailGroup = node, group
	m.focus, m.scroll = "proxies.back", 0
}
func (m *Model) proxyKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.page != "proxies" || strings.HasPrefix(m.focus, "nav.") || m.sidebarOpen {
		return false, nil
	}
	switch k.String() {
	case "/":
		return true, m.focusProxySearch()
	case "r", "g", "ctrl+r":
		if !m.busy {
			return true, m.refresh()
		}
		return true, nil
	case "s":
		m.openProxySort()
		return true, nil
	case "t":
		return true, m.testProxyGroup()
	case "i":
		_, g, n, ok := m.proxyAtFocus()
		if ok && n != "" {
			m.openProxyDetail(g.Name, n)
		}
		return true, nil
	case "right", "l":
		gi, g, n, ok := m.proxyAtFocus()
		if ok && n != "" {
			if m.proxyHorizontal(1) {
				return true, nil
			}
			if _, _, ref := m.proxyGroup(n); ref {
				m.browseProxyReference(n)
			}
		} else if ok {
			m.proxies.expanded = g.Name
			if list := m.proxyMembers(g); len(list) > 0 {
				m.focus = fmt.Sprintf("node.%d.%d", gi, list[0])
			}
			m.revealContentFocus()
		}
		return true, nil
	case "left", "h", "esc":
		if m.proxyBack() {
			return true, nil
		}
		return false, nil
	}
	return m.proxyListKey(k), nil
}
func (m *Model) proxyHorizontal(delta int) bool {
	if m.page != "proxies" || m.proxies.detail != "" {
		return false
	}
	_, hits := m.proxiesBody(dashboardBodyWidth(m.dashboardOptions()))
	var current hitRegion
	for _, h := range hits {
		if h.ID == m.focus && strings.HasPrefix(h.ID, "node.") {
			current = h
			break
		}
	}
	if current.ID == "" {
		return false
	}
	var nearest hitRegion
	seen := make(map[string]bool)
	for _, h := range hits {
		if !strings.HasPrefix(h.ID, "node.") || seen[h.ID] {
			continue
		}
		seen[h.ID] = true
		if h.ID != current.ID && h.Y == current.Y && (delta > 0 && h.X > current.X || delta < 0 && h.X < current.X) {
			if nearest.ID == "" || delta > 0 && h.X < nearest.X || delta < 0 && h.X > nearest.X {
				nearest = h
			}
		}
	}
	if nearest.ID == "" {
		return false
	}
	m.focus = nearest.ID
	m.revealBodyTarget(nearest)
	return true
}
func (m *Model) proxyListKey(k tea.KeyPressMsg) bool {
	key := k.String()
	if m.proxies.detail != "" || key != "up" && key != "down" && key != "j" && key != "k" && key != "home" && key != "end" {
		return false
	}
	_, hits := m.proxiesBody(dashboardBodyWidth(m.dashboardOptions()))
	var list []hitRegion
	index := -1
	seen := make(map[string]bool)
	for _, hit := range hits {
		if listTarget(hit.ID) && !seen[hit.ID] {
			seen[hit.ID] = true
			if hit.ID == m.focus {
				index = len(list)
			}
			list = append(list, hit)
		}
	}
	if len(list) == 0 {
		return true
	}
	// Move to the nearest column in the next visual row, including partial rows.
	distance := func(a, b int) int { return max(a-b, b-a) }
	next := max(0, index)
	if key == "home" {
		next = 0
	} else if key == "end" {
		next = len(list) - 1
	} else {
		delta := 1
		if key == "up" || key == "k" {
			delta = -1
		}
		next = min(len(list)-1, max(0, index+delta))
		if index >= 0 {
			next = index
			row := -1
			for i := index + delta; i >= 0 && i < len(list); i += delta {
				if list[i].Y == list[index].Y {
					continue
				}
				if row >= 0 && list[i].Y != row {
					break
				}
				if row < 0 || distance(list[i].X, list[index].X) < distance(list[next].X, list[index].X) {
					next = i
				}
				row = list[i].Y
			}
		}
	}
	m.focus = list[next].ID
	m.revealBodyTarget(list[next])
	return true
}
func (m *Model) proxyActivate(id string) tea.Cmd {
	if id != "proxies.search" {
		m.proxies.editing = false
		m.proxies.search.Blur()
	}
	switch id {
	case "proxies.search":
		return m.focusProxySearch()
	case "proxies.clear":
		m.proxies.search.SetValue("")
		m.focus = "proxies.search"
	case "proxies.sort":
		m.openProxySort()
	case "proxies.test":
		return m.testProxyGroup()
	case "proxies.back":
		m.proxyBack()
	case "proxies.enter":
		m.browseProxyReference(m.proxies.detail)
	case "proxies.directory":
		m.proxies.expanded = ""
		m.proxies.history = nil
		m.proxies.detail = ""
		m.focus = "content"
		m.scroll = 0
	default:
		previous := m.focus
		m.focus = id
		gi, g, node, ok := m.proxyAtFocus()
		if !ok {
			m.focus = previous
			return nil
		}
		if strings.HasPrefix(id, "proxies.reference.") && node != "" {
			m.browseProxyReference(node)
		} else if strings.HasPrefix(id, "group.") {
			if m.proxies.expanded == g.Name {
				m.proxies.expanded = ""
			} else {
				m.proxies.expanded = g.Name
			}
			m.proxies.detail = ""
			m.revealContentFocus()
		} else if strings.HasPrefix(id, "node.") && node != "" && !m.busy && m.proxiesReady() {
			if g.Type != "Selector" {
				m.openProxyDetail(g.Name, node)
				return nil
			}
			if g.Now == node {
				return nil
			}
			m.focus = fmt.Sprintf("node.%d.%d", gi, indexOf(g.All, node))
			cmd := m.begin(m.tr("model.switching_node"), m.tr("model.node_selected"), func(ctx context.Context) error { return m.backend.Select(ctx, g.Name, node) }, notificationOperation{Key: notificationKey("node.select", g.Name+"\x00"+node), Context: m.tr("model.switching_node") + " · " + clean(g.Name) + " / " + clean(node)})
			m.proxies.pendingGroup, m.proxies.pendingNode = g.Name, node
			return cmd
		}
	}
	return nil
}
func indexOf(values []string, value string) int {
	for i, v := range values {
		if v == value {
			return i
		}
	}
	return -1
}

func (m *Model) stopProxyTests() {
	m.proxies.testStatusVisible = false
	if m.proxies.cancel != nil {
		m.proxies.cancel()
		m.proxies.cancel = nil
	}
	if m.proxies.testing {
		m.proxies.generation++
		m.proxies.testStatus = "canceled"
	}
	m.proxies.testing = false
	m.proxies.queue = nil
	m.proxies.inflight = nil
}
func (m *Model) syncProxies() {
	if m.snapshot.Mode.Actual != m.proxies.mode {
		m.proxies.mode = m.snapshot.Mode.Actual
		m.proxies.history = nil
		m.proxies.detail, m.proxies.expanded = "", ""
		m.proxies.editing = false
		m.proxies.search.Blur()
		m.proxies.search.SetValue("")
		if m.page == "proxies" && !strings.HasPrefix(m.focus, "nav.") {
			m.focus, m.scroll = "content", 0
		}
	}
	if m.page != "proxies" || m.busy || m.quitting || !m.proxiesReady() {
		m.stopProxyTests()
	}
	source := fmt.Sprintf("%s\x00%s\x00%d\x00%s", m.snapshot.Generation, m.snapshot.Applied, m.snapshot.Service.PID, m.snapshot.RunningVersion)
	if source != m.proxies.source {
		m.stopProxyTests()
		m.proxies.results = make(map[string]proxyMeasurement)
		m.proxies.source = source
		m.proxies.order = make(map[string][]string)
	}
	if m.proxies.expanded != "" {
		if _, _, ok := m.proxyGroup(m.proxies.expanded); !ok {
			m.proxies.expanded = ""
			m.proxies.history = nil
		}
	}
	for key, r := range m.proxies.results {
		parts := strings.SplitN(key, "\x00", 2)
		_, g, ok := m.proxyGroup(parts[0])
		if !ok || len(parts) != 2 || indexOf(g.All, parts[1]) < 0 || r.path != m.proxyPathKey(parts[1]) {
			delete(m.proxies.results, key)
		}
	}
}
func (m *Model) testProxyGroup() tea.Cmd {
	if m.proxies.testing {
		m.stopProxyTests()
		return m.showProxyTestStatus()
	}
	backend, ok := m.backend.(proxyDelayBackend)
	if !ok || !m.proxiesReady() || m.busy || m.quitting {
		return nil
	}
	_, g, _, ok := m.proxyAtFocus()
	if !ok {
		return nil
	}
	m.proxies.generation++
	m.proxies.testStatusVisible = false
	m.proxies.testCtx, m.proxies.cancel = context.WithCancel(m.ctx)
	m.proxies.queue = nil
	m.proxies.inflight = make(map[string]bool)
	for _, node := range g.All {
		if m.proxyTestable(node) {
			m.proxies.queue = append(m.proxies.queue, proxyTestTarget{g.Name, node, m.proxyPathKey(node)})
		}
	}
	m.proxies.total, m.proxies.completed = len(m.proxies.queue), 0
	m.proxies.testGroup, m.proxies.testStatus = g.Name, "done"
	if len(m.proxies.queue) == 0 {
		m.proxies.cancel()
		m.proxies.cancel = nil
		return nil
	}
	m.proxies.testing = true
	m.proxies.testStatus = "testing"
	var commands []tea.Cmd
	for range min(4, len(m.proxies.queue)) {
		commands = append(commands, m.nextProxyTest(backend))
	}
	return tea.Batch(append(commands, m.spinner.Tick)...)
}
func (m *Model) nextProxyTest(backend proxyDelayBackend) tea.Cmd {
	if len(m.proxies.queue) == 0 {
		return nil
	}
	target := m.proxies.queue[0]
	m.proxies.queue = m.proxies.queue[1:]
	m.proxies.inflight[proxyResultKey(target.group, target.node)] = true
	ctx, id := m.proxies.testCtx, m.proxies.generation
	return func() tea.Msg {
		result, err := backend.TestProxyDelay(ctx, target.group, target.node)
		return proxyDelayMsg{id, target, result, err}
	}
}
func (m *Model) acceptProxyDelay(msg proxyDelayMsg) tea.Cmd {
	if !m.proxies.testing || msg.generation != m.proxies.generation || m.page != "proxies" {
		return nil
	}
	key := proxyResultKey(msg.target.group, msg.target.node)
	if !m.proxies.inflight[key] {
		return nil
	}
	delete(m.proxies.inflight, key)
	if !errors.Is(msg.err, context.Canceled) && msg.target.path == m.proxyPathKey(msg.target.node) {
		if msg.err != nil {
			msg.result = mihomo.ProxyDelay{State: "failed", Time: time.Now(), URL: mihomo.DefaultProxyDelayURL}
		}
		m.proxies.results[key] = proxyMeasurement{msg.result, msg.target.path, msg.err}
	}
	m.proxies.completed++
	if len(m.proxies.queue) > 0 {
		return m.nextProxyTest(m.backend.(proxyDelayBackend))
	}
	if len(m.proxies.inflight) == 0 {
		m.proxies.testing = false
		m.proxies.testStatus = "done"
		if m.proxies.cancel != nil {
			m.proxies.cancel()
			m.proxies.cancel = nil
		}
		return m.showProxyTestStatus()
	}
	return nil
}

func (m *Model) showProxyTestStatus() tea.Cmd {
	if m.proxies.total == 0 {
		return nil
	}
	m.proxies.testStatusVisible = true
	generation := m.proxies.generation
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return proxyTestStatusExpiredMsg{generation: generation}
	})
}
