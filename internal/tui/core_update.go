package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func (m *Model) checkCoreUpdate() tea.Cmd {
	if m.busy {
		return nil
	}
	w := &m.wizard
	w.updateMode, w.upgrading = true, false
	w.installRequested = false
	w.step, w.focus, w.scroll, w.reveal = stepUpdate, "cancel", 0, true
	return m.queryCoreVersion("check-update", "")
}

func (m *Model) checkInstallVersion() tea.Cmd {
	if m.busy {
		return nil
	}
	w := &m.wizard
	w.updateMode, w.upgrading = false, false
	w.step, w.focus, w.reveal = stepVersion, "install", true
	w.release, w.plan = mihomo.Release{}, app.InstallPlan{}
	return m.queryCoreVersion("install-check", mihomo.ChannelStable)
}

// Installation and upgrades share the same read-only version query. Only an
// explicit install action proceeds from this observation to asset resolution.
func (m *Model) queryCoreVersion(kind string, channel mihomo.Channel) tea.Cmd {
	w := &m.wizard
	w.updateCheck, w.err, w.failedKind, w.refreshError = app.CoreUpdateCheck{}, "", "", ""
	ctx := m.newJob("check-update")
	id, backend, cfg := m.id, m.backend, w.cfg
	m.message = m.tr("core_update.checking")
	return tea.Batch(func() tea.Msg {
		check, err := backend.CheckUpdate(ctx, channel, cfg)
		if err == nil && kind == "install-check" && check.LatestVersion == "" {
			err = diagnostic.New("diagnostic.release.versionChoice", "choose latest or a stable Mihomo version such as v1.19.33")
		}
		return wizardMsg{id: id, kind: kind, updateCheck: check, err: err}
	}, m.spinner.Tick)
}

func (m *Model) updateStatusLabel(status mihomo.UpdateStatus) string {
	switch status {
	case mihomo.UpdateCurrent:
		return m.tr("core_update.current")
	case mihomo.UpdateAvailable:
		return m.tr("core_update.available")
	case mihomo.UpdateAhead:
		return m.tr("core_update.ahead")
	case mihomo.UpdateIncomparable:
		return m.tr("core_update.incomparable")
	case mihomo.UpdateNotInstalled:
		return m.tr("core_update.not_installed")
	case mihomo.UpdateUnavailable:
		return m.tr("core_update.unavailable")
	default:
		return ""
	}
}

func (m *Model) coreUpdateDocument(d wizardDocument) (wizardDocument, []wizardButton, string) {
	w := &m.wizard
	compact := m.height < 20
	if !compact {
		d.label(m.tr("core_update.review"))
	}
	buttons := []wizardButton{{"back", m.tr("wizard.back")}}
	if m.busy {
		d.line(m.spinner.View() + " " + m.tr("core_update.checking"))
		return d, []wizardButton{{"cancel", m.tr("wizard.cancel")}}, m.tr("core_update.title")
	}
	c := w.updateCheck
	if !c.CheckedAt.IsZero() {
		installed := c.InstalledVersion
		if installed == "" {
			installed = m.tr("wizard.unknown")
			if c.Status == mihomo.UpdateNotInstalled {
				installed = m.tr("wizard.not_installed")
			}
		}
		d.pair(m.tr("wizard.installed_version"), installed)
		latest := c.LatestVersion
		if latest == "" {
			latest = m.tr("wizard.unknown")
		}
		d.pair(m.tr("core_update.latest"), latest)
		status := m.updateStatusLabel(c.Status)
		if compact && c.Status == mihomo.UpdateAvailable {
			status = m.tr("core_update.available_short")
		}
		d.line(status)
		if c.InstalledError != "" {
			d.line(themed(clean(m.locale.Diagnostic(c.InstalledError, c.InstalledDiagnostic)), errorColor, raised, false))
		}
		if c.Status == mihomo.UpdateAvailable && c.Channel == mihomo.ChannelStable {
			confirmation := m.tr("core_update.confirm_short")
			if !compact {
				d.space()
				confirmation = m.tr("core_update.confirm")
			}
			d.line(themed(confirmation, warning, raised, false))
			buttons = append(buttons, wizardButton{"upgrade", m.tr("core_update.upgrade")})
		}
		d.space()
		d.pair(m.tr("core_update.channel"), string(c.Channel))
		d.pair(m.tr("core_update.checked_at"), c.CheckedAt.Local().Format("2006-01-02 15:04:05 MST"))
	}
	if w.err != "" {
		d.space()
		d.line(themed(clean(w.err), errorColor, raised, false))
	}
	d.space()
	d.control("network", m.tr("wizard.network.title"), w.focus)
	d.line(m.tr("core_update.read_only"))
	buttons = append(buttons, wizardButton{"check-update", m.tr("core_update.check")})
	return d, buttons, m.tr("core_update.title")
}

func (m *Model) coreUpgradeResultDocument(d wizardDocument) (wizardDocument, []wizardButton, string) {
	w := &m.wizard
	buttons := []wizardButton{{"done", m.tr("wizard.done")}}
	if w.err != "" {
		d.line(themed(m.tr("core_update.failed"), errorColor, raised, false))
		d.pair(m.tr("wizard.failed_stage"), m.wizardFailedStage(w.failedStage))
		d.line(themed(clean(w.err), errorColor, raised, false))
		d.line(m.tr("core_update.retry"))
		buttons = append(buttons, wizardButton{"check-update", m.tr("core_update.check")})
	} else {
		d.line(themed(m.tr("core_update.succeeded"), success, raised, false))
		d.pair(m.tr("wizard.existing_version"), w.upgradeResult.PreviousVersion)
		d.pair(m.tr("wizard.installed_version"), w.upgradeResult.InstalledVersion)
		if w.upgradeResult.Restarted {
			d.line(m.tr("core_update.restarted"))
		} else {
			d.line(m.tr("core_update.stopped"))
		}
	}
	if w.refreshError != "" {
		d.space()
		d.line(themed(w.refreshError, warning, raised, false))
	}
	if w.details && len(w.logs) > 0 {
		d.space()
		d.line(strings.Join(w.logs, "\n"))
	}
	return d, buttons, m.tr("core_update.title")
}
