package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestConnectionsTableFits85ColumnsAnd120ColumnTerminal(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	for _, width := range []int{84, 85, 94, 95, 100, 124, 125, 154, 155, 200} {
		body, targets := m.connectionsBody(width)
		assertCanvas(t, body, width, 1000)
		for _, target := range targets {
			if !strings.HasPrefix(target.ID, "connection.") {
				continue
			}
			want := 1
			if width == 84 {
				want = 2
			}
			if target.H != want {
				t.Fatalf("width %d renders %d rows per connection, want %d", width, target.H, want)
			}
		}
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if hit := findTarget(t, m, "connection.first"); hit.H != 1 {
		t.Fatal("a 120-column terminal still uses the compact list")
	}
}

func TestConnectionTableNumbersAlignWithTheirHeaders(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{85, 95, 125, 155, 200} {
			t.Run(fmt.Sprintf("%s/%d", language, width), func(t *testing.T) {
				m := connectionsModel(t, &fakeBackend{})
				m.locale = i18n.New(language)
				c := fixtureConnection("aligned", "destination.example", 0)
				m.runtime.snapshot.ObservedAt = c.Start.Add(65 * time.Second)
				m.connections.rates.values = map[string]connectionRate{c.ID: {Download: 2048, Upload: 0, Valid: true}}
				columns, _, numericColumn := m.connectionColumns(width)
				lines := m.connectionTable([]mihomo.Connection{c}, width).lines
				values := []string{"2.0 KiB/s", "0 B/s"}
				if width >= 155 {
					values = append(values, formatBytes(c.Download), formatBytes(c.Upload))
				}
				values = append(values, "1m05s")
				x := 0
				for index, column := range columns {
					if index >= numericColumn {
						for row, want := range []string{strings.TrimSpace(column.Title), values[index-numericColumn]} {
							cell := ansi.Strip(ansi.Cut(lines[row], x, x+column.Width))
							if strings.TrimSpace(cell) != want || !strings.HasSuffix(cell, want) {
								t.Fatalf("column %d row %d is not right aligned: got %q, want %q", index, row, cell, want)
							}
						}
					}
					x += column.Width + 1
				}
				if x != width || ansi.StringWidth(lines[0]) != width || ansi.StringWidth(lines[1]) != width {
					t.Fatal("header and row widths diverged")
				}
			})
		}
	}
}

func TestConnectionTablePrioritizesHostAndPreservesOutbound(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	c := fixtureConnection("long", "abcdefghijklmnopqrstuvw.xy", 0)
	c.Chains = []string{"东京出口", "中间选择组", "入口策略组"}
	lines := m.connectionTable([]mihomo.Connection{c}, 95).lines
	plain := ansi.Strip(lines[1])
	if !strings.Contains(plain, connectionDestination(c)) || !strings.Contains(plain, "… → 东京出口") {
		t.Fatalf("host or final outbound was clipped before intermediate groups: %q", plain)
	}
	if got := connectionChain(c); got != "入口策略组 → 中间选择组 → 东京出口" {
		t.Fatal("table shortening changed the complete routing path", got)
	}
	for _, width := range []int{1, 2, 8, 12, 20} {
		value := connectionChainCell(c, width)
		if ansi.StringWidth(value) > width {
			t.Fatalf("wide outbound escaped %d cells: %q", width, value)
		}
		if width >= 8 && !strings.Contains(value, "东京出口") {
			t.Fatalf("outbound was lost although its name fits: %q", value)
		}
	}
}

func TestConnectionsClickDisplayedHeaderSortsFilteredRowsInBothDirections(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			m.locale, m.noColor = i18n.New(language), true
			m.runtime.snapshot.Connections.Connections[0].Chains = []string{"Zulu"}
			m.runtime.snapshot.Connections.Connections[1].Chains = []string{"Alpha"}
			m.runtime.snapshot.Connections.Connections = append(m.runtime.snapshot.Connections.Connections,
				fixtureConnection("hidden", "outside.invalid", 2))
			m.connections.search.SetValue("example")
			m.notification.items = nil
			m.reconcileConnections()
			m, _ = update(m, tea.WindowSizeMsg{Width: 200, Height: 40})
			selected := m.connections.selected
			search := findTarget(t, m, "conn.search")
			m, _ = click(m, search)
			chain := findTarget(t, m, "conn.sort.chain")
			for _, step := range []struct{ arrow, first string }{{"▲", "second"}, {"▼", "first"}} {
				// Click the printed label, independently of the hit region's X.
				line := ansi.Strip(strings.Split(m.View().Content, "\n")[chain.Y])
				label := strings.Index(line, m.tr("connections.chain"))
				if label < 0 {
					t.Fatalf("missing chain header in %q", line)
				}
				m, _ = update(m, tea.MouseClickMsg{X: ansi.StringWidth(line[:label]), Y: chain.Y, Button: tea.MouseLeft})
				list := m.filteredConnections()
				if m.connections.editing || m.connections.detail != nil || len(list) != 2 || list[0].ID != step.first || m.connections.selected != selected {
					t.Fatalf("click on chain %s did not sort the filtered rows and preserve selection: %+v", step.arrow, list)
				}
				if m.connections.search.Value() != "example" || findTarget(t, m, "conn.search") != search || findTarget(t, m, "conn.sort.chain") != chain {
					t.Fatal("clicking the column moved the search or column or discarded the query")
				}
				line = ansi.Strip(strings.Split(m.View().Content, "\n")[chain.Y])
				cell := ansi.Cut(line, chain.X, chain.X+chain.W)
				if !strings.Contains(cell, step.arrow) || strings.Count(line, "▲")+strings.Count(line, "▼") != 1 {
					t.Fatalf("active sort direction is unclear: %q", line)
				}
			}
			m, _ = update(m, special(tea.KeyEnter))
			if list := m.filteredConnections(); list[0].ID != "second" || m.connections.detail != nil {
				t.Fatal("Enter on the focused column did not toggle its order")
			}
			m, _ = update(m, key("S"))
			if list := m.filteredConnections(); list[0].ID != "first" {
				t.Fatal("S did not reverse the selected column")
			}
			m, _ = update(m, key("/"))
			m, _ = update(m, key("S"))
			if m.connections.search.Value() != "exampleS" {
				t.Fatal("the sort shortcut consumed text in the search field")
			}
		})
	}
}

func TestConnectionsSortFocusAndDirectionSurviveResponsiveLayouts(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m.notification.items = nil
	m, _ = update(m, tea.WindowSizeMsg{Width: 200, Height: 40})
	total := findTarget(t, m, "conn.sort.download_total")
	m, _ = click(m, total)
	m, _ = click(m, total)
	for _, width := range []int{120, 60, 200} {
		m, _ = update(m, tea.WindowSizeMsg{Width: width, Height: 32})
		if !hasAction(m.targets, m.focus) || m.connections.sort != connectionSortDownloadTotal || !m.connections.sortReversed {
			t.Fatalf("resizing to %d lost actionable focus or the intended sort direction: focus=%q", width, m.focus)
		}
	}
	if m.focus != total.ID {
		t.Fatalf("returning from compact sort did not focus its visible header: %q", m.focus)
	}
}

func TestConnectionsSortTargetsFollowVisibleColumnsAndHistory(t *testing.T) {
	m, _ := archiveFixture(t, &fakeBackend{})
	for _, closed := range []bool{false, true} {
		if closed {
			m.connectionActivate("conn.closed")
		}
		for _, width := range []int{200, 160, 120, 60, 200} {
			m, _ = update(m, tea.WindowSizeMsg{Width: width, Height: 32})
			bodyWidth := dashboardGeometry(m.dashboardOptions()).width
			for _, field := range []struct {
				id         string
				minWidth   int
				activeOnly bool
			}{
				{"destination", 85, false}, {"protocol", 85, false}, {"chain", 85, false}, {"duration", 85, false},
				{"download_rate", 85, true}, {"upload_rate", 85, true}, {"process", 125, false}, {"rule", 125, false},
				{"download_total", 155, false}, {"upload_total", 155, false},
			} {
				want := bodyWidth >= field.minWidth && !(closed && field.activeOnly)
				if hasAction(m.targets, "conn.sort."+field.id) != want {
					t.Fatalf("closed=%t width=%d: sortable header %s available, want %t", closed, bodyWidth, field.id, want)
				}
			}
			if hasAction(m.targets, "conn.sort") != (bodyWidth < connectionTableMinWidth) {
				t.Fatalf("width %d: compact sort control does not match card layout", bodyWidth)
			}
		}
	}
}
