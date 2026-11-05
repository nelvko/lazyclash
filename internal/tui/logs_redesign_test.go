package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func redesignedLogsModel(width, height, count int) Model {
	m := logsModel()
	m.width, m.height = width, height
	for n := 0; n < count; n++ {
		m.appendLogs([]mihomo.LogEntry{{Time: time.Unix(int64(n), 0), Level: "info", Message: fmt.Sprintf("event-%03d 中文🚀", n)}})
	}
	m.syncLogs()
	m.layout()
	return m
}

func logReadingView(m Model) string {
	width := dashboardBodyWidth(m.dashboardOptions())
	v := m.logViewport(m.logDocument(width), width, m.logsCapacity(width))
	return ansi.Strip(v.View())
}

func TestLogsBrowsingPinsVisibleRowsWhileCaptureContinuesAndEntriesExpire(t *testing.T) {
	m := redesignedLogsModel(120, 32, retainedLogLimit)
	defer m.stopLogs()
	ctx, generation := m.logs.ctx, m.logs.generation
	m.logsScroll(-20)
	before := logReadingView(m)
	anchor := m.logs.anchor
	for n := retainedLogLimit; n < retainedLogLimit+12; n++ {
		m.acceptLogs(logsResultMsg{generation: generation, entries: []mihomo.LogEntry{{Level: "info", Message: fmt.Sprintf("event-%03d", n)}}})
	}
	if got := logReadingView(m); got != before || m.logs.anchor != anchor {
		t.Fatalf("incoming entries moved the pinned reading view:\nbefore:\n%s\nafter:\n%s", before, got)
	}
	if !m.logs.active || m.logs.paused || ctx.Err() != nil || m.logs.generation != generation || m.logs.unread != 12 {
		t.Fatal("browsing changed capture ownership or failed to count new events")
	}
	if cmd := m.logsActivate("logs.latest"); cmd != nil || m.logs.holding || m.logs.unread != 0 || m.logs.generation != generation {
		t.Fatal("returning to the live tail restarted capture or kept the unread state")
	}
	if !strings.Contains(logReadingView(m), "event-211") {
		t.Fatal("returning to latest did not reveal the newest event")
	}
	m.logsKey(special(tea.KeyHome))
	m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "event-212"}, {Level: "info", Message: "event-213"}})
	got := logReadingView(m)
	if !strings.Contains(got, "event-014") || strings.Contains(got, "event-212") {
		t.Fatalf("expired anchor did not fall back to the oldest retained event:\n%s", got)
	}
	m.appendLogs(make([]mihomo.LogEntry, retainedLogLimit+20))
	if m.logs.unread != retainedLogLimit || len(m.logs.entries) != retainedLogLimit {
		t.Fatal("unread count or retained storage grew beyond the visible retention bound")
	}
}

func TestLogsPinnedEntrySurvivesResizeAndCompactReflow(t *testing.T) {
	m := redesignedLogsModel(80, 24, 0)
	defer m.stopLogs()
	for n := 0; n < 40; n++ {
		m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: fmt.Sprintf("event-%03d ", n) + strings.Repeat("中文🚀 segment ", 16)}})
	}
	m.logs.holding, m.logs.anchor = true, logPosition{entry: 8, row: 4}
	ctx, generation := m.logs.ctx, m.logs.generation
	for _, size := range [][2]int{{160, 45}, {40, 16}, {120, 32}, {5, 2}, {80, 24}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		assertCanvas(t, m.screen, size[0], size[1])
		assertRegions(t, m.targets, size[0], size[1])
		if ctx.Err() != nil || m.logs.generation != generation || m.logs.anchor.entry != 8 || !m.logs.holding {
			t.Fatal("resize replaced capture or the pinned event")
		}
		width := dashboardBodyWidth(m.dashboardOptions())
		doc := m.logDocument(width)
		view := m.logViewport(doc, width, m.logsCapacity(width))
		if got := doc.positions[view.YOffset()].entry; got != 8 {
			t.Fatalf("%v reflow skipped the anchored event: got entry %d", size, got)
		}
	}
	m.logsActivate("logs.view")
	doc := m.logDocument(76)
	view := m.logViewport(doc, 76, 15)
	if !m.logs.compact || !strings.HasPrefix(ansi.Strip(view.View()), "--:--:--  INFO   event-008") {
		t.Fatalf("compact reflow skipped or hid the pinned event:\n%s", ansi.Strip(view.View()))
	}
	m.logsActivate("logs.view")
	if m.logs.compact || m.logs.anchor.entry != 8 || m.logs.generation != generation {
		t.Fatal("returning to details discarded the pinned event or restarted capture")
	}
}

func TestLogsLevelAndTextFiltersKeepSeverityAndCaptureGaps(t *testing.T) {
	m := logsModel()
	for _, entry := range []mihomo.LogEntry{
		{Level: "info", Message: "Alpha message"}, {Level: "warning", Message: "Alpha warning"},
		{Level: "warn", Message: "Alpha alias"}, {Level: "error", Message: "Alpha error"},
		{Level: "fatal", Message: "Alpha fatal"}, {Level: "debug", Message: "Alpha debug"},
		{Level: "gap", Message: logCaptureGap},
	} {
		m.appendLogs([]mihomo.LogEntry{entry})
	}
	for _, tc := range []struct {
		level int
		query string
		want  []string
	}{
		{0, "aLpHa", []string{"info", "warning", "warn", "error", "fatal", "debug", "gap"}},
		{1, "", []string{"info", "gap"}},
		{2, "", []string{"warning", "warn", "gap"}},
		{3, "alpha", []string{"error", "fatal", "gap"}},
		{3, "nonexistent", []string{"gap"}},
	} {
		m.logs.level = tc.level
		m.logs.search.SetValue(tc.query)
		var got []string
		for _, entry := range m.logs.entries {
			if m.logMatches(entry) {
				got = append(got, entry.Level)
			}
		}
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("level=%d query=%q: got %v; want %v", tc.level, tc.query, got, tc.want)
		}
	}
	m.locale = i18n.New(i18n.Chinese)
	if got := strings.Join(m.logDocument(70).lines, "\n"); !strings.Contains(ansi.Strip(got), m.tr(logCaptureGap)) {
		t.Fatal("filters hid the localized capture gap")
	}
}

func TestLogsClearOnlyRemovesRetainedEntriesAndPreservesStreamAndFilters(t *testing.T) {
	m := redesignedLogsModel(120, 32, 20)
	defer m.stopLogs()
	m.logs.search.SetValue("Needle")
	m.logs.level, m.logs.compact = 3, true
	m.logs.holding, m.logs.unread = true, 7
	ctx, generation := m.logs.ctx, m.logs.generation
	if cmd := m.logsActivate("logs.clear"); cmd != nil {
		t.Fatal("clear unexpectedly started a backend operation")
	}
	if len(m.logs.entries) != 0 || !m.logs.observedAt.IsZero() || m.logs.holding || m.logs.unread != 0 || m.logs.search.Value() != "Needle" || m.logs.level != 3 || !m.logs.compact {
		t.Fatal("clear did not reset only retained log data and reading state")
	}
	if ctx.Err() != nil || !m.logs.active || m.logs.paused || m.logs.generation != generation || m.logs.first != 20 {
		t.Fatal("clear interrupted capture or reused expired entry identities")
	}
	m.acceptLogs(logsResultMsg{generation: generation - 1, entries: []mihomo.LogEntry{{Level: "error", Message: "Needle stale"}}})
	m.acceptLogs(logsResultMsg{generation: generation, entries: []mihomo.LogEntry{{Level: "error", Message: "Needle current"}}})
	if len(m.logs.entries) != 1 || m.logs.entries[0].Message != "Needle current" || !strings.Contains(logReadingView(m), "Needle current") {
		t.Fatal("clear accepted an old stream result or failed to continue the existing stream")
	}
	m.logsActivate("logs.reset-search")
	if m.logs.search.Value() != "" || m.logs.level != 3 || !m.logs.compact || len(m.logs.entries) != 1 {
		t.Fatal("search reset cleared filters, layout or retained events")
	}
	m.pauseLogs()
	m.logsActivate("logs.clear")
	if !m.logs.paused || m.logs.active || !m.logs.gap {
		t.Fatal("clearing a paused capture resumed it or concealed the pending capture gap")
	}
}

func TestLogsIdleFilteringAndFailureHaveDistinctStates(t *testing.T) {
	m := redesignedLogsModel(120, 32, 0)
	defer m.stopLogs()
	if got := ansi.Strip(m.screen); !strings.Contains(got, m.tr("logs.empty")) || !strings.Contains(got, m.tr("logs.waiting")) || strings.Contains(got, "Start Mihomo") {
		t.Fatalf("healthy idle capture gave misleading guidance:\n%s", got)
	}
	m.logs.search.SetValue("no-such-event")
	m.layout()
	if got := ansi.Strip(m.screen); !strings.Contains(got, m.tr("logs.no_matches")) || !m.logs.active {
		t.Fatal("empty filtered view looked like a stopped capture")
	}
	m.acceptLogs(logsResultMsg{generation: m.logs.generation, done: true, err: errors.New("fixture disconnected")})
	m.layout()
	if got := ansi.Strip(m.screen); !strings.Contains(got, "fixture disconnected") || !strings.Contains(got, m.tr("logs.ended")) || m.syncLogs() != nil {
		t.Fatal("failed capture concealed the failure or retried automatically")
	}
}

func TestLogsToolbarKeepsKeyboardOrderAndMouseTargetsWhileBrowsing(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {80, 24}, {60, 20}, {40, 16}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := redesignedLogsModel(size[0], size[1], 50)
			defer m.stopLogs()
			ctx, generation := m.logs.ctx, m.logs.generation
			search := findTarget(t, m, "logs.search")
			m, _ = update(m, special(tea.KeyHome))
			m, _ = update(m, special(tea.KeyDown))
			anchor := m.logs.anchor
			if got := findTarget(t, m, "logs.search"); got != search {
				t.Fatal("reading history moved the fixed search toolbar")
			}
			m, _ = click(m, search)
			if !m.logs.editing || !m.logs.search.Focused() || m.logs.paused || !m.logs.active || m.logs.anchor != anchor {
				t.Fatal("search focus interrupted capture or the reading position")
			}
			for _, id := range []string{"logs.level", "logs.view", "logs.reset-search", "logs.list", "logs.latest"} {
				m, _ = update(m, special(tea.KeyTab))
				if m.focus != id || m.logs.editing || m.logs.search.Focused() {
					t.Fatalf("Tab should visit %s; got %s (editing=%t)", id, m.focus, m.logs.editing)
				}
			}
			m, _ = click(m, findTarget(t, m, "logs.level"))
			if m.logs.level != 1 || ctx.Err() != nil || m.logs.generation != generation {
				t.Fatal("level click changed capture instead of only filtering display")
			}
			m, _ = click(m, findTarget(t, m, "logs.view"))
			if !m.logs.compact || ctx.Err() != nil {
				t.Fatal("view click did not toggle layout independently of capture")
			}
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
		})
	}
}
