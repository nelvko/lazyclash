package tui

import (
	"fmt"
	"runtime"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type wizardButton struct{ ID, Label string }
type wizardDocument struct {
	locale   i18n.Localizer
	lines    []string
	targets  []hitRegion
	width    int
	hover    string
	disabled func(string) bool
}

func (d *wizardDocument) tr(id string, args ...any) string { return d.locale.T(id, args...) }

func wizardStepLabel(locale i18n.Localizer, step wizardStep) string {
	switch step {
	case stepCore:
		return locale.T("wizard.step.core")
	case stepUpdate:
		return locale.T("core_update.title")
	case stepVersion:
		return locale.T("wizard.step.version")
	case stepConfirm:
		return locale.T("wizard.step.confirm")
	case stepInstalling:
		return locale.T("wizard.step.installing")
	case stepResult:
		return locale.T("wizard.step.result")
	default:
		return clean(string(step))
	}
}

func (d *wizardDocument) line(s string) {
	d.lines = append(d.lines, strings.Split(wrapText(s, d.width), "\n")...)
}
func (d *wizardDocument) space() { d.lines = append(d.lines, "") }
func (d *wizardDocument) label(s string) {
	d.line(themed(lipgloss.NewStyle().Bold(true).Render(s), text, raised, false))
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
		value += d.tr("wizard.busy_suffix")
	}
	d.line(themed(value, color, background, false))
	d.targets = append(d.targets, hitRegion{ID: id, Y: y, W: d.width, H: len(d.lines) - y})
}
func (d *wizardDocument) field(id, label string, input *textinput.Model, focus string) {
	labelColor := muted
	if input.Err != nil {
		labelColor = errorColor
		label += d.tr("wizard.invalid_suffix")
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
	case checkPassed:
		return success
	case checkFailed:
		return errorColor
	case checkTesting:
		return info
	default:
		return muted
	}
}
func (d *wizardDocument) check(label string, state checkState) {
	color := checkColor(state.Status)
	status := state.Status
	switch state.Status {
	case checkNotTested:
		status = d.tr("wizard.check.not_tested")
	case checkTesting:
		status = d.tr("wizard.check.testing")
	case checkNotApplicable:
		status = d.tr("wizard.check.not_applicable")
	case checkPassed:
		status = d.tr("wizard.check.passed")
	case checkFailed:
		status = d.tr("wizard.check.failed")
	}
	d.line(themed(label+": ", muted, raised, false) + themed(status, color, raised, false))
	if state.Detail != "" {
		d.line(themed(clean(state.Detail), color, raised, false))
	}
}
func (d *wizardDocument) pair(label, value string) {
	d.line(themed(label+": ", muted, raised, false) + themed(clean(value), text, raised, false))
}
func (m *Model) wizardSourceName(cfg downloadnet.Config) string {
	return m.tr("wizard.method." + cfg.Normalized().Method)
}
func (m *Model) wizardProxySummary(cfg downloadnet.Config) string {
	if cfg.Normalized().Method == "environment" {
		status, err := downloadnet.EnvironmentStatus()
		if err != nil {
			return m.tr("wizard.environment.invalid")
		}
		return m.tr("wizard.environment." + status)
	}
	return m.tr("wizard.environment.ignored")
}
func (m *Model) wizardByteSize(n int64) string {
	if n < 0 {
		return m.tr("wizard.unknown")
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

func (m *Model) wizardOperation(operation string) string {
	switch operation {
	case "install", "update", "reinstall":
		return m.tr("wizard.operation." + operation)
	case "use existing":
		return m.tr("wizard.operation.use_existing")
	default:
		return operation
	}
}

func (m *Model) stageLabel(stage string) string {
	switch stage {
	case "query", "download", "checksum", "extract", "verify", "deploy", "restart", "rollback", "done":
		return m.tr("wizard.stage." + stage)
	default:
		return clean(stage)
	}
}

func (m *Model) wizardFailedStage(stage string) string {
	// Legacy test fixtures and already-built documents can still contain an
	// English label. New operation results retain the stable stage identifier.
	for _, id := range []string{"query", "download", "checksum", "extract", "verify", "deploy", "restart", "rollback", "done"} {
		if stage == id || stage == stageLabel(id) {
			return m.stageLabel(id)
		}
	}
	return stage
}

func (m *Model) wizardControlDisabled(id string) bool {
	if m.job == "save-network" {
		return id != "back"
	}
	if m.busy {
		switch id {
		case "test", "resolve", "retry", "continue", "install", "reinstall", "check-update", "upgrade":
			return true
		}
	}
	return m.wizard.cancelling && id == "cancel"
}
func (m *Model) wizardDocument(width int) (wizardDocument, []wizardButton, string) {
	w := &m.wizard
	d := wizardDocument{locale: m.locale, width: max(1, width), hover: m.hover, disabled: m.wizardControlDisabled}
	title := m.tr("wizard.title")
	if w.upgrading {
		title = m.tr("core_update.title")
	}
	if w.networkOpen {
		title = m.tr("wizard.network.title")
		choices := make([]string, 0, 3)
		for _, method := range []string{"direct", "environment", "accelerated"} {
			mark := "( ) "
			if method == w.draft.Normalized().Method {
				mark = "(*) "
			}
			choices = append(choices, mark+m.tr("wizard.method."+method))
		}
		separator := "  "
		if d.width < 66 {
			separator = "\n"
		}
		d.control("download-method", strings.Join(choices, separator), w.focus)
		if w.draft.Normalized().Method == "accelerated" {
			d.space()
			d.field("prefix", m.tr("wizard.network.prefix"), &w.prefix, w.focus)
			d.line(m.tr("wizard.network.prefix_help"))
			d.control("url-preview", m.tr("wizard.network.preview_toggle"), w.focus)
			if w.previewURL {
				original := "https://github.com/MetaCubeX/mihomo/releases/latest/download/version.txt"
				if w.release.URL != "" {
					original = w.release.URL
				}
				if value, err := w.draft.Preview(original); err == nil {
					d.line(value)
				} else {
					d.line(themed(m.localizeError(err), errorColor, raised, false))
				}
			}
		}
		d.space()
		d.line(m.wizardProxySummary(w.draft))
		d.space()
		d.label(m.tr("wizard.network.test"))
		d.check(m.tr("wizard.network.version_query"), w.versionTest)
		d.check(m.tr("wizard.network.file_download"), w.fileTest)
		d.line(m.tr("wizard.network.test_limit"))
		d.space()
		mark := "[ ]"
		if w.remember {
			mark = "[x]"
		}
		d.control("remember", mark+m.tr("wizard.network.remember"), w.focus)
		d.space()
		d.line(m.tr("wizard.network.scope"))
		if w.err != "" {
			d.space()
			d.line(themed(clean(w.err), errorColor, raised, false))
		}
		apply := m.tr("wizard.apply")
		if w.failedKind != "" && w.failedKind != "save-network" {
			apply = m.tr("wizard.apply_retry")
		}
		return d, []wizardButton{{"back", m.tr("wizard.back")}, {"test", m.tr("wizard.test_connection")}, {"apply-network", apply}}, title
	}
	switch w.step {
	case stepUpdate:
		return m.coreUpdateDocument(d)
	case stepCore:
		title = m.tr("wizard.manage.title")
		existing := m.snapshot.Version
		if existing == "" {
			existing = m.tr("wizard.not_installed")
		}
		d.pair(m.tr("wizard.installed_version"), existing)
		if m.snapshot.Service.Active {
			running := m.snapshot.RunningVersion
			if running == "" {
				running = m.tr("wizard.running_unknown")
			}
			d.pair(m.tr("wizard.running_version"), running)
		}
		d.pair(m.tr("wizard.platform_detected"), runtime.GOOS+"/"+mihomo.RecommendedVariant())
		d.space()
		d.line(m.tr("core_update.read_only"))
		d.space()
		d.control("network", m.tr("wizard.network.title"), w.focus)
		d.control("manual-install", m.tr("wizard.manual_install"), w.focus)
		return d, []wizardButton{{"cancel", m.tr("wizard.back")}, {"check-update", m.tr("core_update.check")}}, title
	case stepVersion:
		existing := m.snapshot.Version
		if existing == "" {
			existing = m.tr("wizard.not_installed")
		}
		d.pair(m.tr("wizard.installed_version"), existing)
		d.pair(m.tr("wizard.platform_detected"), runtime.GOOS+"/"+mihomo.RecommendedVariant())
		if m.height >= 20 {
			d.space()
		}
		if w.localMode {
			d.field("local", m.tr("wizard.local.executable"), &w.local, w.focus)
			d.line(m.tr("wizard.local.version_check"))
			if m.busy {
				d.line(m.spinner.View() + " " + clean(m.message))
			}
		} else {
			mode := m.tr("wizard.version.latest")
			if w.specific {
				mode = m.tr("wizard.version.specific")
			}
			d.control("version-mode", mode, w.focus)
			if w.specific {
				d.field("version-input", m.tr("wizard.version.identifier"), &w.version, w.focus)
			}
			if m.busy {
				d.line(m.spinner.View() + " " + clean(m.message))
			} else {
				version := w.updateCheck.LatestVersion
				if w.release.Version != "" {
					version = w.release.Version
				}
				if w.specific {
					version = w.release.Version
				}
				if version != "" {
					d.pair(m.tr("wizard.version.recommended"), version)
				} else if w.err == "" {
					d.line(m.tr("wizard.version.pending"))
				}
			}
			d.control("resolve", m.tr("wizard.version.check_again"), w.focus)
			d.line(m.tr("wizard.device_auto"))
		}
		if w.err != "" {
			d.space()
			d.line(themed(clean(w.err), errorColor, raised, false))
		}
		d.space()
		localLabel := m.tr("wizard.local.choose")
		if w.localMode {
			localLabel = m.tr("wizard.local.selected")
		}
		d.control("local-mode", localLabel, w.focus)
		if m.snapshot.Installed {
			mark := "[ ]"
			if w.reinstall {
				mark = "[x]"
			}
			d.control("reinstall", mark+m.tr("wizard.reinstall"), w.focus)
		}
		d.control("network", m.tr("wizard.network.title"), w.focus)
		if m.height >= 24 {
			d.line(m.tr("wizard.network.summary", m.wizardSourceName(w.cfg)))
		}
		if w.release.Asset != "" {
			d.control("details", map[bool]string{false: m.tr("wizard.details_show"), true: m.tr("wizard.details_hide")}[w.details], w.focus)
			if w.details {
				d.pair(m.tr("wizard.asset"), w.release.Asset)
				d.pair(m.tr("wizard.expected_sha256"), w.release.SHA256)
				d.pair(m.tr("wizard.checksum_authority"), m.localizeDetail(w.release.ChecksumSource))
			}
		}
		d.space()
		if m.snapshot.Installed {
			d.line(m.tr("wizard.replace_summary"))
		} else {
			d.line(m.tr("wizard.install_summary"))
		}
		install := m.tr("wizard.download_install")
		if w.localMode {
			install = m.tr("wizard.install")
		}
		return d, []wizardButton{{"back", m.tr("wizard.back")}, {"install", install}}, title
	case stepConfirm:
		p := w.plan
		d.label(m.tr("wizard.review"))
		d.space()
		d.pair(m.tr("wizard.step.core"), "Mihomo")
		d.pair(m.tr("wizard.step.version"), p.Release.Version)
		platform := p.Release.Platform
		if platform == "" {
			platform = runtime.GOOS + "/" + runtime.GOARCH
		}
		d.pair(m.tr("wizard.platform"), platform)
		d.pair(m.tr("wizard.asset"), p.Release.Asset)
		d.pair(m.tr("wizard.install_path"), p.ActualPath)
		current := p.ExistingVersion
		if current == "" {
			current = m.tr("wizard.none")
		}
		d.pair(m.tr("wizard.existing_version"), current)
		if p.RunningVersion != "" {
			d.pair(m.tr("wizard.running_current"), p.RunningVersion)
		}
		d.pair(m.tr("wizard.operation"), m.wizardOperation(p.Operation))
		if p.DiskKnown {
			d.pair(m.tr("wizard.disk_space"), m.wizardByteSize(int64(p.FreeBytes)))
		} else {
			d.pair(m.tr("wizard.disk_space"), m.tr("wizard.disk_unknown"))
		}
		d.pair(m.tr("wizard.permissions"), m.tr("wizard.permissions_current"))
		d.pair(m.tr("wizard.download_source"), m.wizardSourceName(w.cfg))
		if p.Local != "" {
			d.pair(m.tr("wizard.binary_source"), m.tr("wizard.local.no_digest"))
		} else {
			d.pair(m.tr("wizard.expected_sha256"), p.Release.SHA256)
			d.pair(m.tr("wizard.checksum_source"), m.localizeDetail(p.Release.ChecksumSource))
		}
		for _, s := range p.Warnings {
			d.line(themed(clean(m.localizeDetail(s)), warning, raised, false))
		}
		if p.ExistingVersion != "" && p.ExistingVersion == p.Release.Version {
			mark := "[ ]"
			if w.reinstall {
				mark = "[x]"
			}
			d.control("reinstall", mark+m.tr("wizard.reinstall"), w.focus)
		}
		d.space()
		d.control("network", m.tr("wizard.network.title"), w.focus)
		d.line(m.tr("wizard.confirm_boundary"))
		if p.BlockedReason != "" {
			d.space()
			d.line(themed(m.tr("wizard.cannot_install", clean(m.locale.Diagnostic(p.BlockedReason, p.BlockedDiagnostic))), errorColor, raised, false))
		}
		if w.err != "" {
			d.line(themed(clean(w.err), errorColor, raised, false))
		}
		install := m.tr("wizard.install")
		if p.Operation == "use existing" {
			install = m.tr("wizard.use_existing")
		}
		buttons := []wizardButton{{"back", m.tr("wizard.back")}}
		if p.BlockedReason == "" {
			buttons = append(buttons, wizardButton{"install", install})
		}
		return d, buttons, title
	case stepInstalling:
		label := m.stageLabel(w.progress.Stage)
		if w.cancelling {
			label = m.tr("wizard.cancelling")
		}
		d.label(label)
		d.space()
		if w.upgrading {
			d.pair(m.tr("wizard.step.version"), w.updateCheck.LatestVersion)
		} else {
			d.pair(m.tr("wizard.step.version"), w.plan.Release.Version)
			d.pair(m.tr("wizard.asset"), w.plan.Release.Asset)
		}
		if w.progress.Stage == "download" {
			summary := m.wizardByteSize(w.progress.Downloaded)
			if w.progress.Total > 0 {
				summary += " / " + m.wizardByteSize(w.progress.Total) + fmt.Sprintf("  %.1f%%", min(100, float64(w.progress.Downloaded)*100/float64(w.progress.Total)))
			} else {
				summary += m.tr("wizard.total_unknown")
			}
			d.line(summary)
			d.pair(m.tr("wizard.speed"), m.wizardByteSize(int64(w.progress.BytesPerSecond))+"/s")
			if w.progress.Total > 0 {
				count := min(36, max(1, width-4))
				filled := int(min(float64(count), float64(w.progress.Downloaded)*float64(count)/float64(w.progress.Total)))
				d.line(themed(strings.Repeat("━", filled), info, raised, false) + themed(strings.Repeat("─", count-filled), border, raised, false))
			} else {
				d.line(m.spinner.View() + m.tr("wizard.download_unknown"))
			}
		}
		d.space()
		d.line(clean(m.localizeDetail(w.progress.Detail)))
		d.line(m.tr("wizard.cancel_boundary"))
		d.control("details", map[bool]string{false: m.tr("wizard.details_show"), true: m.tr("wizard.details_hide")}[w.details], w.focus)
		if w.details {
			d.space()
			for _, s := range w.logs {
				d.line(themed(clean(s), muted, raised, false))
			}
		}
		return d, []wizardButton{{"cancel", m.tr("wizard.cancel")}}, title
	case stepResult:
		if w.upgrading {
			return m.coreUpgradeResultDocument(d)
		}
		if w.err != "" {
			d.line(themed(m.tr("wizard.incomplete"), errorColor, raised, false))
			d.pair(m.tr("wizard.failed_stage"), m.wizardFailedStage(w.failedStage))
			d.line(themed(clean(w.err), errorColor, raised, false))
			d.space()
			d.line(m.tr("wizard.retry_boundary"))
			d.control("network", m.tr("wizard.network.title"), w.focus)
			d.control("details", map[bool]string{false: m.tr("wizard.details_view"), true: m.tr("wizard.details_hide")}[w.details], w.focus)
			if w.details {
				for _, s := range w.logs {
					d.line(themed(clean(s), muted, raised, false))
				}
			}
			return d, []wizardButton{{"done", m.tr("wizard.overview")}, {"retry", m.tr("wizard.retry")}}, title
		}
		d.line(themed(m.tr("wizard.installed"), success, raised, false))
		d.pair(m.tr("wizard.installed_version"), w.result.InstalledVersion)
		d.pair(m.tr("wizard.install_path"), w.result.ActualPath)
		d.pair(m.tr("wizard.operation"), m.wizardOperation(w.result.Operation))
		if w.result.RunningVersion != "" {
			d.pair(m.tr("wizard.running_current"), w.result.RunningVersion)
			if w.result.RestartRequired {
				d.line(m.tr("wizard.restart_required"))
			}
		} else if m.snapshot.Service.Active {
			d.line(m.tr("wizard.running_preserved"))
		} else {
			d.line(m.tr("wizard.stopped"))
		}
		d.line(m.tr("wizard.validated"))
		if m.snapshot.ConfigReady && (m.snapshot.Applied == "default" || m.snapshot.Applied == "") {
			d.line(m.tr("dashboard.profile.builtinReady"))
		}
		d.space()
		d.control("import", m.tr("wizard.import"), w.focus)
		return d, []wizardButton{{"done", m.tr("wizard.done")}}, title
	}
	return d, []wizardButton{{"cancel", m.tr("wizard.back")}}, title
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
		lines := []string{themed(cellCrop(logo+" · "+wizardStepLabel(m.locale, w.step), boxW), text, raised, false), cellCrop(m.tr("wizard.enlarge"), boxW), cellCrop(m.tr("wizard.compact_help"), boxW)}
		if w.cancelling {
			lines = append(lines, cellCrop(m.tr("wizard.cancelling_progress"), boxW))
		}
		if w.progress.Stage != "" {
			lines = append(lines, cellCrop(m.stageLabel(w.progress.Stage), boxW))
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
		case "continue", "install", "upgrade", "apply-network", "retry", "check-update":
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
			label += m.tr("wizard.busy_suffix")
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
	if modal {
		boxH = min(boxH, len(doc.lines)+footerH+9)
		y = (m.height - boxH) / 2
	}
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
	step := wizardStepLabel(m.locale, w.step)
	if w.networkOpen {
		step = m.tr("wizard.network.subtitle")
	}
	putRows(rows, 3, 2, themed(cellCrop(step, innerW), muted, raised, false), boxW)
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
	help := m.tr("wizard.help")
	if m.busy {
		help = m.tr("wizard.busy_help")
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
