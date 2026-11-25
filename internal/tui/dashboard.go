package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

const (
	sidebarWidth     = 20
	sidebarMinWidth  = 115
	sidebarMinHeight = 9
)

func sidebarVisible(width, height int) bool {
	return width >= sidebarMinWidth && height >= sidebarMinHeight
}

type hitRegion struct {
	ID         string
	X, Y, W, H int
	// Nonzero only for a complete shared input frame. The editor starts two
	// cells from its left edge and one row below its top edge.
	InputWidth int
	// A trailing dropdown aligns its popup to the right edge of its face.
	DropdownRight bool
}
type dashboardOptions struct {
	Locale                          i18n.Localizer
	Theme, EffectiveTheme           string
	MessageTransient                bool
	MessageInformational            bool
	Width, Height                   int
	Page                            string
	SelectedGroup                   string
	Snapshot                        app.Snapshot
	Focus                           string
	OpenDropdown                    string
	Hover                           string
	GuideDismissed                  bool
	GuideSkipped                    bool
	Scroll                          int
	Dim                             bool
	Message, Failure                string
	Notifications                   []notificationItem
	Spinner                         string
	Help                            string
	EditingInput                    bool
	HelpAfterClose                  bool
	HideKeyHints                    bool
	SidebarOpen                     bool
	Sidebar                         *sidebarState
	LogoFrame                       int
	LogoIntroFrame                  int
	Busy                            bool
	CoreAction                      string
	Now                             time.Time
	PlainButtons                    bool
	Monochrome                      bool
	StateUnavailable                bool
	Header                          pageHeader
	Runtime                         app.RuntimeSnapshot
	RuntimeReceived, RuntimePending bool
	TrafficHistory                  []trafficPoint
	TrafficNow                      time.Time
	TrafficScale                    trafficChartScale
	// Body and its targets are relative to the first content row beneath the
	// page header. They are used by the Proxies, Profiles, Connections, Rules and Logs pages.
	Body          string
	BodyTargets   []hitRegion
	BodyRange     bool
	InlineInstall bool
	// BodyTop stays above the scrolling body; its rows share the body width.
	BodyTopRows int
	BodyTop     dashboardBlock
	// A windowed body supplies only these logical rows, while targets retain
	// their positions in the complete body for clipping and keyboard navigation.
	BodyStart, BodyTotal              int
	ProxyStickyGroup, ProxyRangeLabel string
}

type dashboardLayout struct {
	x, width, y, height   int
	headerX, headerWidth  int
	headerY, headerHeight int
	contentY              int // Top of the content area before any fixed toolbars.
	sidebar               bool
}

func dashboardGeometry(o dashboardOptions) dashboardLayout {
	sidebar := sidebarVisible(o.Width, o.Height)
	width := max(1, o.Width-4)
	left := 2
	if sidebar {
		left = sidebarWidth + 2
		width = max(1, o.Width-left-3)
	}
	headerX, headerWidth := left, width
	if o.Page == "" || o.Page == "overview" {
		left = (o.Width - width) / 2
		if sidebar {
			left = sidebarWidth + (o.Width-sidebarWidth-width)/2
		}
		if !sidebar && o.InlineInstall && o.Width < 38 {
			// The compact setup notice needs the full row at very small sizes.
			width, left = o.Width, 0
		}
	}
	headerY, headerHeight := 0, 1
	if o.Height >= 24 {
		headerY, headerHeight = 1, 3
	}
	if (o.Width < 70 || o.Page == "proxies" && o.Width < sidebarMinWidth) && o.Height < 24 && o.Page != "" && o.Page != "overview" {
		headerHeight++
	}
	contentY := headerHeight
	if o.Height < 24 {
		// Short headers have no lower padding, so retain one separating row.
		contentY++
	}
	bodyY := contentY + o.BodyTopRows
	height := max(0, dashboardContentBottom(o)-bodyY)
	if o.BodyRange && height > 1 {
		height--
	}
	return dashboardLayout{
		x: left, width: width, y: bodyY, height: height,
		headerX: headerX, headerWidth: headerWidth, headerY: headerY, headerHeight: headerHeight,
		contentY: contentY, sidebar: sidebar,
	}
}
func dashboardBodyWidth(o dashboardOptions) int { return dashboardGeometry(o).width }

func dashboardContentBottom(o dashboardOptions) int {
	if o.HideKeyHints {
		return o.Height
	}
	return o.Height - 1
}

func dashboardHeaderTitle(o dashboardOptions, g dashboardLayout, page string) (int, string) {
	if g.sidebar {
		return g.headerX, page
	}
	if o.Width >= 50 {
		page = logoText(bg, o.Dim) + " / " + page
	}
	return max(12, g.headerX), page
}

func drawDashboard(o dashboardOptions) (string, []hitRegion, int, []notificationOverlay) {
	tr := o.Locale.T
	if o.Width <= 0 || o.Height <= 0 {
		return "", nil, 0, nil
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
	pageText := themed("▎ ", pageTitleMarker, bg, o.Dim) +
		themed(lipgloss.NewStyle().Bold(true).Render(pageTitle(o.Page, o.Locale)), pageTitleForeground, bg, o.Dim)
	if o.Height < 7 || o.Width < 20 {
		put(0, 0, dashboardTitle(o, logoText(bg, o.Dim)+" / "+pageText, pageText, o.Width))
		if o.Height > 1 {
			put(0, o.Height-1, themed(cellCrop(tr("dashboard.menuCompact"), o.Width), muted, bg, o.Dim))
			add(hitRegion{ID: "menu", X: 0, Y: o.Height - 1, W: min(ansi.StringWidth(tr("dashboard.menu")), o.Width), H: 1})
		}
		return strings.Join(rows, "\n"), targets, 0, nil
	}
	titleX, titleText := dashboardHeaderTitle(o, g, pageText)
	if !g.sidebar {
		label := renderButton(tr("dashboard.menu"), dashboardButtonOptions("menu", o, false, bg))
		put(1, g.headerY, label)
		add(hitRegion{ID: "menu", X: 1, Y: g.headerY, W: ansi.StringWidth(label), H: 1})
	}
	header, headerX, headerY, headerTargets := renderPageHeader(o, g, titleX, ansi.StringWidth(titleText))
	titleRight := g.headerX + g.headerWidth
	if headerY == g.headerY && header != "" {
		titleRight = min(titleRight, headerX-1)
	}
	titleAction := renderPageTitleAction(o)
	actionWidth := ansi.StringWidth(titleAction)
	titleWidth := max(0, titleRight-titleX)
	if actionWidth+2 >= titleWidth {
		titleAction = ""
	}
	if titleAction != "" {
		titleWidth -= actionWidth + 2
	}
	title := dashboardTitle(o, titleText, pageText, titleWidth)
	put(titleX, g.headerY, title)
	if titleAction != "" {
		x := titleX + ansi.StringWidth(title) + 2
		put(x, g.headerY, titleAction)
		if o.Header.titleAction.enabled && !o.Busy {
			add(hitRegion{ID: o.Header.titleAction.id, X: x, Y: g.headerY, W: actionWidth, H: 1})
		}
	}
	put(headerX, headerY, header)
	for _, hit := range headerTargets {
		add(hit)
	}
	for i, line := range o.BodyTop.lines {
		if i < o.BodyTopRows {
			put(g.x, g.contentY+i, cellPad(line, g.width))
		}
	}
	for _, hit := range o.BodyTop.targets {
		hit.X += g.x
		hit.Y += g.contentY
		if hit, ok := clipRegion(hit, g.x, g.contentY, g.width, o.BodyTopRows); ok {
			add(hit)
		}
	}

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
	if o.BodyTotal > 0 {
		total = o.BodyTotal
	}
	scroll := max(0, min(o.Scroll, max(0, total-g.height)))
	o.Scroll = scroll
	if o.Page == "proxies" && o.Width >= sidebarMinWidth {
		group := o.ProxyStickyGroup
		if o.BodyTotal == 0 {
			group = proxyStickyGroup(o.Snapshot.Groups, bodyTargets, scroll, g.height)
		}
		if group != "" {
			// Reuse the space below the title so the list and fixed search
			// stay on the same rows as the label appears and disappears.
			x := 0
			if g.sidebar {
				x = sidebarWidth
			}
			label := strings.Repeat(" ", g.x-x) + tr("proxies.search_group", clean(group))
			put(x, g.contentY-1, themed(cellPad(label, o.Width-x), accent, bg, o.Dim))
		}
	}
	for i := 0; i < g.height && scroll+i < total; i++ {
		index := scroll + i - o.BodyStart
		if index >= 0 && index < len(bodyLines) {
			put(g.x, g.y+i, cellPad(bodyLines[index], g.width))
		}
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
	if o.BodyRange && g.y+g.height < dashboardContentBottom(o) {
		label := o.ProxyRangeLabel
		if o.BodyTotal == 0 {
			label = proxyVisibleRange(o.Locale, bodyTargets, scroll, g.height, g.width)
		}
		put(g.x+max(0, g.width-ansi.StringWidth(label)), g.y+g.height, themed(cellCrop(label, g.width), muted, bg, o.Dim))
	}

	if g.sidebar {
		targets = append(targets, drawSidebar(rows, o, false)...)
	}
	if !g.sidebar && o.SidebarOpen {
		menuWidth := min(sidebarWidth, o.Width)
		menuBounds := hitRegion{Y: g.contentY, W: menuWidth, H: max(0, dashboardContentBottom(o)-g.contentY)}
		// Underlying buttons must not receive a click through the navigation menu.
		kept := targets[:0]
		for _, r := range targets {
			if !regionsOverlap(r, menuBounds) {
				kept = append(kept, r)
			}
		}
		targets = append(kept, drawSidebar(rows, o, true)...)
	}
	if !o.HideKeyHints {
		footer, searchHint := dashboardKeyHints(o)
		put(2, o.Height-1, footer)
		if searchHint.ID != "" {
			searchHint.X += 2
			searchHint.Y = o.Height - 1
			add(searchHint)
		}
	}
	notifications := notificationLayout(o, headerTargets...)
	for _, notification := range notifications {
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

	return strings.Join(rows, "\n"), targets, total, notifications
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
	original := r
	right, bottom := min(r.X+r.W, x+w), min(r.Y+r.H, y+h)
	r.X, r.Y = max(r.X, x), max(r.Y, y)
	r.W, r.H = right-r.X, bottom-r.Y
	if r.X != original.X || r.Y != original.Y || r.W != original.W || r.H != original.H {
		r.InputWidth = 0
	}
	return r, r.W > 0 && r.H > 0
}
func regionsOverlap(a, b hitRegion) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

type dashboardBlock struct {
	lines   []string
	targets []hitRegion
	cards   []hitRegion
	footerY int
}

func (b *dashboardBlock) append(block dashboardBlock) {
	offset := len(b.lines)
	b.lines = append(b.lines, block.lines...)
	for _, r := range block.targets {
		r.Y += offset
		b.targets = append(b.targets, r)
	}
	for _, r := range block.cards {
		r.Y += offset
		b.cards = append(b.cards, r)
	}
}

// copyVisibleBlock places the intersection with the viewport into its line
// buffer, keeping hit rectangles in logical body coordinates for clipping.
func (b *dashboardBlock) copyVisibleBlock(block dashboardBlock, y, start int) {
	end := start + len(b.lines)
	first, last := max(start, y), min(end, y+len(block.lines))
	if first >= last {
		return
	}
	copy(b.lines[first-start:last-start], block.lines[first-y:last-y])
	for _, target := range block.targets {
		target.Y += y
		if target.Y < end && target.Y+target.H > start {
			b.targets = append(b.targets, target)
		}
	}
}

func (b *dashboardBlock) blank() { b.lines = append(b.lines, "") }

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
	body := overviewLayout(o, width)
	return strings.Join(body.lines, "\n"), body.targets
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
		if x > 0 && x+w > inner {
			lines = append(lines, cellPad(buttonLine, inner))
			buttonLine = ""
			x = 0
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
