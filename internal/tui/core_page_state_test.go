package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestStoppedPagesOfferStartAndDoNotReportReconnecting(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, page := range []string{"logs", "rules", "connections"} {
			t.Run(language+"/"+page, func(t *testing.T) {
				m := themeSample("overview-running")
				m.locale = i18n.New(language)
				m.width, m.height, m.page = 80, 24, page
				m.snapshot.Service.Active, m.snapshot.Service.State = false, "inactive"
				m.runtime.snapshot = app.RuntimeSnapshot{State: "stopped", Service: m.snapshot.Service}
				m.rules.received, m.rules.snapshot = true, app.RuleSnapshot{State: "stopped"}
				m.logs = newLogsState(m.locale)
				m.logs.reconnecting = true
				m.layout()
				plain := ansi.Strip(m.screen)
				if !strings.Contains(plain, m.tr("runtime.status.stopped")) || !hasAction(m.targets, "core.start") {
					t.Fatalf("stopped page lost its state or Start action:\n%s", plain)
				}
				for _, misleading := range []string{m.tr("logs.reconnecting"), m.tr("logs.opening"), m.tr("header.connecting")} {
					if strings.Contains(plain, misleading) {
						t.Fatalf("stopped page advertises %q:\n%s", misleading, plain)
					}
				}
				if hasAction(m.targets, "rules.retry") || hasAction(m.targets, "logs.retry") || hasAction(m.targets, "logs.follow") {
					t.Fatal("stopped page retained its live retry/follow controls")
				}
				m.snapshot.Installed = false
				m.rules.snapshot.State, m.runtime.snapshot.State = "uninstalled", "uninstalled"
				m.layout()
				if !hasAction(m.targets, "card.install") || hasAction(m.targets, "core.start") {
					t.Fatalf("uninstalled page did not replace Start with Install: %+v", m.targets)
				}
			})
		}
	}
}

func TestStoppedLogsRetainHistoryAndResumeLiveState(t *testing.T) {
	m := themeSample("overview-running")
	m.page, m.width, m.height = "logs", 80, 24
	m.logs = newLogsState(m.locale)
	m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "retained diagnostic event"}})
	m.logs.reconnecting, m.logs.active = true, true
	m.snapshot.Service.Active, m.snapshot.Service.State = false, "inactive"
	m.layout()
	if plain := ansi.Strip(m.screen); !strings.Contains(plain, "retained diagnostic event") || !strings.Contains(plain, m.tr("logs.ended")) {
		t.Fatalf("stopping lost retained history or its stale-data label:\n%s", plain)
	}
	m.snapshot.Service.Active, m.snapshot.Service.State = true, "active"
	m.logs.reconnecting = false
	m.layout()
	if plain := ansi.Strip(m.screen); strings.Contains(plain, m.tr("runtime.status.stopped")) || !strings.Contains(plain, m.tr("logs.capturing")) {
		t.Fatalf("running log stream retained the stopped state:\n%s", plain)
	}
}
