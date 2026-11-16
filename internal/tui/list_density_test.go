package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestRulesUseAlignedSingleRowsAndKeepExpandedValues(t *testing.T) {
	for _, width := range []int{80, 95, 135} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := rulesModel(t, &fakeBackend{})
			r := sampleRules()[0]
			r.Payload = strings.Repeat("条件.example/", 9)
			r.Type = "UnusuallyLongRuleTypeName"
			r.Proxy = "完整目标组名-with-a-long-target"
			r.Extra, r.Size = &mihomo.RuleExtra{Disabled: true}, 123
			collapsed := m.ruleRows(r, width, 1)
			if len(collapsed) != 1 || !strings.Contains(ansi.Strip(collapsed[0]), m.tr("rules.disabled_suffix")) {
				t.Fatal("wide rules did not use a single row with the disabled state")
			}
			m.rules.expanded = ruleIdentity(r)
			expanded := m.ruleRows(r, width, 1)
			if len(expanded) != m.ruleHeight(r, width, 1) || len(expanded) <= 1 {
				t.Fatal("expanded geometry did not include the complete fields")
			}
			columns, _ := ruleTableColumns(width, 1)
			for column, expected := range []string{r.Payload, r.Type, r.Proxy + m.tr("rules.disabled_suffix")} {
				x := 5
				if column >= 1 {
					x += columns[0] + 2
				}
				if column == 2 {
					x += columns[1] + 3
				}
				var actual strings.Builder
				for _, row := range expanded[:len(expanded)-1] {
					actual.WriteString(strings.TrimSpace(ansi.Strip(ansi.Cut(row, x, x+columns[column]))))
				}
				if strings.ReplaceAll(actual.String(), " ", "") != strings.ReplaceAll(expected, " ", "") {
					t.Fatalf("expanded column %d lost data: got %q, want %q", column, actual.String(), expected)
				}
			}
			if !strings.Contains(ansi.Strip(expanded[len(expanded)-1]), "123") {
				t.Fatal("expanded rule lost its entry count")
			}
			for _, row := range expanded {
				if ansi.StringWidth(row) != width {
					t.Fatal("expanded table row escaped its columns")
				}
			}
		})
	}
}

func TestRulesRowsHaveNoSeparatorAndPreserveMatchOrder(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	m.rules.snapshot.Rules = []mihomo.Rule{sampleRules()[2], sampleRules()[0], sampleRules()[1]}
	for _, width := range []int{56, 76, 95, 135} {
		body := m.rulesBodyWindow(width, 0, -1)
		wantHeight := 2
		if width >= 80 {
			wantHeight = 1
		}
		if body.total != 3*wantHeight || len(body.targets) != 3 {
			t.Fatalf("width %d retained separator rows: total=%d", width, body.total)
		}
		for index, id := range []string{"rule.2", "rule.0", "rule.1"} {
			target := body.targets[index]
			if target.ID != id || target.Y != index*wantHeight || target.H != wantHeight {
				t.Fatalf("width %d reordered or misplaced %s: %+v", width, id, target)
			}
		}
	}
}

func TestListToolbarsReserveOnlyVisibleSearchAndStatusRows(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	m.width, m.height = 80, 24
	for _, page := range []string{"rules", "logs"} {
		m.page = page
		rows := m.filterToolbarRows(76)
		bar := m.rulesToolbar(76, rows)
		if page == "logs" {
			bar = m.logsToolbar(76, rows)
		}
		wantRows := 3
		if page == "logs" {
			wantRows = 4
		}
		if rows != wantRows || len(bar.lines) != wantRows || strings.TrimSpace(ansi.Strip(bar.lines[0])) == "" || strings.TrimSpace(ansi.Strip(bar.lines[wantRows-1])) == "" {
			t.Fatalf("%s reserves blank space around its three-row search frame", page)
		}
	}
	m.page, m.rules.regex = "rules", true
	m.rules.search.SetValue("[")
	rows := m.filterToolbarRows(76)
	bar := m.rulesToolbar(76, rows)
	if rows != 4 || !strings.Contains(ansi.Strip(bar.lines[3]), m.tr("rules.invalid_regex")) {
		t.Fatal("invalid search did not add its visible diagnostic row")
	}
}

func TestLogsDefaultCompactAndDetailedViewPreserveFullMessages(t *testing.T) {
	m := logsModel()
	message := "first line\n" + strings.Repeat("中文 segment ", 12) + "END-OF-MESSAGE"
	m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "short event"}, {Level: "warn", Message: message}})
	doc := m.logDocument(76)
	if !m.logs.compact || len(doc.lines) != 2 || strings.Contains(doc.lines[1], "\n") {
		t.Fatal("default log layout is not one row per event")
	}
	m.logsKey(key("v"))
	doc = m.logDocument(76)
	if m.logs.compact || !strings.Contains(ansi.Strip(doc.lines[0]), "INFO   short event") {
		t.Fatal("detailed logs did not keep short events on their timestamp row")
	}
	plain := ansi.Strip(strings.Join(doc.lines, "\n"))
	compactText := strings.Join(strings.Fields(plain), "")
	if !strings.Contains(compactText, "END-OF-MESSAGE") || strings.Contains(plain, "────") {
		t.Fatal("detailed logs clipped the message or retained separator rows")
	}
	m.logsKey(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if !m.logs.compact || len(m.logs.entries) != 2 {
		t.Fatal("switching back changed retained events")
	}
}
