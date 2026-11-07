package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestDataPagesLocalizeControlsAndKeepObservedValues(t *testing.T) {
	for _, language := range []struct{ code, profiles, connections, rules, logs string }{
		{i18n.English, "Configurations", "Connection details", "Entries: 0", "Mihomo logs"},
		{i18n.Chinese, "配置", "连接详情", "条目：0", "Mihomo 日志"},
	} {
		t.Run(language.code, func(t *testing.T) {
			profiles := ready(&fakeBackend{snapshot: app.Snapshot{
				Profiles: []string{"work"},
				ProfileDetails: []app.ProfileInfo{
					{ID: "default", Name: "default", Validated: true},
					{ID: "work", Name: "Ready", Origin: "https://Active.example", Validated: true},
				},
			}})
			profiles.page, profiles.focus = "profiles", "profile.1"
			connections := connectionsModel(t, &fakeBackend{})
			connection := fixtureConnection("Active", "Rule", 0)
			connection.Metadata.Process = "Ready"
			connections.runtime.snapshot.Connections.Connections = []mihomo.Connection{connection}
			connections.connections.selected, connections.connections.detail = connection.ID, &connection
			rules := rulesModel(t, &fakeBackend{})
			rule := mihomo.Rule{Index: 0, Type: "Domain", Payload: "Active", Proxy: "Ready", Size: 0}
			rules.rules.snapshot.Rules, rules.rules.expanded, rules.rules.selected = []mihomo.Rule{rule}, ruleIdentity(rule), ruleIdentity(rule)
			logs := logsModel()
			logs.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "Active Ready Rule"}})
			for _, item := range []struct {
				model        Model
				title, value string
			}{
				{profiles, language.profiles, "Ready"},
				{connections, language.connections, "Rule:443"},
				{rules, language.rules, "Active"},
				{logs, language.logs, "Active Ready Rule"},
			} {
				m := item.model
				m.locale = i18n.New(language.code)
				for _, size := range [][2]int{{160, 45}, {100, 28}, {80, 24}, {60, 20}, {40, 16}} {
					m.width, m.height = size[0], size[1]
					width := dashboardBodyWidth(dashboardOptions{Width: m.width, Height: m.height, Page: m.page})
					body, _ := m.pageBody(width)
					if m.page == "logs" {
						bar := m.logsToolbar(width, m.filterToolbarRows(width))
						body = strings.Join(bar.lines, "\n") + "\n" + body
					}
					plain := ansi.Strip(body)
					if !strings.Contains(plain, item.title) || !strings.Contains(plain, item.value) {
						t.Fatalf("%s/%s at %v lost translated title or original data:\n%s", language.code, m.page, size, plain)
					}
					m.layout()
					assertCanvas(t, m.screen, m.width, m.height)
					assertRegions(t, m.targets, m.width, m.height)
				}
			}
		})
	}
}

func TestLogRecoveryStatusLocalizesWithoutChangingCoreMessages(t *testing.T) {
	m := logsModel()
	m.width, m.height = 120, 45
	raw := "Connection restored; original core message"
	m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: raw}})
	m.logs.recovered = true
	for _, language := range []string{i18n.Chinese, i18n.English} {
		m.locale = i18n.New(language)
		body, _ := m.logsBody(112)
		if got := ansi.Strip(body); !strings.Contains(got, m.tr("logs.recovered")) || strings.Count(got, raw) != 1 {
			t.Fatalf("recovery notice or original core message changed:\n%s", got)
		}
		m.logs.search.SetValue("nonexistent")
		body, _ = m.logsBody(112)
		if got := ansi.Strip(body); !strings.Contains(got, m.tr("logs.recovered")) || strings.Contains(got, raw) || m.logDocument(112).matched != 0 {
			t.Fatal("recovery status entered the filtered log list or changed its count")
		}
		m.logs.search.Reset()
	}
	if len(m.logs.entries) != 1 || m.logs.entries[0].Message != raw {
		t.Fatal("recovery or language changes inserted a synthetic log entry")
	}
}

func TestProfileUpdateFormattingAndInputsUseSelectedLanguage(t *testing.T) {
	now := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	if got := profileUpdated(now.Add(-2*time.Hour), now, i18n.New(i18n.Chinese)); got != "2 小时前更新" {
		t.Fatalf("unexpected Chinese relative time: %s", got)
	}
	if got := profileUpdated(now.Add(-2*time.Hour), now); got != "Updated 2h ago" {
		t.Fatalf("English formatter changed: %s", got)
	}
	if got := newProfilesState(i18n.New(i18n.Chinese)).url.Placeholder; got != "粘贴订阅 URL 或本地路径" {
		t.Fatalf("unexpected subscription placeholder: %s", got)
	}
	if got := newLogsState(i18n.New(i18n.Chinese)).search.Placeholder; got != "筛选日志级别或内容" {
		t.Fatalf("unexpected log placeholder: %s", got)
	}
}
