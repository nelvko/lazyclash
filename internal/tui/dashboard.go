package tui

import (
	"fmt"
	"runtime"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

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
	SidebarOpen                     bool
	Runtime                         app.RuntimeSnapshot
	RuntimeReceived, RuntimePending bool
	// Body and its targets are relative to the first content row beneath the
	// page header. They are used for Nodes and Subscriptions pages only.
	Body        string
	BodyTargets []hitRegion
}

type dashboardLayout struct {
	x, width, y, height int
	sidebar             bool
}

func dashboardGeometry(o dashboardOptions) dashboardLayout {
	sidebar := o.Width >= 100 && o.Height >= 20
	available := max(1, o.Width-4)
	left := 2
	if sidebar {
		available = max(1, o.Width-27)
		left = 24
	}
	width := min(112, available)
	left += (available - width) / 2
	return dashboardLayout{x: left, width: width, y: 4, height: max(0, o.Height-7), sidebar: sidebar}
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
		put(0, 0, themed(cellCrop("lazyclash / "+pageTitle(o.Page), o.Width), accent, bg, o.Dim))
		if o.Height > 1 {
			put(0, o.Height-1, themed(cellCrop("Menu · q Quit", o.Width), muted, bg, o.Dim))
			add(hitRegion{ID: "menu", X: 0, Y: o.Height - 1, W: min(4, o.Width), H: 1})
		}
		return strings.Join(rows, "\n"), targets, 0
	}
	header := lipgloss.NewStyle().Bold(true).Render("lazyclash")
	if g.sidebar {
		put(2, 1, themed(header, muted, bg, o.Dim))
		put(g.x, 1, themed(lipgloss.NewStyle().Bold(true).Render(pageTitle(o.Page)), text, bg, o.Dim))
	} else {
		label := dashboardButton("menu", "Menu", o, false)
		put(1, 1, label)
		add(hitRegion{ID: "menu", X: 1, Y: 1, W: ansi.StringWidth(label), H: 1})
		put(10, 1, themed(cellCrop("lazyclash / "+pageTitle(o.Page), max(0, o.Width-12)), text, bg, o.Dim))
	}
	platform := "LOCAL / " + runtime.GOOS + "/" + runtime.GOARCH
	if g.sidebar || o.Width >= 90 {
		put(max(g.x+len(pageTitle(o.Page))+3, o.Width-2-len(platform)), 1, themed(platform, muted, bg, o.Dim))
	}
	put(1, 2, themed(strings.Repeat("─", max(0, o.Width-2)), border, bg, o.Dim))

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
		sidebarWidth := min(22, o.Width)
		// Underlying buttons must not receive a click through the navigation menu.
		kept := targets[:0]
		for _, r := range targets {
			if !regionsOverlap(r, hitRegion{X: 0, Y: 3, W: sidebarWidth, H: max(0, o.Height-6)}) {
				kept = append(kept, r)
			}
		}
		targets = append(kept, drawSidebar(rows, o, true)...)
	}
	put(1, o.Height-3, themed(strings.Repeat("─", max(0, o.Width-2)), border, bg, o.Dim))
	status := o.Message
	color := muted
	if o.Failure != "" {
		status = "Error: " + clean(o.Failure)
		color = errorColor
	}
	if status == "" {
		if !o.Snapshot.Installed {
			status = "You can explore the app before completing setup."
		} else if o.Snapshot.Service.Active {
			status = "Mihomo is running. Select a group in Nodes to switch its node."
		} else {
			status = "Core is stopped. Start it from Overview or Settings."
		}
	}
	put(2, o.Height-2, themed(cellCrop(clean(status), max(1, o.Width-4)), color, bg, o.Dim))
	help := "Tab Focus  Enter Open  ↑↓ Scroll  ? Help"
	if o.Width < 60 {
		help = "Tab Focus  Enter Open  ↑↓ Scroll"
	}
	quit := "q Quit"
	put(2, o.Height-1, helpText(cellCrop(help, max(1, o.Width-len(quit)-6)), bg, o.Dim))
	if o.Width >= 16 {
		put(o.Width-len(quit)-2, o.Height-1, helpText(quit, bg, o.Dim))
	}
	return strings.Join(rows, "\n"), targets, total
}

func pageTitle(page string) string {
	switch page {
	case "nodes":
		return "Nodes"
	case "profiles":
		return "Subscriptions"
	case "settings":
		return "Settings"
	case "rules":
		return "Rules"
	case "connections":
		return "Connections"
	default:
		return "Overview"
	}
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
	width := min(22, o.Width)
	end := o.Height - 3
	var targets []hitRegion
	if width < 4 || end < 4 {
		return targets
	}
	for y := 3; y < end; y++ {
		putRows(rows, 0, y, themed(strings.Repeat(" ", width), text, sidebarBackground, o.Dim), o.Width)
	}
	for y := 3; y < end; y++ {
		putRows(rows, width-1, y, themed("│", divider, sidebarBackground, o.Dim), o.Width)
	}
	for i, page := range []string{"overview", "nodes", "profiles", "settings", "connections", "rules"} {
		y := 4 + i*2
		if y >= end {
			break
		}
		id := "nav." + page
		label := fmt.Sprintf("%d %s", i+1, pageTitle(page))
		background, foreground := sidebarBackground, muted
		if page == o.Page {
			background, foreground = selectionBackground, selectionForeground
			label += " •"
		}
		if o.Hover == id && o.Focus != id {
			foreground = text
			background = hoverBackground
		}
		if o.Focus == id {
			label = "› " + label
			if page != o.Page {
				foreground = focus
			}
		}
		label = cellPad(cellCrop(label, width-4), width-4)
		if o.Focus == id {
			label = lipgloss.NewStyle().Underline(true).Render(label)
		}
		putRows(rows, 1, y, themed(" "+label+" ", foreground, background, o.Dim), o.Width)
		targets = append(targets, hitRegion{ID: id, X: 1, Y: y, W: width - 2, H: 1})
	}
	if end >= 20 {
		status, color := "Not installed", muted
		if o.Snapshot.Installed {
			status, color = "Stopped", muted
			if o.Snapshot.Service.Active {
				status, color = "Running", success
			}
			if o.Snapshot.ServiceError != "" {
				status, color = "Unavailable", errorColor
			}
		}
		putRows(rows, 2, end-4, themed(strings.Repeat("─", width-5), divider, sidebarBackground, o.Dim), o.Width)
		putRows(rows, 2, end-3, themed("Core", muted, sidebarBackground, o.Dim), o.Width)
		putRows(rows, 2, end-2, themed(status, color, sidebarBackground, o.Dim), o.Width)
	}
	return targets
}

func dashboardButton(id, label string, o dashboardOptions, primary bool) string {
	foreground, background := text, panel
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
			foreground, background = focus, panel
		}
	}
	return themed(value, foreground, background, o.Dim)
}

type dashboardBlock struct {
	lines   []string
	targets []hitRegion
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
	cards := overviewCards(o)
	columns := 1
	if width >= 76 && o.Height >= 24 {
		columns = 2
	}
	cardWidth := width
	if columns == 2 {
		cardWidth = (width - 2) / 2
	}
	content := make([][]string, len(cards))
	height := 8
	for i, c := range cards {
		for _, line := range c {
			content[i] = append(content[i], strings.Split(wrapText(line, max(1, cardWidth-4)), "\n")...)
		}
		height = max(height, len(content[i])+2)
	}
	for i := 0; i < len(cards); i += columns {
		left := panelFrame(content[i], cardWidth, height, o)
		if columns == 1 {
			body.lines = append(body.lines, left...)
		} else {
			right := panelFrame(content[i+1], width-cardWidth-2, height, o)
			for y := range left {
				body.lines = append(body.lines, left[y]+themed("  ", text, bg, o.Dim)+right[y])
			}
		}
		if i+columns < len(cards) {
			body.blank()
		}
	}
	body.blank()
	body.lines = append(body.lines, themed(cellCrop("You can explore the app before completing setup.", width), muted, bg, o.Dim))
	return strings.Join(body.lines, "\n"), body.targets
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
	subscription := []string{title("Current subscription"), style(configured, text), "", label("Active profile", active), label("Saved profiles", fmt.Sprint(saved)), style("Add or import a configuration", muted)}
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
	if s.ServiceError != "" {
		groupName, node, groupStatus = "--", "--", "Service unavailable"
	} else if s.Service.Active && s.APIError != "" {
		groupName, node, groupStatus = "--", "--", "API unavailable"
	}
	groups := []string{title("Selected proxy group"), style(groupStatus, muted), "", label("Group", groupName), label("Current node", node), style("Open Nodes to browse proxy groups", muted)}
	observed := o.Runtime
	status := runtimeStatus(observed, o.RuntimeReceived, o.RuntimePending, s.Installed)
	connections, rate, totals, memory := "--", "--", "--", "--"
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
	traffic := []string{title("Traffic statistics"), style(status, muted), "", label("Connections", connections), label("Upload / download", rate), label("Since core start", totals), label("Core memory", memory), style("Totals reset when the core restarts", muted)}
	for _, issue := range []string{observed.Error, observed.ConnectionsError, observed.TrafficError, observed.MemoryError} {
		if issue != "" {
			traffic = append(traffic, style(issue, errorColor))
			break
		}
	}
	coreStatus, color := "Not installed", muted
	if s.Installed {
		coreStatus, color = "Stopped", muted
		if s.Service.Active {
			coreStatus, color = "Running", success
		}
		if s.ServiceError != "" {
			coreStatus, color = "Unavailable", errorColor
		}
	}
	kernel := []string{title("Kernel and environment"), style(coreStatus, color), "", label("Platform", runtime.GOOS+"/"+runtime.GOARCH), label("System proxy", "Unchanged"), label("TUN", "Off")}
	if s.Installed {
		kernel = append(kernel, label("Installed version", s.Version))
		if s.Service.Active {
			running := s.RunningVersion
			if running == "" {
				running = "Unavailable"
			}
			kernel = append(kernel, label("Running version", running))
		}
	}
	return [][]string{subscription, groups, traffic, kernel}
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
	controls := []struct {
		id, label string
		enabled   bool
	}{{"install", map[bool]string{false: "Install core", true: "Manage / Update core"}[o.Snapshot.Installed], true}, {"network", "Network settings", true}, {"import", "Import config", true}, {"start", "Start", o.Snapshot.Installed && !o.Snapshot.Service.Active}, {"stop", "Stop", o.Snapshot.Installed && o.Snapshot.Service.Active}, {"restart", "Restart", o.Snapshot.Installed}}
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
	}
	if o.Snapshot.ServiceError != "" {
		lines = append(lines, strings.Split(wrapText(themed(clean(o.Snapshot.ServiceError), errorColor, panel, o.Dim), inner), "\n")...)
	}
	if o.Snapshot.Installed && !o.Snapshot.Service.Linger {
		lines = append(lines, strings.Split(wrapText(label("Linger is disabled; the user service may stop after logout."), inner), "\n")...)
	}
	b.lines = panelFrame(lines, width, len(lines)+2, o)
	return strings.Join(b.lines, "\n"), b.targets
}
