package tui

import (
	"fmt"
	"runtime"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

const logo = "la💤yClash"

const sidebarWidth = 28

func logoText(background string, dimmed bool) string {
	return themed(lipgloss.NewStyle().Bold(true).Render(logo), accent, background, dimmed)
}

type hitRegion struct {
	ID         string
	X, Y, W, H int
}
type dashboardOptions struct {
	Width, Height                   int
	Page                            string
	SelectedGroup                   string
	Snapshot                        app.Snapshot
	Focus                           string
	Hover                           string
	GuideDismissed                  bool
	Scroll                          int
	Dim                             bool
	Message, Failure                string
	Help                            string
	SidebarOpen                     bool
	Busy                            bool
	Runtime                         app.RuntimeSnapshot
	RuntimeReceived, RuntimePending bool
	// Body and its targets are relative to the first content row beneath the
	// page header. They are used by the Proxies, Profiles, Connections, Rules and Logs pages.
	Body        string
	BodyTargets []hitRegion
}

type dashboardLayout struct {
	x, width, y, height   int
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
	width := min(112, available)
	if o.Page == "" || o.Page == "overview" {
		width = available
		left = (o.Width - width) / 2
		if sidebar {
			left = sidebarWidth + (o.Width-sidebarWidth-width)/2
		}
	}
	headerY, bodyY := 0, 3
	if o.Height >= 24 {
		headerY, bodyY = 1, 5
	}
	return dashboardLayout{x: left, width: width, y: bodyY, height: max(0, o.Height-bodyY-2), headerY: headerY, headerHeight: bodyY - 2, sidebar: sidebar}
}
func dashboardBodyWidth(o dashboardOptions) int { return dashboardGeometry(o).width }

func renderDashboard(o dashboardOptions) (string, []hitRegion, int) {
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
	if o.Height < 7 || o.Width < 20 {
		put(0, 0, themed(cellCrop(logoText(bg, o.Dim)+" / "+pageTitle(o.Page), o.Width), text, bg, o.Dim))
		if o.Height > 1 {
			put(0, o.Height-1, themed(cellCrop("Menu · q Quit", o.Width), muted, bg, o.Dim))
			add(hitRegion{ID: "menu", X: 0, Y: o.Height - 1, W: min(4, o.Width), H: 1})
		}
		return strings.Join(rows, "\n"), targets, 0
	}
	titleX := g.x
	titleText := pageTitle(o.Page)
	if !g.sidebar {
		label := dashboardButton("menu", "Menu", o, false)
		put(1, g.headerY, label)
		add(hitRegion{ID: "menu", X: 1, Y: g.headerY, W: ansi.StringWidth(label), H: 1})
		titleX = max(12, g.x)
		if o.Width >= 50 {
			titleText = logoText(bg, o.Dim) + " / " + titleText
		}
	}
	put(titleX, g.headerY, themed(cellCrop(titleText, max(1, g.x+g.width-titleX)), text, bg, o.Dim))
	status, statusColor := coreSummary(o.Snapshot)
	status = "Mihomo · " + status
	statusX := g.x + g.width - ansi.StringWidth(status)
	if statusX > titleX+ansi.StringWidth(titleText)+2 {
		put(statusX, g.headerY, themed(status, statusColor, bg, o.Dim))
	}
	// A top-edge stroke aligns with the sidebar's background boundary.
	put(g.x, g.headerHeight, themed(strings.Repeat("▔", g.width), divider, bg, o.Dim))

	var body string
	var bodyTargets []hitRegion
	switch o.Page {
	case "overview":
		body, bodyTargets = overviewContent(o, g.width)
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
			if !regionsOverlap(r, hitRegion{X: 0, Y: g.y - 1, W: menuWidth, H: max(0, o.Height-g.y-1)}) {
				kept = append(kept, r)
			}
		}
		targets = append(kept, drawSidebar(rows, o, true)...)
	}
	status = o.Message
	color := muted
	if status == "State refreshed" {
		status = ""
	}
	if o.Failure != "" {
		status = "Error: " + clean(o.Failure)
		color = errorColor
	}
	put(2, o.Height-2, themed(cellCrop(clean(status), max(1, o.Width-4)), color, bg, o.Dim))
	help := o.Help
	if help == "" {
		help = "Tab Focus · Enter Open · ? Help"
	}
	quit := "q Quit"
	put(2, o.Height-1, helpText(cellCrop(help, max(1, o.Width-len(quit)-6)), bg, o.Dim))
	if o.Width >= 16 {
		put(o.Width-len(quit)-2, o.Height-1, helpText(quit, bg, o.Dim))
	}

	return strings.Join(rows, "\n"), targets, total
}

// navPages defines both visible order and the 1–7 page shortcuts.
func navPages() []string {
	return []string{"overview", "proxies", "profiles", "connections", "rules", "logs", "settings"}
}

func pageTitle(page string) string {
	switch page {
	case "proxies":
		return "Proxies"
	case "profiles":
		return "Profiles"
	case "settings":
		return "Settings"
	case "rules":
		return "Rules"
	case "connections":
		return "Connections"
	case "logs":
		return "Logs"
	default:
		return "Overview"
	}
}

func coreSummary(s app.Snapshot) (string, string) {
	if !s.Installed {
		return "Not installed", muted
	}
	if s.ServiceError != "" {
		return "Unavailable", errorColor
	}
	if s.Service.Active {
		return "Running", success
	}
	switch s.Service.State {
	case "activating":
		return "Starting", warning
	case "deactivating":
		return "Stopping", warning
	case "reloading":
		return "Reloading", warning
	case "failed":
		return "Failed", errorColor
	case "unknown", "unsupported":
		return "Unavailable", errorColor
	}
	return "Stopped", muted
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

func drawSidebar(rows []string, o dashboardOptions, popup bool) []hitRegion {
	width := min(sidebarWidth, o.Width)
	end := o.Height - 2
	g := dashboardGeometry(o)
	var targets []hitRegion
	if width < 4 || end < 4 {
		return targets
	}
	start := g.y - 1
	if !popup {
		start = 0
	}
	for y := start; y < end; y++ {
		putRows(rows, 0, y, themed(strings.Repeat(" ", width), text, sidebarBackground, o.Dim), o.Width)
		if popup {
			putRows(rows, width-1, y, themed("│", divider, sidebarBackground, o.Dim), o.Width)
		}
	}
	if !popup {
		header := lipgloss.NewStyle().Width(width - 1).Align(lipgloss.Center).Render(logoText(sidebarBackground, o.Dim))
		putRows(rows, 0, g.headerY, themed(header, accent, sidebarBackground, o.Dim), o.Width)
	}
	pages := navPages()
	menuY := g.y
	if !popup {
		// Permanent navigation starts directly at the shared header edge.
		menuY = g.headerHeight
	}
	availableRows := max(0, end-menuY)
	if availableRows == 0 {
		return targets
	}
	// Consecutive menu rows share an edge; padding stays inside each item.
	itemHeight := 1
	if availableRows >= len(pages)*3 {
		itemHeight = 3
	}
	first, last := 0, len(pages)
	if availableRows < len(pages) {
		selected := 0
		for i, page := range pages {
			if o.Page == page {
				selected = i
			}
		}
		for i, page := range pages {
			if o.Focus == "nav."+page {
				selected = i
			}
		}
		// Keep the focused item visible in short navigation popups.
		first = max(0, min(selected-availableRows/2, len(pages)-availableRows))
		last = first + availableRows
	}
	y := menuY
	for i := first; i < last; i++ {
		page := pages[i]
		if y+itemHeight > end {
			break
		}
		id := "nav." + page
		label := pageTitle(page)
		labelStyle := lipgloss.NewStyle()
		current := "  "
		background, foreground := sidebarBackground, muted
		if page == o.Page {
			background, foreground = bg, selectionForeground
			if popup {
				background = selectionBackground
			}
			labelStyle = labelStyle.Bold(true)
			current = " •"
		}
		if o.Hover == id && o.Focus != id && page != o.Page {
			foreground = text
			if popup {
				background = hoverBackground
			} else {
				labelStyle = labelStyle.Underline(true)
			}
		}
		marker := "  "
		if o.Focus == id {
			marker = "› "
			if page != o.Page {
				foreground = focus
			}
			labelStyle = labelStyle.Underline(true)
		}
		label = labelStyle.Render(label)
		// Equal marker space keeps the page name centered as selection changes.
		value := lipgloss.NewStyle().Width(width - 2).Align(lipgloss.Center).Render(marker + cellCrop(label, max(1, width-6)) + current)
		value = themed(value, foreground, background, o.Dim)
		for row := 0; row < itemHeight; row++ {
			if !popup {
				putRows(rows, 0, y+row, themed(strings.Repeat(" ", width), foreground, background, o.Dim), o.Width)
			}
			line := themed(strings.Repeat(" ", width-2), foreground, background, o.Dim)
			if row == itemHeight/2 {
				line = value
			}
			putRows(rows, 1, y+row, line, o.Width)
		}
		x, hitWidth := 0, width
		if popup {
			x, hitWidth = 1, width-2
		}
		targets = append(targets, hitRegion{ID: id, X: x, Y: y, W: hitWidth, H: itemHeight})
		y += itemHeight
	}
	if first > 0 {
		putRows(rows, width-2, menuY-1, themed("↑", muted, sidebarBackground, o.Dim), o.Width)
	}
	if last < len(pages) {
		putRows(rows, width-2, end-1, themed("↓", muted, sidebarBackground, o.Dim), o.Width)
	}
	if !popup {
		footer := sidebarSummary(o, width)
		footerY := end - len(footer) - 1
		// Use spare space without reducing the navigation's size or spacing.
		if footerY >= y+1 {
			for row, line := range footer {
				putRows(rows, 2, footerY+row, line, o.Width)
			}
		}
	}
	return targets
}

func sidebarSummary(o dashboardOptions, width int) []string {
	inner := max(1, width-4)
	s := o.Snapshot
	style := func(value, color string) string {
		return themed(value, color, sidebarBackground, o.Dim)
	}
	lines := []string{style(strings.Repeat("─", inner), divider)}
	if !s.Installed {
		return append(lines, style(cellPad("Mihomo not installed", inner), muted))
	}
	pair := func(label, value string) string {
		value = cellCrop(clean(value), max(1, inner-ansi.StringWidth(label)-1))
		gap := max(1, inner-ansi.StringWidth(label)-ansi.StringWidth(value))
		return style(label, muted) + style(strings.Repeat(" ", gap)+value, text)
	}
	profile := clean(s.Applied)
	if profile == "" {
		profile = "Not configured"
		if s.ConfigReady {
			profile = "default"
		}
	}
	mode := map[string]string{"rule": "Rule", "global": "Global", "direct": "Direct"}[s.Mode.Desired]
	if mode == "" {
		mode = "Unknown"
	}
	version := clean(s.Version)
	if version == "" {
		version = "Unknown"
	}
	return append(lines, pair("Profile", profile), pair("Saved mode", mode), pair("Installed", version))
}

func dashboardButton(id, label string, o dashboardOptions, primary bool) string {
	surface := panel
	if id == "menu" {
		surface = bg
	}
	foreground, background := text, surface
	if primary {
		foreground, background = primaryButtonForeground, primaryButtonBackground
	}
	value := "[ " + label + " ]"
	if o.Hover == id && o.Focus != id {
		if !primary {
			foreground, background = text, hoverBackground
		}
		value = lipgloss.NewStyle().Underline(true).Render(value)
	}
	if o.Focus == id {
		value = "›" + value
		value = lipgloss.NewStyle().Underline(true).Bold(true).Render(value)
		if !primary {
			foreground, background = focus, surface
		}
	}
	return themed(value, foreground, background, o.Dim)
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

func panelFrame(lines []string, width, height int, o dashboardOptions) []string {
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
	result[0] = themed("┌"+strings.Repeat("─", width-2)+"┐", divider, panel, o.Dim)
	for i := 1; i < height-1; i++ {
		content := ""
		if i-1 < len(lines) {
			content = lines[i-1]
		}
		result[i] = themed("│ ", divider, panel, o.Dim) + themed(cellPad(content, width-4), text, panel, o.Dim) + themed(" │", divider, panel, o.Dim)
	}
	result[height-1] = themed("└"+strings.Repeat("─", width-2)+"┘", divider, panel, o.Dim)
	return result
}

func overviewContent(o dashboardOptions, width int) (string, []hitRegion) {
	var body dashboardBlock
	s := o.Snapshot
	if !o.GuideDismissed && !s.GuideDismissed && (!s.Installed || !s.ConfigReady || (!s.HasRun && !s.Service.Active)) {
		body.append(getStarted(o, width))
		body.blank()
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
				value := cellCrop(dashboardButton(actionID, label, o, false), max(1, cardWidth-4))
				content[index] = append(content[index], "", value)
				actions = append(actions, hitRegion{ID: actionID, X: 2 + column*(cardWidth+2), W: max(1, ansi.StringWidth(value)), H: 1})
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
	s := o.Snapshot
	switch index {
	case 0:
		return "card.import", "Import config"
	case 1:
		if s.Service.Active && len(s.Groups) > 0 {
			return "card.proxies", "Open Proxies"
		}
	}
	return "", ""
}

type overviewField struct{ label, value string }

func overviewTrafficFields(o dashboardOptions) []overviewField {
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
	return []overviewField{{"Upload / download", rate}, {"Since core start", totals}, {"Connections", connections}, {"Core memory", memory}}
}

func overviewEnvironmentFields(s app.Snapshot) []overviewField {
	fields := []overviewField{{"Platform", runtime.GOOS + "/" + runtime.GOARCH}, {"System proxy", "Unchanged"}, {"TUN", "Unsupported in user service"}}
	if s.Installed {
		fields = append(fields, overviewField{"Installed version", s.Version})
		if s.Service.Active {
			running := s.RunningVersion
			if running == "" {
				running = "Unavailable"
			}
			fields = append(fields, overviewField{"Running version", running})
		}
		if s.Desired.Known {
			desired := "Stopped"
			if s.Desired.Running {
				desired = "Running"
			}
			fields = append(fields, overviewField{"Desired state", desired})
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

func getStarted(o dashboardOptions, width int) dashboardBlock {
	title, description := "Install your first proxy core", "Then add a subscription or import a local configuration."
	primaryID, primaryLabel := "install", "Install core"
	secondaryID, secondaryLabel := "import", "Import config"
	step := 1
	if o.Snapshot.Installed {
		step = 2
		title = "Choose your first configuration"
		description = "Import Mihomo YAML, or use the built-in DIRECT configuration."
		primaryID, primaryLabel = "import", "Import config"
		secondaryID, secondaryLabel = "start", "Use default"
		if o.Snapshot.ConfigReady {
			step = 3
			title = "Start your proxy core"
			description = "Your configuration is ready. Start Mihomo to browse and select nodes."
			primaryID, primaryLabel = "start", "Start core"
			secondaryID, secondaryLabel = "import", "Import config"
		}
	}
	inner := max(1, width-4)
	heading := themed(lipgloss.NewStyle().Bold(true).Render("Get started"), text, panel, o.Dim)
	steps := fmt.Sprintf("%d / 3 · Core > Config > Start", step)
	if inner >= 55 {
		heading = cellPad(heading, inner-ansi.StringWidth(steps)) + themed(steps, muted, panel, o.Dim)
	}
	lines := []string{heading}
	lines = append(lines, strings.Split(wrapText(themed(lipgloss.NewStyle().Bold(true).Render(title), text, panel, o.Dim), inner), "\n")...)
	lines = append(lines, strings.Split(wrapText(themed(description, muted, panel, o.Dim), inner), "\n")...)
	lines = append(lines, "")
	b := dashboardBlock{}
	buttons := []struct {
		id, label string
		primary   bool
	}{{primaryID, primaryLabel, true}, {secondaryID, secondaryLabel, false}, {"dismiss", "Dismiss", false}}
	x := 0
	buttonLine := ""
	for _, button := range buttons {
		label := dashboardButton(button.id, button.label, o, button.primary)
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
		// Keep the dismiss action right-aligned when all controls fit one row.
		if button.id == "dismiss" && x+w <= inner && inner >= 60 {
			gap := inner - x - w
			buttonLine += themed(strings.Repeat(" ", gap), text, panel, o.Dim)
			x += gap
		}
		b.targets = append(b.targets, hitRegion{ID: button.id, X: 2 + x, Y: 1 + len(lines), W: min(w, inner-x), H: 1})
		buttonLine += label
		x += w
	}
	lines = append(lines, cellPad(buttonLine, inner))
	b.lines = panelFrame(lines, width, len(lines)+2, o)
	return b
}

func overviewCards(o dashboardOptions) [][]string {
	s := o.Snapshot
	style := func(value, color string) string { return themed(clean(value), color, panel, o.Dim) }
	title := func(value string) string { return lipgloss.NewStyle().Bold(true).Render(style(value, text)) }
	label := func(name, value string) string { return style(cellPad(name, 20), muted) + style(value, text) }
	configured, active := "Not configured", "None"
	if s.ConfigReady {
		configured = "Configuration ready"
		active = s.Applied
		if active == "" {
			active = "default"
		}
	}
	saved := 0
	for _, name := range s.Profiles {
		if name != "default" {
			saved++
		}
	}
	profile := []string{title("Current profile"), style(configured, text)}
	if s.ConfigReady || saved > 0 {
		profile = append(profile, "", label("Active profile", active), label("Saved profiles", fmt.Sprint(saved)))
	} else {
		profile = append(profile, style("Import a subscription URL or local YAML.", muted))
	}
	groupName, node, groupStatus := "--", "--", "No active core"
	if s.Service.Active {
		groupStatus = "No proxy groups"
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
			groupName, node, groupStatus = g.Name, g.Now, "Ready to select"
		}
	}
	if !s.Installed {
		groupName, node, groupStatus = "--", "--", "Core not installed"
	} else if s.ServiceError != "" {
		groupName, node, groupStatus = "--", "--", "Service unavailable"
	} else if s.Service.Active && s.APIError != "" {
		groupName, node, groupStatus = "--", "--", "API unavailable"
	}
	groups := []string{title("Selected proxy group"), style(groupStatus, muted)}
	if groupName != "--" {
		groups = append(groups, "", label("Group", groupName), label("Current node", node))
	} else if !s.Service.Active {
		hint := "Start the core to browse Proxies."
		if !s.Installed {
			hint = "Install the core to browse Proxies."
		}
		groups = append(groups, style(hint, muted))
	}
	observed := o.Runtime
	status := runtimeStatus(observed, o.RuntimeReceived, o.RuntimePending, s.Installed)
	traffic := []string{title("Traffic statistics"), style(status, muted)}
	if o.RuntimeReceived && observed.State == "ready" {
		traffic = append(traffic, "")
		for _, field := range overviewTrafficFields(o) {
			traffic = append(traffic, label(field.label, field.value))
		}
		traffic = append(traffic, style("Totals reset when the core restarts", muted))
	} else {
		traffic = append(traffic, style("Runtime data appears after the core starts.", muted))
	}
	for _, issue := range []string{observed.Error, observed.ConnectionsError, observed.TrafficError, observed.MemoryError} {
		if issue != "" {
			traffic = append(traffic, style(issue, errorColor))
			break
		}
	}
	coreStatus, color := coreSummary(s)
	kernel := []string{title("Kernel and environment"), style(coreStatus, color), ""}
	for _, field := range overviewEnvironmentFields(s) {
		kernel = append(kernel, label(field.label, field.value))
	}
	if s.Installed && s.Drift != "" {
		kernel = append(kernel, style(clean(s.Drift), warning))
	}
	return [][]string{profile, groups, traffic, kernel}
}

func settingsContent(o dashboardOptions, width int) (string, []hitRegion) {
	var b dashboardBlock
	inner := max(1, width-4)
	label := func(s string) string { return themed(s, muted, panel, o.Dim) }
	lines := []string{themed(lipgloss.NewStyle().Bold(true).Render("Core and configuration"), text, panel, o.Dim)}
	lines = append(lines, label("Theme: "+themeName))
	info := "Install Mihomo, manage its configuration, and control the user service."
	lines = append(lines, strings.Split(wrapText(label(info), inner), "\n")...)
	lines = append(lines, "")
	type settingControl struct {
		id, label string
		enabled   bool
	}
	controls := []settingControl{{"install", map[bool]string{false: "Install core", true: "Manage / Update core"}[o.Snapshot.Installed], true}, {"network", "Network settings", true}, {"import", "Import config", true}, {"runtime-settings", "Runtime settings / Ports", o.Snapshot.Installed}, {"start", "Start", o.Snapshot.Installed && !o.Snapshot.Service.Active}, {"stop", "Stop", o.Snapshot.Installed && (o.Snapshot.Service.Active || o.Snapshot.Desired.Running)}, {"restart", "Restart", o.Snapshot.Installed}}
	if o.Snapshot.Installed {
		autostartID, autostartLabel := "autostart-enable", "Enable autostart"
		if o.Snapshot.Service.Enabled {
			autostartID, autostartLabel = "autostart-disable", "Disable autostart"
		}
		controls = append(controls, settingControl{autostartID, autostartLabel, true})
		for _, mode := range []string{"rule", "global", "direct"} {
			controls = append(controls, settingControl{"mode." + mode, "Use " + mode + " mode", true})
		}
	}
	for _, control := range controls {
		if !control.enabled {
			continue
		}
		value := dashboardButton(control.id, control.label, o, control.id == "install")
		b.targets = append(b.targets, hitRegion{ID: control.id, X: 2, Y: 1 + len(lines), W: min(inner, ansi.StringWidth(value)), H: 1})
		lines = append(lines, value, "")
	}
	if o.Snapshot.Installed {
		lines = append(lines, label("Installed: "+clean(o.Snapshot.Version)))
		if o.Snapshot.RunningVersion != "" {
			lines = append(lines, label("Running: "+clean(o.Snapshot.RunningVersion)))
		}
		lines = append(lines, strings.Split(wrapText(label("Controller: "+clean(o.Snapshot.Controller)), inner), "\n")...)
		lines = append(lines, label("Mode: desired "+clean(o.Snapshot.Mode.Desired)))
		if o.Snapshot.Mode.Actual != "" {
			lines = append(lines, label("Mode: actual "+clean(o.Snapshot.Mode.Actual)))
		}
		lines = append(lines, label("TUN: unsupported in the unprivileged user service"))
		lines = append(lines, strings.Split(wrapText(label("Remove core: stop and disable autostart, then run lazyclash uninstall. Saved profiles are retained."), inner), "\n")...)
		if o.Snapshot.Service.Result != "" && o.Snapshot.Service.Result != "success" {
			lines = append(lines, label(fmt.Sprintf("Last service result: %s (exit %d)", clean(o.Snapshot.Service.Result), o.Snapshot.Service.ExitCode)))
		}
		if o.Snapshot.Drift != "" {
			lines = append(lines, strings.Split(wrapText(themed(clean(o.Snapshot.Drift), warning, panel, o.Dim), inner), "\n")...)
		}
	}
	if o.Snapshot.ServiceError != "" {
		lines = append(lines, strings.Split(wrapText(themed(clean(o.Snapshot.ServiceError), errorColor, panel, o.Dim), inner), "\n")...)
	}
	if o.Snapshot.Installed && !o.Snapshot.Service.Linger {
		issue := "Linger is disabled; enable it with loginctl enable-linger to keep Mihomo running after logout."
		if !o.Snapshot.Service.LingerKnown {
			issue = "Linger status is unknown; run lazyclash doctor before relying on operation after logout."
		}
		lines = append(lines, strings.Split(wrapText(label(issue), inner), "\n")...)
	}
	b.lines = panelFrame(lines, width, len(lines)+2, o)
	return strings.Join(b.lines, "\n"), b.targets
}
