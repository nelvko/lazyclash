package tui

import (
	"fmt"
	"runtime"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/downloadnet"
)

type wizardButton struct{ ID, Label string }
type wizardDocument struct {
	lines    []string
	targets  []hitRegion
	width    int
	hover    string
	disabled func(string) bool
}

func (d *wizardDocument) line(s string) {
	d.lines = append(d.lines, strings.Split(wrapText(s, d.width), "\n")...)
}
func (d *wizardDocument) space() { d.lines = append(d.lines, "") }
func (d *wizardDocument) label(s string) {
	d.line(themed(lipgloss.NewStyle().Bold(true).Render(s), text, raised, false))
}
func (d *wizardDocument) steps(current wizardStep) {
	var parts []string
	for i, step := range []wizardStep{stepCore, stepVersion, stepConfirm} {
		label := fmt.Sprintf("%d %s", i+1, step)
		color := muted
		if step == current {
			label = "[" + label + "]"
			color = accent
		}
		parts = append(parts, themed(label, color, raised, false))
	}
	d.line(strings.Join(parts, themed("  >  ", muted, raised, false)))
}
func (d *wizardDocument) control(id, label, focused string) {
	prefix := "  "
	color, background := text, raised
	if strings.HasPrefix(label, "(*)") || strings.HasPrefix(label, "[x]") {
		color, background = selectionForeground, selectionBackground
	}
	if id == focused {
		prefix = "› "
		color, background = focus, raised
	}
	y := len(d.lines)
	value := prefix + label
	if id == d.hover && id != focused {
		background = hoverBackground
		value = lipgloss.NewStyle().Underline(true).Render(value)
	}
	if d.disabled != nil && d.disabled(id) {
		color, background = disabled, raised
		value += " (busy)"
	}
	d.line(themed(value, color, background, false))
	d.targets = append(d.targets, hitRegion{ID: id, Y: y, W: d.width, H: len(d.lines) - y})
}
func (d *wizardDocument) field(id, label string, input *textinput.Model, focus string) {
	labelColor := muted
	if input.Err != nil {
		labelColor = errorColor
		label += " · Invalid value"
	}
	d.line(themed(label, labelColor, raised, false))
	styleInput(input, inputBackground, input.Err != nil)
	input.SetWidth(max(1, d.width-4))
	if id == focus {
		if !input.Focused() {
			input.Focus()
		}
	} else if input.Focused() {
		input.Blur()
	}
	value := input.View()
	if id == focus {
		value = "› " + value
	} else {
		value = "  " + value
	}
	y := len(d.lines)
	d.line(themed(cellPad(value, d.width), text, inputBackground, false))
	d.targets = append(d.targets, hitRegion{ID: id, Y: y, W: d.width, H: 1})
}
func checkColor(status string) string {
	switch status {
	case "Passed":
		return success
	case "Failed":
		return errorColor
	case "Testing":
		return info
	default:
		return muted
	}
}
func (d *wizardDocument) check(label string, state checkState) {
	color := checkColor(state.Status)
	d.line(themed(label+": ", muted, raised, false) + themed(state.Status, color, raised, false))
	if state.Detail != "" {
		d.line(themed(clean(state.Detail), color, raised, false))
	}
}
func (d *wizardDocument) pair(label, value string) {
	d.line(themed(label+": ", muted, raised, false) + themed(clean(value), text, raised, false))
}
func sourceName(cfg downloadnet.Config) string {
	switch cfg.Normalized().Source {
	case "prefix":
		return "URL prefix accelerator"
	case "mirror":
		return "Custom mirror template"
	default:
		return "Official / GitHub direct"
	}
}
func proxySummary(cfg downloadnet.Config) string {
	cfg = cfg.Normalized()
	switch cfg.ProxyMode {
	case "environment":
		return "Environment proxy (HTTP(S)_PROXY / ALL_PROXY / NO_PROXY)"
	case "custom":
		return "Custom proxy · " + downloadnet.RedactedURL(cfg.ProxyURL)
	default:
		return "No proxy (environment variables ignored)"
	}
}
func byteSize(n int64) string {
	if n < 0 {
		return "Unknown"
	}
	units := []string{"B", "KiB", "MiB", "GiB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
func (m *Model) wizardControlDisabled(id string) bool {
	if m.job == "save-network" {
		return id != "back"
	}
	if m.busy {
		switch id {
		case "test", "resolve", "retry", "continue", "install", "reinstall":
			return true
		}
	}
	return m.wizard.cancelling && id == "cancel"
}
func (m *Model) wizardDocument(width int) (wizardDocument, []wizardButton, string) {
	w := &m.wizard
	d := wizardDocument{width: max(1, width), hover: m.hover, disabled: m.wizardControlDisabled}
	title := "Install proxy core"
	if w.networkOpen {
		title = "Network settings"
		d.line("Download source and connection are independent.")
		d.space()
		d.label("DOWNLOAD SOURCE")
		d.control("source", sourceName(w.draft)+"  ‹ / ›", w.focus)
		switch w.draft.Source {
		case "prefix":
			d.space()
			d.field("prefix", "Prefix URL (input hidden)", &w.prefix, w.focus)
		case "mirror":
			d.space()
			d.field("mirror", "Template with {version} and {asset} (input hidden)", &w.mirror, w.focus)
		}
		d.space()
		d.label("Final file URL preview")
		if w.release.URL != "" {
			if value, err := w.draft.FilePreview(w.release.URL, w.release.Version, w.release.Asset); err == nil {
				d.line(value)
			} else {
				d.line(themed("Invalid source settings: "+err.Error(), errorColor, raised, false))
			}
		} else {
			d.line("Resolve a version to preview its actual asset URL.")
		}
		d.space()
		d.label("CONNECTION")
		d.control("proxy-mode", proxySummary(w.draft)+"  ‹ / ›", w.focus)
		if w.draft.ProxyMode == "custom" {
			d.field("proxy-url", "Proxy URL · http / https / socks5 / socks5h (hidden)", &w.proxy, w.focus)
		}
		d.space()
		d.label("CONNECTION TEST")
		d.check("Version query", w.versionTest)
		d.check("File download", w.fileTest)
		d.line("File test reads at most 4 KiB; installation still verifies the complete SHA256.")
		d.space()
		mark := "[ ]"
		if w.remember {
			mark = "[x]"
		}
		d.control("remember", mark+" Remember download preferences", w.focus)
		d.line("Credentials, query tokens and unrecognized private paths are session-only.")
		d.space()
		d.line("Version metadata and expected SHA256: official GitHub API through the connection proxy.")
		d.line("Prefix/mirror: release files only; no API rewriting. A successful file test does not prove that version queries work.")
		if w.err != "" {
			d.space()
			d.line(themed(clean(w.err), errorColor, raised, false))
		}
		apply := "Apply"
		if w.failedKind != "" && w.failedKind != "save-network" {
			apply = "Apply & retry"
		}
		return d, []wizardButton{{"back", "Back"}, {"test", "Test connection"}, {"apply-network", apply}}, title
	}
	switch w.step {
	case stepCore:
		d.label("Choose a proxy core")
		d.steps(w.step)
		d.space()
		d.control("core", "(*) Mihomo · Clash-compatible proxy core", w.focus)
		d.space()
		d.pair("Detected platform", runtime.GOOS+"/"+runtime.GOARCH)
		existing := m.snapshot.Version
		if existing == "" {
			existing = "Not installed"
		}
		d.pair("Installed version", existing)
		if m.snapshot.Service.Active {
			running := m.snapshot.RunningVersion
			if running == "" {
				running = "Running (version unavailable)"
			}
			d.pair("Running version", running)
		}
		d.space()
		d.control("network", "Network settings", w.focus)
		d.line(sourceName(w.cfg) + " / " + proxySummary(w.cfg))
		d.space()
		localLabel := "Use a local binary instead of downloading"
		if w.localMode {
			localLabel = "[x] Local binary selected"
		}
		d.control("local-mode", localLabel, w.focus)
		if w.localMode {
			d.field("local", "Local Mihomo executable", &w.local, w.focus)
		}
		d.space()
		d.line("Installation does not start the service, change system proxy, enable TUN or enable autostart.")
		return d, []wizardButton{{"cancel", "Skip for now"}, {"continue", "Continue"}}, title
	case stepVersion:
		d.label("Choose a version")
		d.steps(w.step)
		d.space()
		if w.localMode {
			d.pair("Local binary", w.local.Value())
			d.line("Its version is checked by executing -v with a timeout.")
		} else {
			mode := "(*) Latest stable · resolved from the official release API"
			if w.specific {
				mode = "(*) Specific stable version"
			}
			d.control("version-mode", mode, w.focus)
			if w.specific {
				d.field("version-input", "Version identifier", &w.version, w.focus)
			}
			d.control("resolve", "Resolve / Retry version query", w.focus)
		}
		d.space()
		if m.busy {
			d.line(m.spinner.View() + " " + clean(m.message))
		} else if w.release.Version != "" {
			d.pair("Resolved version", w.release.Version)
			d.pair("Asset", w.release.Asset)
			d.pair("Platform", w.release.Platform)
			d.pair("Checksum authority", w.release.ChecksumSource)
		} else {
			d.line("No version has been resolved yet.")
		}
		d.space()
		d.control("network", "Network settings", w.focus)
		d.line(sourceName(w.cfg) + " / " + proxySummary(w.cfg))
		if w.err != "" {
			d.space()
			label := "Version query failed: "
			if w.failedKind == "prepare" {
				label = "Preflight failed: "
			}
			d.line(themed(label+clean(w.err), errorColor, raised, false))
		}
		return d, []wizardButton{{"back", "Back"}, {"continue", "Continue"}}, title
	case stepConfirm:
		p := w.plan
		d.label("Review installation")
		d.steps(w.step)
		d.space()
		d.pair("Core", "Mihomo")
		d.pair("Version", p.Release.Version)
		platform := p.Release.Platform
		if platform == "" {
			platform = runtime.GOOS + "/" + runtime.GOARCH
		}
		d.pair("Platform", platform)
		d.pair("Asset", p.Release.Asset)
		d.pair("Install path", p.ActualPath)
		current := p.ExistingVersion
		if current == "" {
			current = "None"
		}
		d.pair("Existing version", current)
		if p.RunningVersion != "" {
			d.pair("Current running version", p.RunningVersion)
		}
		d.pair("Operation", p.Operation)
		if p.DiskKnown {
			d.pair("Free disk space", byteSize(int64(p.FreeBytes)))
		} else {
			d.pair("Free disk space", "Unknown; could not reliably determine")
		}
		d.pair("Permissions", "Current user; no privilege elevation")
		d.pair("Download source", sourceName(w.cfg))
		d.pair("Connection proxy", proxySummary(w.cfg))
		if p.Local != "" {
			d.pair("Binary source", "Local executable; no official digest is claimed")
		} else {
			d.pair("Expected SHA256", p.Release.SHA256)
			d.pair("Checksum source", p.Release.ChecksumSource)
		}
		for _, s := range p.Warnings {
			d.line(themed(clean(s), warning, raised, false))
		}
		if p.ExistingVersion != "" && p.ExistingVersion == p.Release.Version {
			mark := "[ ]"
			if w.reinstall {
				mark = "[x]"
			}
			d.control("reinstall", mark+" Reinstall selected version (otherwise use existing)", w.focus)
		}
		d.space()
		d.control("network", "Network settings", w.focus)
		d.line("This installs the core only. It will NOT start the service, modify system proxy, enable TUN or enable autostart.")
		if p.BlockedReason != "" {
			d.space()
			d.line(themed("Cannot install: "+clean(p.BlockedReason), errorColor, raised, false))
		}
		if w.err != "" {
			d.line(themed(clean(w.err), errorColor, raised, false))
		}
		install := "Install"
		if p.Operation == "use existing" {
			install = "Use existing"
		}
		buttons := []wizardButton{{"back", "Back"}}
		if p.BlockedReason == "" {
			buttons = append(buttons, wizardButton{"install", install})
		}
		return d, buttons, title
	case stepInstalling:
		label := stageLabel(w.progress.Stage)
		if w.cancelling {
			label = "Cancelling / Cleaning up"
		}
		d.label(label)
		d.space()
		d.pair("Version", w.plan.Release.Version)
		d.pair("Asset", w.plan.Release.Asset)
		if w.progress.Stage == "download" {
			summary := byteSize(w.progress.Downloaded)
			if w.progress.Total > 0 {
				summary += " / " + byteSize(w.progress.Total) + fmt.Sprintf("  %.1f%%", min(100, float64(w.progress.Downloaded)*100/float64(w.progress.Total)))
			} else {
				summary += " / total unknown (indeterminate)"
			}
			d.line(summary)
			d.pair("Speed", byteSize(int64(w.progress.BytesPerSecond))+"/s")
			if w.progress.Total > 0 {
				count := min(36, max(1, width-4))
				filled := int(min(float64(count), float64(w.progress.Downloaded)*float64(count)/float64(w.progress.Total)))
				d.line(themed(strings.Repeat("━", filled), info, raised, false) + themed(strings.Repeat("─", count-filled), border, raised, false))
			} else {
				d.line(m.spinner.View() + " Downloading with unknown total size")
			}
		}
		d.space()
		d.line(clean(w.progress.Detail))
		d.line("Cancel waits for requests and safe deployment cleanup; existing configuration is preserved.")
		d.control("details", map[bool]string{false: "Show details", true: "Hide details"}[w.details], w.focus)
		if w.details {
			d.space()
			for _, s := range w.logs {
				d.line(themed(clean(s), muted, raised, false))
			}
		}
		return d, []wizardButton{{"cancel", "Cancel"}}, title
	case stepResult:
		if w.err != "" {
			d.line(themed("Installation did not complete", errorColor, raised, false))
			d.pair("Failed stage", w.failedStage)
			d.line(themed(clean(w.err), errorColor, raised, false))
			d.space()
			d.line("Your choices are preserved. Retry after reviewing network settings or return without changing the existing core.")
			d.control("network", "Network settings", w.focus)
			d.control("details", map[bool]string{false: "View details", true: "Hide details"}[w.details], w.focus)
			if w.details {
				for _, s := range w.logs {
					d.line(themed(clean(s), muted, raised, false))
				}
			}
			return d, []wizardButton{{"done", "Back to overview"}, {"retry", "Retry"}}, title
		}
		d.line(themed("Mihomo installed and verified", success, raised, false))
		d.pair("Installed version", w.result.InstalledVersion)
		d.pair("Install path", w.result.ActualPath)
		d.pair("Operation", w.result.Operation)
		if w.result.RunningVersion != "" {
			d.pair("Current running version", w.result.RunningVersion)
			if w.result.RestartRequired {
				d.line("The running process was preserved. Restart explicitly to use the installed binary.")
			}
		} else if m.snapshot.Service.Active {
			d.line("The previous process is still running; its version is unavailable. Restart explicitly to use the installed binary.")
		} else {
			d.line("Service remains stopped. Installation did not start it.")
		}
		d.line("Executable version and configuration validation passed.")
		d.space()
		d.control("import", "Add subscription / Import config", w.focus)
		return d, []wizardButton{{"done", "Done / Overview"}}, title
	}
	return d, []wizardButton{{"cancel", "Back"}}, title
}

func (m *Model) renderWizard(base string) (string, []hitRegion) {
	w := &m.wizard
	// A bounded modal needs both its minimum width and minimum height. A wide,
	// short terminal uses the same single-column document without a background.
	modal := m.width >= 110 && m.height >= 30
	boxW, boxH := m.width, m.height
	x, y := 0, 0
	if modal {
		boxW = min(86, m.width-12)
		boxH = min(33, m.height-4)
		x = (m.width - boxW) / 2
		y = (m.height - boxH) / 2
	}
	if boxW < 34 || boxH < 10 {
		lines := []string{themed(cellCrop(logo+" · "+string(w.step), boxW), text, raised, false), cellCrop("Please enlarge terminal", boxW), cellCrop("Esc Back / Cancel · Ctrl+C Quit", boxW)}
		if w.cancelling {
			lines = append(lines, cellCrop("Cancelling / Cleaning up…", boxW))
		}
		if w.progress.Stage != "" {
			lines = append(lines, cellCrop(stageLabel(w.progress.Stage), boxW))
		}
		lines = lines[:min(len(lines), boxH)]
		return overlay("", strings.Join(lines, "\n"), 0, 0, m.width, m.height), nil
	}
	innerW := boxW - 6
	doc, buttons, title := m.wizardDocument(innerW)
	// Construct bottom actions independently, wrapping at narrow widths.
	var footer []string
	var buttonHits []hitRegion
	line := ""
	row := 0
	for _, b := range buttons {
		label := "  [ " + b.Label + " ]"
		foreground, background := text, raised
		switch b.ID {
		case "continue", "install", "apply-network", "retry":
			foreground, background = primaryButtonForeground, primaryButtonBackground
		case "done":
			if w.err == "" {
				foreground, background = primaryButtonForeground, primaryButtonBackground
			}
		}
		style := lipgloss.NewStyle()
		if m.hover == b.ID && w.focus != b.ID {
			if background != primaryButtonBackground {
				background = hoverBackground
			}
			style = style.Underline(true)
		}
		if w.focus == b.ID {
			label = "› [ " + b.Label + " ]"
			if background != primaryButtonBackground {
				foreground, background = focus, raised
			}
			style = style.Underline(true)
		}
		if m.wizardControlDisabled(b.ID) {
			label += " (busy)"
			foreground, background = disabled, raised
		}
		style = style.Foreground(lipgloss.Color(foreground)).Background(lipgloss.Color(background))
		bw := min(innerW, ansi.StringWidth(label))
		if ansi.StringWidth(line) > 0 && ansi.StringWidth(line)+2+bw > innerW {
			footer = append(footer, cellPad(line, innerW))
			line = ""
			row++
		}
		if line != "" {
			line += "  "
		}
		bx := ansi.StringWidth(line)
		line += style.Render(cellCrop(label, innerW))
		buttonHits = append(buttonHits, hitRegion{ID: b.ID, X: bx, Y: row, W: bw, H: 1})
	}
	footer = append(footer, cellPad(line, innerW))
	footerH := len(footer)
	bodyY := 5
	bodyH := max(1, boxH-bodyY-footerH-4)
	w.controls = nil
	for _, r := range doc.targets {
		w.controls = append(w.controls, r.ID)
	}
	for _, b := range buttons {
		w.controls = append(w.controls, b.ID)
	}
	if w.reveal {
		for _, r := range doc.targets {
			if r.ID == w.focus {
				if r.Y < w.scroll {
					w.scroll = r.Y
				}
				if r.Y+r.H > w.scroll+bodyH {
					w.scroll = r.Y + r.H - bodyH
				}
			}
		}
		w.reveal = false
	}
	w.scroll = max(0, min(w.scroll, max(0, len(doc.lines)-bodyH)))
	rows := make([]string, boxH)
	for i := range rows {
		rows[i] = themed(strings.Repeat(" ", boxW), text, raised, false)
	}
	putRows(rows, 0, 0, themed("┌"+strings.Repeat("─", boxW-2)+"┐", border, raised, false), boxW)
	for r := 1; r < boxH-1; r++ {
		putRows(rows, 0, r, themed("│", border, raised, false), boxW)
		putRows(rows, boxW-1, r, themed("│", border, raised, false), boxW)
	}
	putRows(rows, 0, boxH-1, themed("└"+strings.Repeat("─", boxW-2)+"┘", border, raised, false), boxW)
	putRows(rows, 3, 1, themed(lipgloss.NewStyle().Bold(true).Render(cellCrop(title, innerW)), text, raised, false), boxW)
	step := string(w.step)
	if w.networkOpen {
		step = "Source + Connection"
	}
	putRows(rows, 3, 2, themed(cellCrop("Mihomo · "+step+" · "+runtime.GOOS+"/"+runtime.GOARCH, innerW), muted, raised, false), boxW)
	putRows(rows, 2, 3, themed(strings.Repeat("─", boxW-4), border, raised, false), boxW)
	for i := 0; i < bodyH && w.scroll+i < len(doc.lines); i++ {
		putRows(rows, 3, bodyY+i, themed(cellPad(doc.lines[w.scroll+i], innerW), text, raised, false), boxW)
	}
	var hits []hitRegion
	for _, r := range doc.targets {
		r.X += 3
		r.Y += bodyY - w.scroll
		if clipped, ok := clipRegion(r, 3, bodyY, innerW, bodyH); ok {
			clipped.X += x
			clipped.Y += y
			hits = append(hits, clipped)
		}
	}
	footerY := boxH - footerH - 3
	putRows(rows, 2, footerY-1, themed(strings.Repeat("─", boxW-4), border, raised, false), boxW)
	for i, line := range footer {
		putRows(rows, 3, footerY+i, themed(line, text, raised, false), boxW)
	}
	for _, r := range buttonHits {
		r.X += x + 3
		r.Y += y + footerY
		hits = append(hits, r)
	}
	help := "Tab Focus · Enter Select · Esc Back · Ctrl+C Quit"
	if m.busy {
		help = "Esc Cancel · cleanup completes before leaving"
	}
	putRows(rows, 3, boxH-2, helpText(cellCrop(help, innerW), raised, false), boxW)
	if len(doc.lines) > bodyH {
		putRows(rows, boxW-3, bodyY+bodyH-1, themed("↕", info, raised, false), boxW)
	}
	box := strings.Join(rows, "\n")
	if !modal {
		base = ""
	}
	return overlay(base, box, x, y, m.width, m.height), hits
}
func (m *Model) renderImport(base string) (string, []hitRegion) {
	boxW := min(76, m.width)
	boxH := min(12, m.height)
	x, y := (m.width-boxW)/2, (m.height-boxH)/2
	if boxW < 24 || boxH < 7 {
		return overlay("", "Enlarge terminal · Esc Back", 0, 0, m.width, m.height), nil
	}
	m.input.SetWidth(max(1, boxW-8))
	styleInput(&m.input, inputBackground, m.failure != "")
	label := "Configuration name"
	if m.form == "source" {
		label = "Subscription URL or local YAML path (hidden)"
	}
	lines := []string{themed("Import configuration", text, raised, false), "", cellCrop(label, boxW-4), "", themed("› "+m.input.View(), text, inputBackground, false), "", themed(cellCrop("Enter Continue / Save · Esc Cancel", boxW-4), muted, raised, false)}
	if m.failure != "" {
		lines = append(lines, themed(cellCrop(m.failure, boxW-4), errorColor, raised, false))
	}
	box := lipgloss.NewStyle().Background(lipgloss.Color(raised)).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(focus)).Width(boxW-2).Height(boxH-2).Padding(0, 1).Render(strings.Join(lines, "\n"))
	return overlay(base, themed(box, text, raised, false), x, y, m.width, m.height), []hitRegion{{ID: "import-input", X: x + 2, Y: y + 5, W: max(1, boxW-4), H: 1}}
}
