package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func logsToolbarFixture(language string, width int) Model {
	m := redesignedLogsModel(width, 24, 0)
	m.locale = i18n.New(language)
	for index := range 12 {
		message := "other"
		if index%2 == 0 {
			message = "match"
		}
		m.appendLogs([]mihomo.LogEntry{{Level: []string{"info", "warning", "error"}[index%3], Message: fmt.Sprintf("%s-%d", message, index)}})
	}
	m.layout()
	return m
}

func visibleLogCountPosition(t *testing.T, m Model, full, compact string) (int, int) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(m.screen), "\n") {
		for _, count := range []string{full, compact} {
			if index := strings.Index(line, count); index >= 0 {
				return ansi.StringWidth(line[:index]), y
			}
		}
	}
	t.Fatalf("filtered count %q or %q was not visible:\n%s", full, compact, ansi.Strip(m.screen))
	return 0, 0
}

func TestLogsToolbarOnlyShowsCountsWhenTextOrLevelFiltersAreActive(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{160, 80, 60, 40, 32} {
			t.Run(fmt.Sprint(language, "/", width), func(t *testing.T) {
				m := logsToolbarFixture(language, width)
				defer m.stopRuntime()
				for _, test := range []struct {
					name, query string
					level, want int
					regular     bool
					filtered    bool
				}{
					{name: "default"},
					{name: "blank with matching modes", query: "  ", regular: true},
					{name: "text", query: "match", want: 6, filtered: true},
					{name: "level", level: 2, want: 4, filtered: true},
					{name: "intersection", query: "match", level: 2, want: 2, filtered: true},
					{name: "invalid expression", query: "[", regular: true, filtered: true},
				} {
					m.logs.search.SetValue(test.query)
					m.logs.level, m.logs.regex = test.level, test.regular
					m.layout()
					full, compact := m.logCount()
					if !test.filtered {
						if full != "" || compact != "" || strings.Contains(ansi.Strip(m.screen), "12 / 12") {
							t.Fatalf("%s showed an unfiltered count: full=%q compact=%q", test.name, full, compact)
						}
					} else {
						wantFull, wantCompact := m.tr("logs.filtered_count", test.want, 12), fmt.Sprintf("%d / 12", test.want)
						if full != wantFull || compact != wantCompact {
							t.Fatalf("%s count: got %q/%q; want %q/%q", test.name, full, compact, wantFull, wantCompact)
						}
						x, y := visibleLogCountPosition(t, m, wantFull, wantCompact)
						field := findTarget(t, m, "logs.search")
						if x >= field.X && x < field.X+field.W && y >= field.Y && y < field.Y+field.H {
							t.Fatalf("%s count was inside the search frame: x=%d y=%d field=%+v", test.name, x, y, field)
						}
						if width >= 60 && y != field.Y+1 {
							t.Fatalf("%s count did not share the input row at width %d: y=%d field=%+v", test.name, width, y, field)
						}
					}
					plain := ansi.Strip(m.screen)
					if strings.Contains(plain, "Mihomo logs") || strings.Contains(plain, "Mihomo 日志") {
						t.Fatalf("%s restored the removed persistent summary:\n%s", test.name, plain)
					}
					assertCanvas(t, m.screen, width, 24)
					assertRegions(t, m.targets, width, 24)
				}
				m.logs.search.Reset()
				m.logs.level = 0
				m.logs.entries = nil
				m.layout()
				if strings.Contains(ansi.Strip(m.screen), "0 / 0") {
					t.Fatal("an unfiltered empty list showed a redundant zero count")
				}
			})
		}
	}
}

func TestLogsToolbarCountAndCacheGrowthKeepControlGeometry(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{160, 80, 60, 40, 32} {
			t.Run(fmt.Sprint(language, "/", width), func(t *testing.T) {
				m := logsToolbarFixture(language, width)
				defer m.stopRuntime()
				ctx, generation := m.runtime.ctx, m.runtime.generation
				baseline := map[string]hitRegion{}
				for _, id := range []string{"logs.search", "logs.level", "logs.view"} {
					baseline[id] = findTarget(t, m, id)
				}
				assertStable := func() {
					t.Helper()
					m.layout()
					for id, want := range baseline {
						if got := findTarget(t, m, id); got != want {
							t.Fatalf("filter or cache growth moved %s: got %+v; want %+v", id, got, want)
						}
					}
					if m.runtime.generation != generation || ctx.Err() != nil || !m.logs.active {
						t.Fatal("local count or display filtering replaced capture")
					}
				}
				m.logs.search.SetValue("match")
				assertStable()
				for _, total := range []int{99, 100, retainedLogLimit} {
					for len(m.logs.entries) < total {
						m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "match incoming"}})
					}
					assertStable()
				}
				m.logs.search.Reset()
				assertStable()
				for level := range 4 {
					m.logs.level = level
					assertStable()
				}
			})
		}
	}
}

func TestLogsToolbarCountUsesPausedSnapshotWhileLiveCacheGrows(t *testing.T) {
	m := logsToolbarFixture(i18n.English, 80)
	defer m.stopRuntime()
	m.logs.search.SetValue("match")
	m.pauseLogs()
	ctx, generation := m.runtime.ctx, m.runtime.generation
	for len(m.logs.entries) < retainedLogLimit {
		m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "match incoming"}})
	}
	full, compact := m.logCount()
	if full != m.tr("logs.filtered_count", 6, 12) || compact != "6 / 12" || len(m.logs.frozen) != 12 || len(m.logs.entries) != retainedLogLimit {
		t.Fatalf("paused count followed the growing live cache: full=%q compact=%q", full, compact)
	}
	m.logs.level = 2
	full, compact = m.logCount()
	if full != m.tr("logs.filtered_count", 2, 12) || compact != "2 / 12" || m.runtime.generation != generation || ctx.Err() != nil || !m.logs.paused {
		t.Fatal("filtering the paused snapshot changed capture or used live events")
	}
}
