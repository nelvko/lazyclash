package tui

import (
	"fmt"
	"runtime"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

const sidebarWidth = 20

type hitRegion struct {
	ID         string
	X, Y, W, H int
}
type dashboardOptions struct {
	Locale                          i18n.Localizer
	MessageTransient                bool
	MessageInformational            bool
	Width, Height                   int
	Page                            string
	SelectedGroup                   string
	Snapshot                        app.Snapshot
	Focus                           string
	Hover                           string
	GuideDismissed                  bool
	GuideSkipped                    bool
	Scroll                          int
	Dim                             bool
	Message, Failure                string
	Notifications                   []notificationItem
	Spinner                         string
	Help                            string
	HideKeyHints                    bool
	SidebarOpen                     bool
	Sidebar                         *sidebarState
	LogoFrame                       int
	LogoIntroFrame                  int
	Busy                            bool
	PlainButtons                    bool
	StateUnavailable                bool
	Header                          pageHeader
	Runtime                         app.RuntimeSnapshot
	RuntimeReceived, RuntimePending bool
	// Body and its targets are relative to the first content row beneath the
	// page header. They are used by the Proxies, Profiles, Connections, Rules and Logs pages.
	Body          string
	BodyTargets   []hitRegion
	InlineInstall bool
}

type dashboardLayout struct {
	x, width, y, height   int
	headerX, headerWidth  int
	headerY, headerHeight int
	sidebar               bool
}

func dashboardGeometry(o dashboardOptions) dashboardLayout {
	sidebar := o.Width >= 100 && o.Height >= 20
	available := max(1, o.Width-4)
	left := 2
	if sidebar {
		left = sidebarWidth + 2
		available = max(1, o.Width-left-3)
	}
	headerX, headerWidth := left, available
	width := min(112, available)
	if o.Page == "" || o.Page == "overview" {
		width = available
		left = (o.Width - width) / 2
		if sidebar {
			left = sidebarWidth + (o.Width-sidebarWidth-width)/2
		} else if o.InlineInstall && o.Width < 38 {
			// The compact setup notice needs the full row at very small sizes.
			width, left = o.Width, 0
		}
	}
	headerY, bodyY := 0, 3
	if o.Height >= 24 {
		headerY, bodyY = 1, 5
	}
	if o.Width < 70 && o.Height < 24 && o.Page != "" && o.Page != "overview" {
		bodyY++
	}
	return dashboardLayout{x: left, width: width, y: bodyY, height: max(0, dashboardContentBottom(o)-bodyY), headerX: headerX, headerWidth: headerWidth, headerY: headerY, headerHeight: bodyY - 2, sidebar: sidebar}
}
func dashboardBodyWidth(o dashboardOptions) int { return dashboardGeometry(o).width }

func dashboardContentBottom(o dashboardOptions) int {
	if o.HideKeyHints {
		return o.Height
	}
	return o.Height - 1
}

func renderDashboard(o dashboardOptions) (string, []hitRegion, int) {
	tr := o.Locale.T
	if o.Width <= 0 || o.Height <= 0 {
		return "", nil, 0
	}
	if o.Page == "" {
		o.Page = "overview"
	}
	g := dashboardGeometry(o)
	rows := make([]string, o.Height)
	for i := range rows {
		rows[i] = themed(strings.Repeat(" ", o.Width), text, bg, o.Dim)
	}
	put := func(x, y int, s string) { putRows(rows, x, y, s, o.Width) }
	var targets []hitRegion
	add := func(r hitRegion) {
		if r, ok := clipRegion(r, 0, 0, o.Width, o.Height); ok {
			targets = append(targets, r)
		}
	}
	titleText := lipgloss.NewStyle().Bold(true).Render(pageTitle(o.Page, o.Locale))
	if o.Height < 7 || o.Width < 20 {
		put(0, 0, themed(cellCrop(logoText(bg, o.Dim)+" / "+titleText, o.Width), text, bg, o.Dim))
		if o.Height > 1 {
			put(0, o.Height-1, themed(cellCrop(tr("dashboard.menuCompact"), o.Width), muted, bg, o.Dim))
			add(hitRegion{ID: "menu", X: 0, Y: o.Height - 1, W: min(ansi.StringWidth(tr("dashboard.menu")), o.Width), H: 1})
		}
		return strings.Join(rows, "\n"), targets, 0
	}
	titleX := g.headerX
	if !g.sidebar {
		label := dashboardButton("menu", tr("dashboard.menu"), o, false)
		put(1, g.headerY, label)
		add(hitRegion{ID: "menu", X: 1, Y: g.headerY, W: ansi.StringWidth(label), H: 1})
		titleX = max(12, g.headerX)
		if o.Width >= 50 {
			titleText = logoText(bg, o.Dim) + " / " + titleText
		}
	}
	put(titleX, g.headerY, themed(cellCrop(titleText, max(1, g.headerX+g.headerWidth-titleX)), text, bg, o.Dim))
	header, headerX, headerY, headerTargets := renderPageHeader(o, g, titleX, ansi.StringWidth(titleText))
	put(headerX, headerY, header)
	for _, hit := range headerTargets {
		add(hit)
	}
	// A top-edge stroke aligns with the sidebar's background boundary.
	put(g.headerX, g.headerHeight, themed(strings.Repeat("▔", g.headerWidth), divider, bg, o.Dim))

	var body string
	var bodyTargets []hitRegion
	switch o.Page {
	case "overview":
		if o.InlineInstall {
			body, bodyTargets = o.Body, o.BodyTargets
		} else {
			body, bodyTargets = overviewContent(o, g.width)
		}
	case "settings":
		body, bodyTargets = settingsContent(o, g.width)
	default:
		body, bodyTargets = o.Body, o.BodyTargets
	}
	bodyLines := strings.Split(body, "\n")
	if body == "" {
		bodyLines = nil
	}
	total := len(bodyLines)
	scroll := max(0, min(o.Scroll, max(0, total-g.height)))
	o.Scroll = scroll
	for i := 0; i < g.height && scroll+i < total; i++ {
		put(g.x, g.y+i, cellPad(bodyLines[scroll+i], g.width))
	}
	for _, r := range bodyTargets {
		r.X += g.x
		r.Y += g.y - scroll
		if r, ok := clipRegion(r, g.x, g.y, g.width, g.height); ok {
			add(r)
		}
	}
	if total > g.height && g.height > 0 {
		// The marker occupies the outer margin and never consumes card contents.
		marker := "↓"
		if scroll > 0 {
			marker = "↕"
		}
		if scroll+g.height >= total {
			marker = "↑"
		}
		put(min(o.Width-1, g.x+g.width), g.y+g.height-1, themed(marker, accent, bg, o.Dim))
	}

	if g.sidebar {
		targets = append(targets, drawSidebar(rows, o, false)...)
	}
	if !g.sidebar && o.SidebarOpen {
		menuWidth := min(sidebarWidth, o.Width)
		// Underlying buttons must not receive a click through the navigation menu.
		kept := targets[:0]
		for _, r := range targets {
			if !regionsOverlap(r, hitRegion{X: 0, Y: g.headerHeight + 1, W: menuWidth, H: max(0, dashboardContentBottom(o)-g.headerHeight-1)}) {
				kept = append(kept, r)
			}
		}
		targets = append(kept, drawSidebar(rows, o, true)...)
	}
	if !o.HideKeyHints {
		help := o.Help
		if help == "" {
			help = tr("dashboard.footer.default")
		}
		quit := tr("dashboard.footer.quit")
		put(2, o.Height-1, helpText(cellCrop(help, max(1, o.Width-ansi.StringWidth(quit)-6)), bg, o.Dim))
		if o.Width >= 16 {
			put(o.Width-ansi.StringWidth(quit)-2, o.Height-1, helpText(quit, bg, o.Dim))
		}
	}
	for _, notification := range notificationLayout(o) {
		bounds := notification.Bounds
		put(bounds.X, bounds.Y, notification.Content)
		// A Toast owns every cell it covers, including its padding and border.
		// Remove underlying controls so a click cannot pass through the card.
		kept := targets[:0]
		for _, target := range targets {
			if !regionsOverlap(target, bounds) {
				kept = append(kept, target)
			}
		}
		targets = kept
		for _, target := range notification.Targets {
			add(target)
		}
	}

	return strings.Join(rows, "\n"), targets, total
}

// navPages defines both visible order and the 1–7 page shortcuts.
func navPages() []string {
	return []string{"overview", "proxies", "profiles", "connections", "rules", "logs", "settings"}
}

func pageTitle(page string, locales ...i18n.Localizer) string {
	tr := dashboardLocale(locales).T
	switch page {
	case "proxies":
		return tr("dashboard.page.proxies")
	case "profiles":
		return tr("dashboard.page.profiles")
	case "settings":
		return tr("dashboard.page.settings")
	case "rules":
		return tr("dashboard.page.rules")
	case "connections":
		return tr("dashboard.page.connections")
	case "logs":
		return tr("dashboard.page.logs")
	default:
		return tr("dashboard.page.overview")
	}
}

func coreSummary(s app.Snapshot, locales ...i18n.Localizer) (string, string) {
	tr := dashboardLocale(locales).T
	if s.Service.State == "unknown" && s.ServiceError != "" {
		return tr("dashboard.status.unknown"), errorColor
	}
	if !s.Installed {
		return tr("dashboard.status.notInstalled"), muted
	}
	if s.ServiceError != "" {
		return tr("dashboard.status.unavailable"), errorColor
	}
	if s.Service.Active {
		return tr("dashboard.status.running"), success
	}
	switch s.Service.State {
	case "activating":
		return tr("dashboard.status.starting"), warning
	case "deactivating":
		return tr("dashboard.status.stopping"), warning
	case "reloading":
		return tr("dashboard.status.reloading"), warning
	case "failed":
		return tr("dashboard.status.failed"), errorColor
	case "unknown", "unsupported":
		return tr("dashboard.status.unavailable"), errorColor
	}
	return tr("dashboard.status.stopped"), muted
}

func clipRegion(r hitRegion, x, y, w, h int) (hitRegion, bool) {
	right, bottom := min(r.X+r.W, x+w), min(r.Y+r.H, y+h)
	r.X, r.Y = max(r.X, x), max(r.Y, y)
	r.W, r.H = right-r.X, bottom-r.Y
	return r, r.W > 0 && r.H > 0
}
func regionsOverlap(a, b hitRegion) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func sidebarTraffic(o dashboardOptions, width int) []string {
	inner := max(1, width-4)
	style := func(value, color string) string {
		return themed(value, color, sidebarBackground, o.Dim)
	}
	up, down := "--", "--"
	if !o.StateUnavailable && o.Snapshot.Installed && o.RuntimeReceived && o.Runtime.State == "ready" && o.Runtime.Traffic != nil {
		up = formatBytes(o.Runtime.Traffic.Up) + "/s"
		down = formatBytes(o.Runtime.Traffic.Down) + "/s"
	}
	rate := func(arrow, value string) string {
		value = cellCrop(value, max(1, inner-2))
		gap := max(1, inner-1-ansi.StringWidth(value))
		return style(arrow, muted) + style(strings.Repeat(" ", gap)+value, text)
	}
	return []string{style(strings.Repeat("─", inner), divider), rate("↑", up), rate("↓", down)}
}

func dashboardButton(id, label string, o dashboardOptions, primary bool) string {
	return renderButton(label, dashboardButtonOptions(id, o, primary, bg))
}

type dashboardBlock struct {
	lines   []string
	targets []hitRegion
	footerY int
}

func (b *dashboardBlock) append(block dashboardBlock) {
	offset := len(b.lines)
	b.lines = append(b.lines, block.lines...)
	for _, r := range block.targets {
		r.Y += offset
		b.targets = append(b.targets, r)
	}
}
func (b *dashboardBlock) blank() { b.lines = append(b.lines, "") }

func overviewRow(o dashboardOptions, width, columns int, render func(int, int) dashboardBlock) dashboardBlock {
	cardWidth := (width - 2*(columns-1)) / columns
	blocks := make([]dashboardBlock, columns)
	widths := make([]int, columns)
	height := 0
	for column := range blocks {
		widths[column] = cardWidth
		if column == columns-1 {
			widths[column] = width - column*(cardWidth+2)
		}
		blocks[column] = render(column, widths[column])
		height = max(height, len(blocks[column].lines))
	}
	var row dashboardBlock
	for column := range blocks {
		block := &blocks[column]
		padding := height - len(block.lines)
		if padding > 0 {
			insert := len(block.lines) - 1
			if block.footerY > 0 {
				insert = block.footerY
			}
			lines := append([]string(nil), block.lines[:insert]...)
			blank := panelFrame(nil, widths[column], 3, o)[1]
			for range padding {
				lines = append(lines, blank)
			}
			block.lines = append(lines, block.lines[insert:]...)
			for i := range block.targets {
				if block.targets[i].Y >= insert {
					block.targets[i].Y += padding
				}
			}
		}
		for _, hit := range block.targets {
			hit.X += column * (cardWidth + 2)
			row.targets = append(row.targets, hit)
		}
	}
	for y := 0; y < height; y++ {
		line := ""
		for column, block := range blocks {
			if column > 0 {
				line += themed("  ", text, bg, o.Dim)
			}
			line += block.lines[y]
		}
		row.lines = append(row.lines, line)
	}
	return row
}

func panelFrame(lines []string, width, height int, o dashboardOptions, edge ...string) []string {
	edgeColor := divider
	if len(edge) > 0 {
		edgeColor = edge[0]
	}
	if width < 4 {
		result := make([]string, max(1, height))
		for i := range result {
			if i < len(lines) {
				result[i] = cellCrop(lines[i], width)
			}
		}
		return result
	}
	height = max(height, len(lines)+2)
	result := make([]string, height)
	result[0] = themed("┌"+strings.Repeat("─", width-2)+"┐", edgeColor, panel, o.Dim)
	for i := 1; i < height-1; i++ {
		content := ""
		if i-1 < len(lines) {
			content = lines[i-1]
		}
		result[i] = themed("│ ", edgeColor, panel, o.Dim) + themed(cellPad(content, width-4), text, panel, o.Dim) + themed(" │", edgeColor, panel, o.Dim)
	}
	result[height-1] = themed("└"+strings.Repeat("─", width-2)+"┘", edgeColor, panel, o.Dim)
	return result
}

func overviewContent(o dashboardOptions, width int) (string, []hitRegion) {
	var body dashboardBlock
	if overviewSetupVisible(o) {
		setup := getStarted(o, width)
		return strings.Join(setup.lines, "\n"), setup.targets
	}
	controlColumns := 1
	if width >= 76 && o.Height >= 20 {
		controlColumns = 2
	}
	if width >= 166 && o.Height >= 24 {
		controlColumns = 4
	}
	for first := 0; first < 4; first += controlColumns {
		body.append(overviewRow(o, width, min(controlColumns, 4-first), func(column, cardWidth int) dashboardBlock {
			index := first + column
			if index == 1 {
				return overviewModeCard(o, cardWidth)
			}
			if index > 1 {
				index--
			}
			return overviewControlCard(o, index, cardWidth)
		}))
		body.blank()
	}
	cards := overviewCards(o)
	columns := 1
	if width >= 76 && o.Height >= 24 {
		columns = 2
	}
	cardWidth := width
	if columns == 2 {
		cardWidth = (width - 2) / 2
	}
	gridCards := 2
	content := make([][]string, len(cards))
	for i, c := range cards {
		for _, line := range c {
			content[i] = append(content[i], strings.Split(wrapText(line, max(1, cardWidth-4)), "\n")...)
		}
	}
	for i := 0; i < gridCards; i += columns {
		var actions []hitRegion
		for column := 0; column < columns; column++ {
			index := i + column
			actionID, label := overviewAction(o, index)
			if actionID != "" {
				options := dashboardButtonOptions(actionID, o, false, panel)
				options.Disabled = o.Busy
				value := cellCrop(renderButton(label, options), max(1, cardWidth-4))
				content[index] = append(content[index], "", value)
				if !o.Busy {
					actions = append(actions, hitRegion{ID: actionID, X: 2 + column*(cardWidth+2), W: max(1, ansi.StringWidth(value)), H: 1})
				}
			}
		}
		height := len(content[i]) + 2
		if columns == 2 {
			height = max(height, len(content[i+1])+2)
		}
		for column := 0; column < columns; column++ {
			index := i + column
			if actionID, _ := overviewAction(o, index); actionID != "" {
				last := len(content[index]) - 1
				button := content[index][last]
				content[index] = content[index][:last]
				for len(content[index]) < height-3 {
					content[index] = append(content[index], "")
				}
				content[index] = append(content[index], button)
			}
		}
		for _, action := range actions {
			action.Y = len(body.lines) + height - 2
			body.targets = append(body.targets, action)
		}
		left := panelFrame(content[i], cardWidth, height, o)
		if columns == 1 {
			body.lines = append(body.lines, left...)
		} else {
			right := panelFrame(content[i+1], width-cardWidth-2, height, o)
			for y := range left {
				body.lines = append(body.lines, left[y]+themed("  ", text, bg, o.Dim)+right[y])
			}
		}
		if i+columns < gridCards {
			body.blank()
		}
	}
	body.blank()
	if columns == 2 {
		body.append(overviewTrafficSummary(o, cards[2], width))
	} else {
		var lines []string
		for _, line := range cards[2] {
			lines = append(lines, strings.Split(wrapText(line, max(1, width-4)), "\n")...)
		}
		body.append(dashboardBlock{lines: panelFrame(lines, width, len(lines)+2, o)})
	}
	body.blank()
	body.append(overviewRow(o, width, columns, func(column, cardWidth int) dashboardBlock {
		if column == 0 {
			return overviewKernelCard(o, cardWidth)
		}
		return overviewSystemCard(o, cardWidth)
	}))
	if columns == 1 {
		body.blank()
		body.append(overviewSystemCard(o, width))
	}
	return strings.Join(body.lines, "\n"), body.targets
}

func overviewAction(o dashboardOptions, index int) (string, string) {
	tr := o.Locale.T
	s := o.Snapshot
	switch index {
	case 0:
		return "card.import", tr("dashboard.action.import")
	case 1:
		if s.Service.Active && len(s.Groups) > 0 {
			return "card.proxies", tr("dashboard.action.openProxies")
		}
	}
	return "", ""
}

type overviewField struct{ label, value string }

func overviewTrafficFields(o dashboardOptions) []overviewField {
	tr := o.Locale.T
	connections, rate, totals, memory := "--", "--", "--", "--"
	observed := o.Runtime
	if o.RuntimeReceived && observed.State == "ready" {
		if observed.Connections != nil {
			connections = fmt.Sprint(len(observed.Connections.Connections))
			totals = "↑ " + formatBytes(observed.Connections.UploadTotal) + "  ↓ " + formatBytes(observed.Connections.DownloadTotal)
		}
		if observed.Traffic != nil {
			rate = "↑ " + formatBytes(observed.Traffic.Up) + "/s  ↓ " + formatBytes(observed.Traffic.Down) + "/s"
		}
		if observed.Memory != nil {
			memory = formatBytes(int64(*observed.Memory))
		}
	}
	return []overviewField{{tr("dashboard.field.uploadDownload"), rate}, {tr("dashboard.field.sinceStart"), totals}, {tr("dashboard.field.connections"), connections}, {tr("dashboard.field.coreMemory"), memory}}
}

func overviewEnvironmentFields(s app.Snapshot, locales ...i18n.Localizer) []overviewField {
	tr := dashboardLocale(locales).T
	fields := []overviewField{{tr("dashboard.field.platform"), runtime.GOOS + "/" + runtime.GOARCH}, {tr("dashboard.field.systemProxy"), tr("dashboard.field.unchanged")}, {tr("dashboard.field.tun"), tr("dashboard.field.tunUnsupported")}}
	if s.Installed {
		fields = append(fields, overviewField{tr("dashboard.field.installedVersion"), s.Version})
		if s.Service.Active {
			running := s.RunningVersion
			if running == "" {
				running = tr("dashboard.status.unavailable")
			}
			fields = append(fields, overviewField{tr("dashboard.field.runningVersion"), running})
		}
		if s.Desired.Known {
			desired := tr("dashboard.status.stopped")
			if s.Desired.Running {
				desired = tr("dashboard.status.running")
			}
			fields = append(fields, overviewField{tr("dashboard.field.desiredState"), desired})
		}
	}
	return fields
}

// Live metrics use two or four columns as the Overview content area grows.
func overviewTrafficSummary(o dashboardOptions, card []string, width int) dashboardBlock {
	inner := width - 4
	b := dashboardBlock{}
	heading := card[0] + themed(" · ", muted, panel, o.Dim) + card[1]
	lines := []string{heading}
	var fields []overviewField
	var notes []string
	if o.RuntimeReceived && o.Runtime.State == "ready" {
		fields = overviewTrafficFields(o)
		notes = card[3+len(fields):]
	} else {
		notes = card[2:]
	}
	fieldLines := func(field overviewField, width int) []string {
		label := themed(field.label, muted, panel, o.Dim)
		value := themed(clean(field.value), text, panel, o.Dim)
		return append(strings.Split(wrapText(label, width), "\n"), strings.Split(wrapText(value, width), "\n")...)
	}
	columns := 2
	if inner >= 142 {
		columns = 4
	}
	columnWidth := (inner - 2*(columns-1)) / columns
	for i := 0; i < len(fields); i += columns {
		cells := make([][]string, columns)
		height := 0
		for column := range cells {
			cells[column] = fieldLines(fields[i+column], columnWidth)
			height = max(height, len(cells[column]))
		}
		for y := 0; y < height; y++ {
			line := ""
			for column, cell := range cells {
				if column > 0 {
					line += themed("  ", text, panel, o.Dim)
				}
				value := ""
				if y < len(cell) {
					value = cell[y]
				}
				line += cellPad(value, columnWidth)
			}
			lines = append(lines, line)
		}
	}
	for _, note := range notes {
		lines = append(lines, strings.Split(wrapText(note, inner), "\n")...)
	}
	b.lines = panelFrame(lines, width, len(lines)+2, o)
	return b
}

func overviewSetupVisible(o dashboardOptions) bool {
	return !o.StateUnavailable && !o.Snapshot.Installed && !o.GuideSkipped && !o.GuideDismissed && !o.Snapshot.GuideDismissed
}

func getStarted(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	inner := max(1, width-4)
	lines := strings.Split(wrapText(themed(tr("dashboard.guide.title"), muted, panel, o.Dim), inner), "\n")
	lines = append(lines, "")
	lines = append(lines, strings.Split(wrapText(themed(lipgloss.NewStyle().Bold(true).Render(tr("dashboard.guide.installTitle")), accent, panel, o.Dim), inner), "\n")...)
	for _, key := range []string{"dashboard.guide.installDescription", "dashboard.guide.readyHint"} {
		lines = append(lines, strings.Split(wrapText(themed(tr(key), text, panel, o.Dim), inner), "\n")...)
	}
	lines = append(lines, "")
	b := dashboardBlock{}
	buttons := []struct {
		id, label string
		primary   bool
	}{{"install", tr("dashboard.guide.install"), true}, {"guide.later", tr("dashboard.guide.later"), false}}
	x := 0
	buttonLine := ""
	for _, button := range buttons {
		label := renderButton(button.label, dashboardButtonOptions(button.id, o, button.primary, panel))
		label = cellCrop(label, inner)
		w := ansi.StringWidth(label)
		if x > 0 && x+2+w > inner {
			lines = append(lines, cellPad(buttonLine, inner))
			buttonLine = ""
			x = 0
		}
		if x > 0 {
			buttonLine += themed("  ", text, panel, o.Dim)
			x += 2
		}
		left, top := 2, 1
		if width < 4 {
			left, top = 0, 0
		}
		b.targets = append(b.targets, hitRegion{ID: button.id, X: left + x, Y: top + len(lines), W: w, H: 1})
		buttonLine += label
		x += w
	}
	lines = append(lines, cellPad(buttonLine, inner))
	lines = append(lines, "")
	for _, key := range []string{"dashboard.guide.hint", "dashboard.guide.boundary"} {
		lines = append(lines, strings.Split(wrapText(themed(tr(key), muted, panel, o.Dim), inner), "\n")...)
	}
	b.lines = panelFrame(lines, width, len(lines)+2, o, accent)
	visible := b.targets[:0]
	for _, target := range b.targets {
		if target, ok := clipRegion(target, 0, 0, width, len(b.lines)); ok {
			visible = append(visible, target)
		}
	}
	b.targets = visible
	return b
}

func overviewCards(o dashboardOptions) [][]string {
	tr := o.Locale.T
	s := o.Snapshot
	style := func(value, color string) string { return themed(clean(value), color, panel, o.Dim) }
	title := func(value string) string { return lipgloss.NewStyle().Bold(true).Render(style(value, text)) }
	label := func(name, value string) string { return style(cellPad(name, 20), muted) + style(value, text) }
	configured, active := tr("dashboard.sidebar.notConfigured"), tr("dashboard.profile.none")
	if s.ConfigReady {
		configured = tr("dashboard.profile.ready")
		active = snapshotProfileName(s, s.Applied, o.Locale)
		if active == "" {
			active = tr("profiles.builtin_name")
		}
	}
	saved := 0
	for _, name := range s.Profiles {
		if name != "default" {
			saved++
		}
	}
	profile := []string{title(tr("dashboard.profile.title")), style(configured, text)}
	if s.ConfigReady || saved > 0 {
		profile = append(profile, "", style(tr("dashboard.profile.active"), muted), style(active, text), label(tr("dashboard.profile.saved"), fmt.Sprint(saved)))
	} else {
		profile = append(profile, style(tr("dashboard.profile.importHint"), muted))
	}
	groupName, node, groupStatus := "--", "--", tr("dashboard.groups.inactive")
	if s.Service.Active {
		groupStatus = tr("dashboard.groups.empty")
		if len(s.Groups) > 0 {

			g := s.Groups[0]
			found := false
			if o.SelectedGroup != "" {
				for _, candidate := range s.Groups {
					if candidate.Name == o.SelectedGroup {
						g = candidate
						found = true
						break
					}
				}
			}
			if !found {
				for _, candidate := range s.Groups {
					if candidate.Name == "PROXY" {
						g = candidate
						found = true
						break
					}
				}
			}
			if !found {
				for _, candidate := range s.Groups {
					if candidate.Type == "Selector" {
						g = candidate
						break
					}
				}
			}
			groupName, node, groupStatus = g.Name, g.Now, tr("dashboard.groups.ready")
		}
	}
	if !s.Installed {
		groupName, node, groupStatus = "--", "--", tr("dashboard.groups.notInstalled")
	} else if s.ServiceError != "" {
		groupName, node, groupStatus = "--", "--", tr("dashboard.groups.serviceUnavailable")
	} else if s.Service.Active && s.APIError != "" {
		groupName, node, groupStatus = "--", "--", tr("dashboard.groups.apiUnavailable")
	}
	groups := []string{title(tr("dashboard.groups.title")), style(groupStatus, muted)}
	if groupName != "--" {
		groups = append(groups, "", label(tr("dashboard.groups.group"), groupName), label(tr("dashboard.groups.node"), node))
	} else if !s.Service.Active {
		hint := tr("dashboard.groups.startHint")
		if !s.Installed {
			hint = tr("dashboard.groups.installHint")
		}
		groups = append(groups, style(hint, muted))
	}
	observed := o.Runtime
	status := runtimeStatus(observed, o.RuntimeReceived, o.RuntimePending, s.Installed, o.Locale)
	traffic := []string{title(tr("dashboard.traffic.title")), style(status, muted)}
	if o.RuntimeReceived && observed.State == "ready" {
		traffic = append(traffic, "")
		for _, field := range overviewTrafficFields(o) {
			traffic = append(traffic, label(field.label, field.value))
		}
		traffic = append(traffic, style(tr("dashboard.traffic.resetHint"), muted))
	} else {
		traffic = append(traffic, style(tr("dashboard.traffic.startHint"), muted))
	}
	for _, issue := range []struct {
		text  string
		cause error
	}{{observed.Error, observed.Diagnostic}, {observed.ConnectionsError, observed.ConnectionsDiagnostic}, {observed.TrafficError, observed.TrafficDiagnostic}, {observed.MemoryError, observed.MemoryDiagnostic}} {
		if issue.text != "" {
			traffic = append(traffic, style(o.Locale.Diagnostic(issue.text, issue.cause), errorColor))
			break
		}
	}
	coreStatus, color := coreSummary(s, o.Locale)
	kernel := []string{title(tr("dashboard.kernel.environment")), style(coreStatus, color), ""}
	for _, field := range overviewEnvironmentFields(s, o.Locale) {
		kernel = append(kernel, label(field.label, field.value))
	}
	if s.Installed && s.Drift != "" {
		kernel = append(kernel, style(clean(o.Locale.Detail(s.Drift)), warning))
	}
	return [][]string{profile, groups, traffic, kernel}
}

func settingsContent(o dashboardOptions, width int) (string, []hitRegion) {
	tr := o.Locale.T
	var b dashboardBlock
	inner := max(1, width-4)
	label := func(s string) string { return themed(s, muted, panel, o.Dim) }
	lines := []string{themed(lipgloss.NewStyle().Bold(true).Render(tr("dashboard.settings.title")), text, panel, o.Dim)}
	lines = append(lines, label(tr("dashboard.settings.theme", themeName)))
	info := tr("dashboard.settings.description")
	lines = append(lines, strings.Split(wrapText(label(info), inner), "\n")...)
	lines = append(lines, "")
	type settingControl struct {
		id, label string
		enabled   bool
	}
	controls := []settingControl{{"install", map[bool]string{false: tr("dashboard.action.install"), true: tr("dashboard.settings.manage")}[o.Snapshot.Installed], true}, {"network", tr("dashboard.action.network"), true}, {"import", tr("dashboard.action.import"), true}, {"runtime-settings", tr("dashboard.settings.ports"), o.Snapshot.Installed}, {"start", tr("dashboard.settings.start"), o.Snapshot.Installed && !o.Snapshot.Service.Active}, {"stop", tr("dashboard.settings.stop"), o.Snapshot.Installed && (o.Snapshot.Service.Active || o.Snapshot.Desired.Running)}, {"restart", tr("dashboard.action.restart"), o.Snapshot.Installed}}
	if o.Snapshot.Installed {
		autostartID, autostartLabel := "autostart-enable", tr("dashboard.settings.enableAutostart")
		if o.Snapshot.Service.Enabled {
			autostartID, autostartLabel = "autostart-disable", tr("dashboard.settings.disableAutostart")
		}
		controls = append(controls, settingControl{autostartID, autostartLabel, true})
		for _, mode := range []string{"rule", "global", "direct"} {
			controls = append(controls, settingControl{"mode." + mode, tr("dashboard.settings.useMode." + mode), true})
		}
	}
	for _, control := range controls {
		if !control.enabled || o.Header.owns(control.id) {
			continue
		}
		options := dashboardButtonOptions(control.id, o, control.id == "install", panel)
		options.Disabled = o.Busy
		value := cellCrop(renderButton(control.label, options), inner)
		if !o.Busy {
			b.targets = append(b.targets, hitRegion{ID: control.id, X: 2, Y: 1 + len(lines), W: min(inner, ansi.StringWidth(value)), H: 1})
		}
		lines = append(lines, value, "")
	}
	if o.Snapshot.Installed {
		lines = append(lines, label(tr("dashboard.settings.installed", clean(o.Snapshot.Version))))
		if o.Snapshot.RunningVersion != "" {
			lines = append(lines, label(tr("dashboard.settings.running", clean(o.Snapshot.RunningVersion))))
		}
		lines = append(lines, strings.Split(wrapText(label(tr("dashboard.settings.controller", clean(o.Snapshot.Controller))), inner), "\n")...)
		lines = append(lines, label(tr("dashboard.settings.desiredMode", dashboardModeLabel(o.Snapshot.Mode.Desired, o.Locale))))
		if o.Snapshot.Mode.Actual != "" {
			lines = append(lines, label(tr("dashboard.settings.actualMode", dashboardModeLabel(o.Snapshot.Mode.Actual, o.Locale))))
		}
		lines = append(lines, label(tr("dashboard.settings.tunUnsupported")))
		lines = append(lines, strings.Split(wrapText(label(tr("dashboard.settings.removeHint")), inner), "\n")...)
		if o.Snapshot.Service.Result != "" && o.Snapshot.Service.Result != "success" {
			lines = append(lines, label(tr("dashboard.settings.serviceResult", clean(o.Snapshot.Service.Result), o.Snapshot.Service.ExitCode)))
		}
		if o.Snapshot.Drift != "" {
			lines = append(lines, strings.Split(wrapText(themed(clean(o.Locale.Detail(o.Snapshot.Drift)), warning, panel, o.Dim), inner), "\n")...)
		}
	}
	if o.Snapshot.ServiceError != "" {
		lines = append(lines, strings.Split(wrapText(themed(clean(o.Locale.Diagnostic(o.Snapshot.ServiceError, o.Snapshot.ServiceDiagnostic)), errorColor, panel, o.Dim), inner), "\n")...)
	}
	if o.Snapshot.Installed && !o.Snapshot.Service.Linger {
		issue := tr("dashboard.settings.lingerDisabled")
		if !o.Snapshot.Service.LingerKnown {
			issue = tr("dashboard.settings.lingerUnknown")
		}
		lines = append(lines, strings.Split(wrapText(label(issue), inner), "\n")...)
	}
	b.lines = panelFrame(lines, width, len(lines)+2, o)
	result := interfaceSettings(o, width)
	result.blank()
	result.append(languageSettings(o, width))
	result.blank()
	result.append(b)
	return strings.Join(result.lines, "\n"), result.targets
}

func interfaceSettings(o dashboardOptions, width int) dashboardBlock {
	state := o.Locale.T("dashboard.interface.keyHintsOn")
	if o.HideKeyHints {
		state = o.Locale.T("dashboard.interface.keyHintsOff")
	}
	lines := []string{
		themed(o.Locale.T("dashboard.interface.keyHints")+": "+state, text, panel, o.Dim),
		themed(o.Locale.T("dashboard.interface.keyHintsHint"), muted, panel, o.Dim),
	}
	return overviewControlPanel(o, width, o.Locale.T("dashboard.interface.title"), lines, []overviewControlButton{
		{id: "key-hints.toggle", label: state, enabled: true, selected: !o.HideKeyHints},
	})
}

func languageSettings(o dashboardOptions, width int) dashboardBlock {
	name := "English"
	if o.Locale.Language() == "zh-Hans" {
		name = "简体中文"
	}
	lines := []string{
		themed(o.Locale.T("dashboard.language.current", name), text, panel, o.Dim),
		themed(o.Locale.T("dashboard.language.savedHint"), muted, panel, o.Dim),
	}
	buttons := []overviewControlButton{
		{id: "language.en", label: "English", enabled: !o.Busy, selected: o.Locale.Language() == "en"},
		{id: "language.zh-Hans", label: "简体中文", enabled: !o.Busy, selected: o.Locale.Language() == "zh-Hans"},
	}
	return overviewControlPanel(o, width, o.Locale.T("dashboard.language.title"), lines, buttons)
}

func dashboardModeLabel(mode string, locale i18n.Localizer) string {
	switch mode {
	case "rule", "global", "direct":
		return locale.T("dashboard.mode." + mode)
	default:
		return clean(mode)
	}
}

func dashboardLocale(locales []i18n.Localizer) i18n.Localizer {
	if len(locales) > 0 {
		return locales[0]
	}
	return i18n.Localizer{}
}
