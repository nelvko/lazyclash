package tui

import (
	"fmt"
	"runtime"
	"strings"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// First installation uses the shared installation state machine, while the
// overview owns its layout, scrolling and navigation. Installed-core management
// keeps the modal because replacement and upgrades need an impact review.
func (m Model) inlineInstallVisible() bool {
	return m.wizard.inline && !m.guideSkipped && m.snapshotReady &&
		(m.wizard.step == stepInstalling || m.wizard.step == stepResult || !m.snapshot.Installed && !m.snapshot.GuideDismissed)
}

func (m *Model) syncInlineInstall() tea.Cmd {
	if m.wizard.inline || m.busy || m.quitting || m.page != "overview" || m.feedbackDialogOpen() || m.helpOpen || !overviewSetupVisible(m.navigationOptions()) {
		return nil
	}
	return m.beginInlineInstall(false, false)
}

func (m *Model) beginInlineInstall(local, takeFocus bool) tea.Cmd {
	if m.job == "install" || m.quitting {
		return nil
	}
	if m.busy {
		if !m.wizard.inline {
			return nil
		}
		m.abortLookup()
	}
	if !m.wizard.inline || m.wizard.step == stepResult {
		cfg, expanded := m.wizard.cfg, m.wizard.expanded
		m.wizard = newWizard()
		m.wizard.cfg, m.wizard.draft, m.wizard.expanded = cfg, cfg, expanded
		m.wizard.inline = true
	}
	m.guideSkipped = false
	m.wizard.open, m.wizard.networkOpen = false, false
	m.wizard.step, m.wizard.localMode = stepVersion, local
	m.wizard.focus, m.wizard.installRequested = "install", false
	if takeFocus {
		m.browsePage("overview")
		m.focus, m.scroll = "setup.install", 0
	} else if m.page == "overview" && m.focus == "install" {
		m.focus = "setup.install"
	}
	if local {
		m.wizard.more = true
		m.focus, m.wizard.focus = "setup.local", "local"
		m.wizard.local.Focus()
		m.revealContentFocus()
		return textinput.Blink
	}
	if m.wizard.updateCheck.LatestVersion != "" || m.wizard.specific {
		return nil
	}
	return m.checkInstallVersion()
}

func (m *Model) openWizard(local bool) tea.Cmd {
	if m.snapshotReady && !m.snapshot.Installed {
		return m.beginInlineInstall(local, true)
	}
	if m.busy || m.quitting {
		return nil
	}
	m.wizard.inline, m.wizard.more = false, false
	return m.openManagementWizard(local)
}

func (m *Model) handleInlineWizardResult(v wizardMsg) tea.Cmd {
	previous := m.focus
	cmd := m.handleWizardResult(v)
	if !m.wizard.inline {
		return cmd
	}
	// Results may arrive after a page switch or a Tab. Only replace the control
	// that owned this operation; a background result never moves page focus.
	if m.page == "overview" && strings.HasPrefix(previous, "setup.") && !m.wizard.networkOpen {
		switch v.kind {
		case "install", "upgrade":
			m.focus = "setup.start"
			if m.wizard.err != "" {
				m.focus = "setup.retry"
			}
		case "prepare":
			if m.job == "install" {
				m.focus = "setup.cancel"
			}
		}
		if m.wizard.err != "" && (previous == "setup.install" || previous == "setup.resolve") && (v.kind == "install-check" || v.kind == "resolve" || v.kind == "prepare") {
			m.focus = "setup.resolve"
			if v.kind == "prepare" && m.wizard.localMode {
				m.wizard.more = true
				m.focus = "setup.local"
				m.wizard.local.Focus()
			}
		}
		if m.focus != previous {
			m.revealContentFocus()
		}
	}
	if m.wizard.err != "" {
		m.failure = m.wizard.err
	} else {
		m.failure = ""
	}
	return cmd
}

func (m Model) inlineInputActive() bool {
	return m.page == "overview" && m.inlineInstallVisible() && !m.sidebarOpen &&
		!m.wizard.open && !m.wizard.networkOpen &&
		!m.inlineInstallCompact(dashboardBodyWidth(m.dashboardOptions())) &&
		(m.focus == "setup.local" || m.focus == "setup.version-input")
}

func (m Model) inlineInstallCompact(width int) bool {
	return width < 34 || m.width < 38 || dashboardGeometry(m.dashboardOptions()).height < 6
}

func (m *Model) updateInlineInput(msg tea.Msg) tea.Cmd {
	if !m.inlineInputActive() {
		return nil
	}
	m.wizard.focus = strings.TrimPrefix(m.focus, "setup.")
	return m.updateWizardInput(msg)
}

func (m *Model) blurInlineInput() {
	m.wizard.local.Blur()
	m.wizard.version.Blur()
}

func (m Model) inlineCanInstall() bool {
	if m.busy || m.quitting || m.wizard.err != "" || m.inlineInstallCompact(dashboardBodyWidth(m.dashboardOptions())) {
		return false
	}
	w := m.wizard
	if w.step == stepConfirm {
		return w.plan.BlockedReason == ""
	}
	if w.step != stepVersion {
		return false
	}
	if w.localMode {
		return strings.TrimSpace(w.local.Value()) != ""
	}
	if w.specific {
		return strings.TrimSpace(w.version.Value()) != ""
	}
	return w.updateCheck.LatestVersion != ""
}

func (m Model) inlineInstallHint() string {
	if m.inlineInputActive() {
		return m.tr("dashboard.setup.inputHint")
	}
	if m.wizard.step == stepInstalling {
		return m.tr("dashboard.setup.progressHint")
	}
	if m.wizard.step == stepResult {
		return m.tr("dashboard.setup.resultHint")
	}
	return m.tr("dashboard.setup.hint")
}

func (m *Model) inlineInstallKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.page != "overview" || !m.inlineInstallVisible() || m.sidebarOpen || !strings.HasPrefix(m.focus, "setup.") {
		return false, nil
	}
	key := k.String()
	if m.inlineInstallCompact(dashboardBodyWidth(m.dashboardOptions())) {
		if key == "esc" {
			if m.job == "install" {
				m.cancelTask(false)
			} else {
				m.returnToNavigation()
			}
			return true, nil
		}
		switch key {
		case "q", "?", "tab", "shift+tab", "1", "2", "3", "4", "5", "6", "7":
			return false, nil
		default:
			return true, nil
		}
	}
	if m.inlineInputActive() {
		switch key {
		case "tab", "shift+tab":
			delta := 1
			if key == "shift+tab" {
				delta = -1
			}
			m.blurInlineInput()
			m.cycleFocus(delta)
			return true, textinput.Blink
		case "esc":
			m.blurInlineInput()
			m.returnToNavigation()
			return true, nil
		case "enter":
			return true, m.inlineInstallActivate("setup.install")
		default:
			return true, m.updateInlineInput(k)
		}
	}
	// A read-only first-run query must not trap users who want to import a
	// profile, choose another route, or use an executable they already have.
	if m.job != "install" && (key == "a" || key == "n" || key == "b" || key == "i") {
		if m.busy {
			m.abortLookup()
		}
		m.wizard.installRequested = false
		return false, nil
	}
	switch key {
	case "esc":
		if m.job == "install" {
			m.cancelTask(false)
			return true, nil
		}
	case "enter", "space", " ":
		return true, m.inlineInstallActivate(m.focus)
	case "left", "right":
		if m.focus == "setup.version-mode" {
			return true, m.inlineInstallActivate(m.focus)
		}
	}
	return false, nil
}

func (m *Model) inlineInstallActivate(id string) tea.Cmd {
	if !strings.HasPrefix(id, "setup.") || !m.inlineInstallVisible() || m.quitting {
		return nil
	}
	w := &m.wizard
	action := strings.TrimPrefix(id, "setup.")
	w.focus = action
	if m.job == "install" {
		if action == "cancel" || action == "details" {
			return m.wizardActivate(action)
		}
		return nil
	}
	if m.busy && action != "more" && action != "network" && action != "later" && action != "version-mode" && action != "local-mode" && action != "local" && action != "version-input" {
		return nil
	}
	switch action {
	case "more":
		w.more = !w.more
		if !w.more {
			m.blurInlineInput()
		}
	case "later", "done":
		if m.busy {
			m.abortLookup()
		}
		w.inline = false
		m.guideSkipped = true
		m.failure, m.message = "", ""
		m.scroll = 0
		m.blurInlineInput()
		m.returnToNavigation()
	case "start":
		if w.step != stepResult || w.err != "" || !m.snapshot.Installed {
			return nil
		}
		cmd := m.serviceAction("start")
		m.notification.operation.Inline = true
		return cmd
	case "network":
		m.openNetwork(w.err != "")
	case "local", "version-input":
		if m.busy {
			m.abortLookup()
		}
		m.focus = id
		return m.updateInlineInput(nil)
	case "local-mode", "version-mode", "install", "retry", "resolve", "details":
		if action == "version-mode" && w.localMode {
			return nil
		}
		if action == "install" && !m.inlineCanInstall() {
			return nil
		}
		cmd := m.wizardActivate(action)
		if (action == "local-mode" && w.localMode) || (action == "version-mode" && w.specific) {
			m.focus = "setup." + w.focus
		} else if action == "local-mode" || action == "version-mode" {
			m.focus = "setup.install"
		}
		m.revealContentFocus()
		return cmd
	}
	return nil
}

type inlineInstallDocument struct {
	model   Model
	width   int
	lines   []string
	targets []hitRegion
}

func (d *inlineInstallDocument) line(value, color string) {
	d.lines = append(d.lines, strings.Split(wrapText(themed(value, color, panel, false), d.width), "\n")...)
}

func (d *inlineInstallDocument) space() { d.lines = append(d.lines, "") }

func (d *inlineInstallDocument) control(action, label string) {
	id := "setup." + action
	prefix, color, background := "  ", text, panel
	if d.model.hover == id {
		background = hoverBackground
	}
	if d.model.focus == id {
		prefix, color = "› ", focus
	}
	row := len(d.lines)
	d.lines = append(d.lines, strings.Split(wrapText(themed(prefix+label, color, background, false), d.width), "\n")...)
	d.targets = append(d.targets, hitRegion{ID: id, Y: row, W: d.width, H: len(d.lines) - row})
}

func (d *inlineInstallDocument) field(action, label string, input textinput.Model) {
	d.line(label, muted)
	id := "setup." + action
	styleInput(&input, inputBackground, input.Err != nil)
	input.SetWidth(max(1, d.width-2))
	if d.model.focus == id {
		input.Focus()
	} else {
		input.Blur()
	}
	row := len(d.lines)
	d.lines = append(d.lines, themed(cellPad(input.View(), d.width), text, inputBackground, false))
	d.targets = append(d.targets, hitRegion{ID: id, Y: row, W: d.width, H: 1})
}

func (d *inlineInstallDocument) buttons(buttons ...wizardButton) {
	o := d.model.navigationOptions()
	line := ""
	for _, button := range buttons {
		id := "setup." + button.ID
		primary := button.ID == "install" || button.ID == "retry" || button.ID == "start"
		label := dashboardButton(id, button.Label, o, primary)
		if button.ID == "install" && !d.model.inlineCanInstall() || d.model.wizard.cancelling && button.ID == "cancel" {
			label = themed("[ "+button.Label+" ]", disabled, panel, false)
		}
		label = cellCrop(label, d.width)
		width := ansi.StringWidth(label)
		if line != "" && ansi.StringWidth(line)+2+width > d.width {
			d.lines = append(d.lines, line)
			line = ""
		}
		if line != "" {
			line += "  "
		}
		d.targets = append(d.targets, hitRegion{ID: id, X: ansi.StringWidth(line), Y: len(d.lines), W: width, H: 1})
		line += label
	}
	if line != "" {
		d.lines = append(d.lines, line)
	}
}

func (m Model) inlineInstallBody(width int) (string, []hitRegion) {
	w := m.wizard
	if m.inlineInstallCompact(width) {
		lines := []string{
			themed(cellCrop(m.tr("dashboard.guide.installTitle"), width), accent, bg, false),
			themed(cellCrop(m.tr("wizard.enlarge"), width), text, bg, false),
		}
		if w.step == stepInstalling {
			label := m.stageLabel(w.progress.Stage)
			if w.cancelling {
				label = m.tr("wizard.cancelling")
			}
			lines = append(lines, themed(cellCrop(label, width), muted, bg, false))
		}
		return strings.Join(lines, "\n"), nil
	}
	d := inlineInstallDocument{model: m, width: max(1, width-4)}
	d.line(lipgloss.NewStyle().Bold(true).Render(m.tr("dashboard.guide.installTitle")), accent)
	d.space()
	switch w.step {
	case stepInstalling:
		label := m.stageLabel(w.progress.Stage)
		if w.cancelling {
			label = m.tr("wizard.cancelling")
		}
		d.line(m.spinner.View()+" "+label, text)
		d.line(w.plan.Release.Version, muted)
		d.line(m.tr("dashboard.setup.method", m.wizardSourceName(w.cfg)), muted)
		if w.progress.Stage == "download" {
			summary := m.wizardByteSize(w.progress.Downloaded)
			if w.progress.Total > 0 {
				fraction := min(1, float64(w.progress.Downloaded)/float64(w.progress.Total))
				bar := progress.New(progress.WithWidth(min(54, d.width)), progress.WithoutPercentage(), progress.WithColors(lipgloss.Color(info)))
				d.line(bar.ViewAs(fraction), info)
				summary += " / " + m.wizardByteSize(w.progress.Total) + fmt.Sprintf(" · %.0f%%", fraction*100)
			}
			d.line(summary+" · "+m.wizardByteSize(int64(w.progress.BytesPerSecond))+"/s", text)
		}
		if w.progress.Detail != "" {
			d.line(clean(m.localizeDetail(w.progress.Detail)), muted)
		}
		d.space()
		d.buttons(wizardButton{"cancel", m.tr("wizard.cancel")})
		d.control("details", m.tr(map[bool]string{false: "wizard.details_show", true: "wizard.details_hide"}[w.details]))
		if w.details {
			for _, line := range w.logs {
				d.line(clean(line), muted)
			}
		}
	case stepResult:
		if w.err != "" {
			d.line(m.tr("wizard.incomplete"), errorColor)
			d.line(clean(w.err), errorColor)
			if w.failedStage != "" {
				d.line(m.tr("wizard.failed_stage")+": "+m.wizardFailedStage(w.failedStage), muted)
			}
			d.space()
			d.buttons(wizardButton{"retry", m.tr("wizard.retry")}, wizardButton{"done", m.tr("wizard.overview")})
			d.control("network", m.tr("dashboard.setup.downloadMethod"))
		} else {
			d.line(m.tr("dashboard.setup.installed", w.result.InstalledVersion), success)
			d.line(m.tr("dashboard.profile.builtinReady"), text)
			d.line(m.tr("dashboard.setup.startHint"), muted)
			d.space()
			d.buttons(wizardButton{"start", m.tr("dashboard.action.start")}, wizardButton{"done", m.tr("wizard.overview")})
		}
	case stepConfirm:
		// An external installation can appear after the initial snapshot. Its
		// replacement still requires the same reviewed plan as core management.
		if w.plan.BlockedReason != "" {
			d.line(clean(m.locale.Diagnostic(w.plan.BlockedReason, w.plan.BlockedDiagnostic)), errorColor)
		} else {
			d.line(m.tr("wizard.replace_summary"), warning)
			d.line(w.plan.ExistingVersion+" → "+w.plan.Release.Version, text)
			d.buttons(wizardButton{"install", m.tr("wizard.install")})
		}
		d.control("later", m.tr("dashboard.guide.later"))
	default:
		d.line(m.tr("dashboard.setup.description"), text)
		version := m.tr("wizard.version.pending")
		if w.updateCheck.LatestVersion != "" {
			version = w.updateCheck.LatestVersion
		}
		if w.specific {
			version = m.tr("wizard.version.specific")
			if w.version.Value() != "" {
				version = clean(w.version.Value())
			}
		}
		if w.localMode {
			version = m.tr("dashboard.setup.localVersion")
		}
		if m.busy {
			version = m.spinner.View() + " " + clean(m.message)
		}
		versionLabel := "dashboard.setup.latestVersion"
		if w.specific || w.localMode {
			versionLabel = "dashboard.setup.version"
		}
		d.line(m.tr(versionLabel, version), text)
		d.line(m.tr("dashboard.setup.device", runtime.GOOS+" / "+mihomo.RecommendedVariant()), muted)
		d.line(m.tr("dashboard.setup.method", m.wizardSourceName(w.cfg)), muted)
		if w.err != "" {
			d.space()
			d.line(clean(w.err), errorColor)
			d.control("resolve", m.tr("wizard.retry"))
			if !w.more {
				d.control("network", m.tr("dashboard.setup.downloadMethod"))
			}
		} else if !m.busy && !w.specific && !w.localMode && w.updateCheck.LatestVersion == "" {
			d.control("resolve", m.tr("wizard.version.check_again"))
		}
		d.space()
		install := m.tr("wizard.download_install")
		if w.localMode {
			install = m.tr("wizard.install")
		}
		d.buttons(wizardButton{"install", install}, wizardButton{"later", m.tr("dashboard.guide.later")})
		d.space()
		d.control("more", m.tr(map[bool]string{false: "dashboard.setup.more", true: "dashboard.setup.less"}[w.more]))
		if w.more {
			d.space()
			d.control("network", m.tr("dashboard.setup.downloadMethod")+" · "+m.wizardSourceName(w.cfg))
			if !w.localMode {
				mode := m.tr("wizard.version.latest")
				if w.specific {
					mode = m.tr("wizard.version.specific")
				}
				d.control("version-mode", mode)
				if w.specific {
					d.field("version-input", m.tr("wizard.version.identifier"), w.version)
				}
			}
			local := m.tr("wizard.local.choose")
			if w.localMode {
				local = m.tr("wizard.local.selected")
			}
			d.control("local-mode", local)
			if w.localMode {
				d.field("local", m.tr("wizard.local.executable"), w.local)
			}
		}
		d.space()
		d.line(m.tr("dashboard.guide.boundary"), muted)
	}
	if m.failure != "" && w.step == stepResult && w.err == "" {
		d.space()
		d.line(clean(m.failure), errorColor)
	}
	o := m.navigationOptions()
	lines := panelFrame(d.lines, width, len(d.lines)+2, o, accent)
	for i := range d.targets {
		if width >= 4 {
			d.targets[i].X += 2
			d.targets[i].Y++
		}
	}
	return strings.Join(lines, "\n"), d.targets
}
