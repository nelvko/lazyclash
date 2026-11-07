package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/nelvko/lazyclash/internal/i18n"
)

// Keep card identities independent of their position and translated contents.
// The two columns flow independently; short cards never acquire filler rows.
func overviewLayout(o dashboardOptions, width int) dashboardBlock {
	if overviewSetupVisible(o) {
		return overviewNamedCard("dashboard.guide.title", getStarted(o, width), width)
	}
	core := func(w int) dashboardBlock {
		return overviewNamedCard("dashboard.control.core", overviewCoreCard(o, w), w)
	}
	route := func(w int) dashboardBlock {
		return overviewNamedCard("dashboard.control.routingMode", overviewRouteCard(o, w), w)
	}
	traffic := func(w int) dashboardBlock {
		return overviewNamedCard("dashboard.traffic.title", overviewTrafficCard(o, w), w)
	}
	profile := func(w int) dashboardBlock {
		return overviewNamedCard("dashboard.profile.title", overviewProfileCard(o, w), w)
	}
	var body dashboardBlock
	if width >= 90 && o.Height >= 20 {
		leftWidth := (width - 2) * 3 / 5
		rightWidth := width - 2 - leftWidth
		left, right := core(leftWidth), traffic(rightWidth)
		left.blank()
		left.append(route(leftWidth))
		right.blank()
		right.append(profile(rightWidth))
		body = overviewColumns(o, left, right, leftWidth, rightWidth)
	} else {
		for _, render := range []func(int) dashboardBlock{core, route, traffic, profile} {
			if len(body.lines) > 0 {
				body.blank()
			}
			body.append(render(width))
		}
	}
	body.blank()
	body.append(overviewNamedCard("dashboard.system.title", overviewSystemCard(o, width), width))
	return body
}

func overviewNamedCard(id string, block dashboardBlock, width int) dashboardBlock {
	if width >= 4 {
		block.cards = []hitRegion{{ID: "overview.card." + id, W: width, H: len(block.lines)}}
	}
	return block
}

// Reserve the same perimeter as a framed panel so focus can draw an outline
// without moving the content, actions or adjacent cards.
func overviewSurface(lines []string, width, height int, o dashboardOptions) []string {
	if width < 4 {
		return panelFrame(lines, width, height, o)
	}
	height = max(height, len(lines)+2)
	result := make([]string, height)
	result[0] = themed(strings.Repeat(" ", width), text, panel, o.Dim)
	for y := 1; y < height-1; y++ {
		content := ""
		if y-1 < len(lines) {
			content = lines[y-1]
		}
		result[y] = themed("  ", text, panel, o.Dim) + themed(cellPad(content, width-4), text, panel, o.Dim) + themed("  ", text, panel, o.Dim)
	}
	result[height-1] = result[0]
	return result
}

func overviewColumns(o dashboardOptions, left, right dashboardBlock, leftWidth, rightWidth int) dashboardBlock {
	var b dashboardBlock
	for y := 0; y < max(len(left.lines), len(right.lines)); y++ {
		l, r := "", ""
		if y < len(left.lines) {
			l = left.lines[y]
		}
		if y < len(right.lines) {
			r = right.lines[y]
		}
		b.lines = append(b.lines, cellPad(l, leftWidth)+themed("  ", text, bg, o.Dim)+cellPad(r, rightWidth))
	}
	b.targets = append(b.targets, left.targets...)
	b.cards = append(b.cards, left.cards...)
	for _, target := range right.targets {
		target.X += leftWidth + 2
		b.targets = append(b.targets, target)
	}
	for _, card := range right.cards {
		card.X += leftWidth + 2
		b.cards = append(b.cards, card)
	}
	return b
}

func overviewProfileCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	name, status := tr("dashboard.profile.none"), tr("dashboard.profile.notApplied")
	if s.ConfigReady {
		name, status = snapshotProfileName(s, s.Applied, o.Locale), tr("dashboard.profile.ready")
		if name == "" {
			name = tr("profiles.builtin_name")
		}
	}
	saved := 0
	for _, name := range s.Profiles {
		if name != "default" {
			saved++
		}
	}
	lines := []string{
		themed(lipgloss.NewStyle().Bold(true).Render(clean(name)), text, panel, o.Dim),
		themed(status, muted, panel, o.Dim),
		themed(tr("dashboard.overview.savedProfiles", saved), muted, panel, o.Dim),
	}
	if !s.ConfigReady && saved == 0 {
		lines = append(lines, themed(tr("dashboard.profile.importHint"), muted, panel, o.Dim))
	}
	return overviewControlPanel(o, width, tr("dashboard.profile.title"), lines, []overviewControlButton{
		{id: "card.profiles", label: tr("dashboard.action.profiles"), enabled: !o.Busy},
	})
}

func overviewRouteCard(o dashboardOptions, width int) dashboardBlock {
	b := overviewModeCard(o, width)
	var buttons []overviewControlButton
	if o.Snapshot.Service.Active && o.Snapshot.ServiceError == "" && o.Snapshot.APIError == "" && len(o.Snapshot.Groups) > 0 {
		buttons = append(buttons, overviewControlButton{id: "card.proxies", label: o.Locale.T("dashboard.action.openProxies"), enabled: !o.Busy})
	}
	group := overviewControlPanel(o, width, o.Locale.T("dashboard.overview.currentProxy"), overviewGroupLines(o), buttons)
	if width < 4 {
		b.append(group)
		return b
	}
	offset := len(b.lines) - 1
	separator := overviewSurface([]string{themed(strings.Repeat("─", max(0, width-4)), divider, panel, o.Dim)}, width, 3, o)[1]
	b.lines = append(b.lines[:offset], separator)
	b.lines = append(b.lines, group.lines[1:]...)
	for _, hit := range group.targets {
		hit.Y += offset
		b.targets = append(b.targets, hit)
	}
	return b
}

type overviewField struct{ label, value string }

// Use aligned cells with natural wrapping, including long translated labels.
func overviewFieldGrid(o dashboardOptions, fields []overviewField, width, columns int) []string {
	columns = min(columns, len(fields))
	cellWidth := max(1, (width-2*(columns-1))/max(1, columns))
	var lines []string
	for i := 0; i < len(fields); i += columns {
		cells := make([][]string, min(columns, len(fields)-i))
		height := 0
		for j := range cells {
			f := fields[i+j]
			label := themed(f.label, muted, panel, o.Dim)
			value := themed(lipgloss.NewStyle().Bold(true).Render(clean(f.value)), text, panel, o.Dim)
			cells[j] = append(strings.Split(wrapText(label, cellWidth), "\n"), strings.Split(wrapText(value, cellWidth), "\n")...)
			height = max(height, len(cells[j]))
		}
		for y := 0; y < height; y++ {
			var row []string
			for _, cell := range cells {
				value := ""
				if y < len(cell) {
					value = cell[y]
				}
				row = append(row, cellPad(value, cellWidth))
			}
			lines = append(lines, strings.Join(row, themed("  ", text, panel, o.Dim)))
		}
	}
	return lines
}

func overviewTrafficCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Runtime
	status := runtimeStatus(s, o.RuntimeReceived, o.RuntimePending, o.Snapshot.Installed, o.Locale)
	var lines []string
	if !o.RuntimeReceived || s.State != "ready" || s.Traffic == nil || s.Connections == nil || s.Memory == nil {
		lines = append(lines, themed(status, muted, panel, o.Dim))
	}
	if o.RuntimeReceived && s.State == "ready" {
		up, down, connections, memory := "--", "--", "--", "--"
		if s.Traffic != nil {
			up, down = formatBytes(s.Traffic.Up)+"/s", formatBytes(s.Traffic.Down)+"/s"
		}
		if s.Connections != nil {
			connections = fmt.Sprint(len(s.Connections.Connections))
		}
		if s.Memory != nil {
			memory = formatBytes(int64(*s.Memory))
		}
		columns := 1
		if width >= 34 {
			columns = 2
		}
		fields := []overviewField{{tr("dashboard.overview.upload"), up}, {tr("dashboard.overview.download"), down}, {tr("dashboard.field.connections"), connections}, {tr("dashboard.field.coreMemory"), memory}}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, overviewFieldGrid(o, fields, max(1, width-4), columns)...)
		lines = append(lines, "", themed(tr("dashboard.field.sinceStart"), muted, panel, o.Dim))
		total := "--"
		if s.Connections != nil {
			total = "↑ " + formatBytes(s.Connections.UploadTotal) + "  ↓ " + formatBytes(s.Connections.DownloadTotal)
		}
		lines = append(lines, themed(total, text, panel, o.Dim), themed(tr("dashboard.traffic.resetHint"), muted, panel, o.Dim))
	} else {
		lines = append(lines, themed(tr("dashboard.traffic.startHint"), muted, panel, o.Dim))
	}
	for _, issue := range []struct {
		text  string
		cause error
	}{{s.Error, s.Diagnostic}, {s.ConnectionsError, s.ConnectionsDiagnostic}, {s.TrafficError, s.TrafficDiagnostic}, {s.MemoryError, s.MemoryDiagnostic}} {
		if issue.text != "" {
			lines = append(lines, themed(clean(o.Locale.Diagnostic(issue.text, issue.cause)), errorColor, panel, o.Dim))
			break
		}
	}
	return overviewControlPanel(o, width, tr("dashboard.overview.traffic"), lines, nil)
}

func overviewVersion(s, fallback string, locale i18n.Localizer) string {
	if s = clean(s); s != "" {
		return s
	}
	return locale.T(fallback)
}
