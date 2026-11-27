package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestDropdownLongNamesKeepCompactTriggerAndStableOptionRows(t *testing.T) {
	prefix := strings.Repeat("跨区域", 8)
	d := dropdown{Value: "short", Items: []menuItem{
		{ID: "short", Label: "Auto"},
		{ID: "a", Label: prefix + "上海"},
		{ID: "b", Label: prefix + "南京"},
	}}
	_, trigger := d.render("group", buttonOptions{}, false, 0)
	trigger.X, trigger.Y = 50, 2
	var m actionMenu
	d.show(&m, trigger)
	full := ansi.Strip(m.render("", 160, 24, 23, true))
	if trigger.W != 48+dropdownGutter || m.bounds.W != ansi.StringWidth(d.Items[2].Label)+6 ||
		!strings.Contains(full, d.Items[1].Label) || !strings.Contains(full, d.Items[2].Label) {
		t.Fatalf("wide popup did not reveal both full names independently of trigger width: %+v / %+v", trigger, m.bounds)
	}
	for _, y := range []int{2, 20} {
		trigger.X, trigger.Y = 3, y
		m.setAnchor(trigger)
		m.key(special(tea.KeyHome))
		m.render("", 29, 23, 22, true)
		firstY := m.optionY
		for i := 1; i < len(d.Items); i++ {
			target := tea.MouseClickMsg{X: m.bounds.X + 2, Y: firstY + i, Button: tea.MouseLeft}
			m.mouse(tea.MouseMotionMsg(target))
			view := ansi.Strip(m.render("", 29, 23, 22, true))
			if m.optionY != firstY || m.optionCount != len(d.Items) || m.selected != i ||
				!strings.Contains(view, d.Items[i].Label[len(prefix):]) || strings.Join(m.detail.lines, "") != d.Items[i].Label {
				t.Fatalf("hover failed to display the selected full label or moved its target at y=%d:\n%s", y, view)
			}
			copy := m
			if result := copy.mouse(target); result.Action != d.Items[i].ID {
				t.Fatalf("hover and click at same coordinates chose different items: %+v", result)
			}
			copy = m
			target.Y = m.detail.y + len(m.detail.lines) - 1
			if result := copy.mouse(target); result.Action != d.Items[i].ID {
				t.Fatalf("click on last line of the selected name chose %+v", result)
			}
		}
	}
}

func TestDropdownPagesThroughOversizedNameAndRewrapsOnResize(t *testing.T) {
	label := strings.Repeat("华东🇨🇳e\u0301", 80) + "唯一末尾"
	d := dropdown{Value: "long", Items: []menuItem{{ID: "long", Label: label}, {ID: "short", Label: "Direct"}}}
	for _, height := range []int{12, 2} {
		var m actionMenu
		d.show(&m, hitRegion{ID: "field", X: 4, Y: 0, W: 12, H: 1})
		m.render("", 24, height, height, true)
		at := 0
		var read strings.Builder
		for {
			// The final page can overlap the previous one when it is shorter.
			for i, line := range m.detail.lines {
				if m.detail.offset+i == at {
					read.WriteString(line)
					at++
				}
			}
			if m.detail.offset == m.detail.maxOffset {
				break
			}
			before := m.detail.offset
			m.key(special(tea.KeyPgDown))
			m.render("", 24, height, height, true)
			if m.detail.offset <= before {
				t.Fatal("PgDown stopped before the end of the name")
			}
		}
		if read.String() != label || m.selected != 0 || !m.open {
			t.Fatalf("paging lost a character or changed the selection at %d rows", height)
		}
		before := m.detail.offset
		m.mouse(tea.MouseWheelMsg{X: m.bounds.X + 1, Y: m.detail.y, Button: tea.MouseWheelUp})
		m.render("", 24, height, height, true)
		if m.detail.offset != before-1 || m.selected != 0 {
			t.Fatal("wheel over the reading area moved the option or skipped a line")
		}
		m.key(special(tea.KeyPgUp))
		m.render("", 160, 45, 44, true)
		m.key(special(tea.KeyDown))
		m.render("", 24, height, height, true)
		if len(m.detail.lines) != 0 || m.key(special(tea.KeyEnter)).Action != "short" {
			t.Fatal("resizing retained a stale reading pane or committed the wrong option")
		}
	}
}
