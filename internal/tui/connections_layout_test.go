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
				lines := m.connectionTable([]mihomo.Connection{c}, width)
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
	lines := m.connectionTable([]mihomo.Connection{c}, 95)
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
