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
		{i18n.English, "Configurations", "Connection details", "Rule details", "Mihomo logs"},
		{i18n.Chinese, "配置", "连接详情", "规则详情", "Mihomo 日志"},
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
			rule := mihomo.Rule{Index: 0, Type: "Domain", Payload: "Active", Proxy: "Ready", Size: -1}
			rules.rules.snapshot.Rules, rules.rules.detail = []mihomo.Rule{rule}, &rule
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
						bar := m.logsToolbar(width, 4)
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

func TestLogCaptureGapFollowsLanguageWithoutTranslatingCoreMessages(t *testing.T) {
	m := logsModel()
	m.width, m.height = 120, 45
	raw := "Capture resumed; logs while inactive were not collected."
	m.appendLogs([]mihomo.LogEntry{{Level: "gap", Message: logCaptureGap}, {Level: "info", Message: raw}})
	m.locale = i18n.New(i18n.Chinese)
	body, _ := m.logsBody(112)
	plain := ansi.Strip(body)
	if !strings.Contains(plain, "已恢复采集") || !strings.Contains(plain, raw) {
		t.Fatalf("capture gap or original core message changed:\n%s", plain)
	}
	if m.logs.entries[0].Message != logCaptureGap || m.logs.entries[1].Message != raw {
		t.Fatal("rendering rewrote retained log data")
	}
	m.logs.search.SetValue("暂停期间")
	body, _ = m.logsBody(112)
	if !strings.Contains(ansi.Strip(body), "已恢复采集") || strings.Contains(ansi.Strip(body), raw) {
		t.Fatal("search did not use the displayed capture gap text")
	}
	m.logs.search.Reset()
	m.locale = i18n.New(i18n.English)
	body, _ = m.logsBody(112)
	if strings.Count(ansi.Strip(body), raw) != 2 {
		t.Fatal("retained capture gap did not follow the new language")
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
