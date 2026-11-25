package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestConnectionDetailOverlayKeepsListInPlaceAndOwnsInput(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m.width, m.height = 160, 30
	m.colorProfile = colorprofile.TrueColor
	m.connections.search.SetValue("host-")
	for i := range 60 {
		c := fixtureConnection(fmt.Sprintf("id-%02d", i), fmt.Sprintf("host-%02d.example", i), i)
		m.runtime.snapshot.Connections.Connections = append(m.runtime.snapshot.Connections.Connections, c)
	}
	m.connections.selected, m.focus = "id-12", "connection.id-12"
	m.reconcileConnections()
	m.revealConnection(0)
	m.layout()
	row := findTarget(t, m, m.focus)
	scroll := m.scroll
	if scroll == 0 {
		t.Fatal("fixture should open the inspector from a scrolled list")
	}
	m, _ = click(m, row)
	box := m.connections.detailBounds
	if m.dashboardOptions().Dim || box.X < m.width/2 || box.Y+box.H != m.height-2 {
		t.Fatalf("wide inspector should float above an undimmed list at the lower right: %+v", box)
	}
	screenRows := strings.Split(ansi.Strip(m.screen), "\n")
	for _, y := range []int{box.Y, box.Y + box.H - 1} {
		edge := ansi.Cut(screenRows[y], box.X, box.X+box.W)
		if ansi.StringWidth(strings.TrimSpace(edge)) != box.W {
			t.Fatal("the undimmed inspector lost its outline in TrueColor")
		}
	}
	if m.hover != "" || m.pageViews["connections"].focus != "connection.id-12" {
		t.Fatal("inspector retained the hovered row or overwrote the list's remembered focus")
	}
	// The same destination is still visible at the same row on the left.
	line := screenRows[row.Y]
	if !strings.Contains(ansi.Cut(line, 0, box.X), "host-12.example") || !strings.Contains(m.screen, m.tr("connections.details")) {
		t.Fatal("opening details replaced or displaced the connection list")
	}
	for _, msg := range []tea.Msg{key("1"), key("v"), key("p"), key("s"), key("/"), key("?"), tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}, tea.PasteMsg{Content: "other"}} {
		m, _ = update(m, msg)
	}
	m, _ = update(m, tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 0, Y: 0})
	if m.focus != "conn.back" || m.connections.detail == nil || m.connections.closedView || m.connections.paused || m.helpOpen || m.scroll != scroll || m.connections.editing || m.connections.search.Value() != "host-" {
		t.Fatal("detail window leaked a shortcut, paste or wheel to the list")
	}
	m, _ = update(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: 0})
	if m.connections.detail != nil || m.page != "connections" || m.focus != "connection.id-12" || m.scroll != scroll || m.connections.search.Value() != "host-" {
		t.Fatal("clicking outside did not dismiss the window and restore the selected row")
	}
}

func TestConnectionDetailDoesNotHighlightAnotherLiveConnection(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m, _ = update(m, special(tea.KeyEnter))
	first := *m.connections.detail
	other := fixtureConnection("other", "other.example", 2)
	reused := fixtureConnection(first.ID, "replacement.example", 3)
	m.connections.selected = other.ID
	if !m.connectionRowSelected(first) || m.connectionRowSelected(other) || m.connectionRowSelected(reused) {
		t.Fatal("only the inspected connection lifetime should be highlighted behind the inspector")
	}
	m.connections.history.generation++
	if m.connectionRowSelected(first) {
		t.Fatal("a replacement core's row must not be highlighted behind the old inspector")
	}
}

func TestConnectionDetailNarrowViewportScrollsAndReflows(t *testing.T) {
	for _, lang := range []string{i18n.English, i18n.Chinese} {
		t.Run(lang, func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			m.locale = i18n.New(lang)
			m.runtime.snapshot.Connections.Connections[0].Metadata.ProcessPath = strings.Repeat("/组件-long", 15) + "/tail"
			m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 16})
			m, _ = update(m, special(tea.KeyEnter))
			box := m.connections.detailBounds
			v := m.connections.detailViewport
			if v == nil || !v.AtTop() || v.AtBottom() || box.X != (m.width-box.W)/2 || box.Y != (m.height-box.H)/2 {
				t.Fatal("narrow inspector did not open a centered scrollable viewport")
			}
			lines := strings.Join(m.connectionDetailLines(v.Width(), true), "\n")
			for _, field := range []string{"connections.network_inbound", "connections.resolved_ip", "connections.process_path"} {
				if !strings.Contains(ansi.Strip(lines), m.tr(field)) {
					t.Fatalf("narrow view truncated the %s label", field)
				}
			}
			scroll := m.scroll
			m, _ = update(m, tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: box.X + 2, Y: box.Y + 3})
			if v.AtTop() || m.scroll != scroll {
				t.Fatal("the mouse wheel must scroll just the detail fields")
			}
			m, _ = update(m, special(tea.KeyEnd))
			if !v.AtBottom() || !strings.Contains(ansi.Strip(v.View()), "tail") || !strings.Contains(ansi.Strip(v.View()), "first") {
				t.Fatal("the final ID and path component must be reachable")
			}
			m, _ = update(m, special(tea.KeyHome))
			if !v.AtTop() || !strings.Contains(ansi.Strip(v.View()), "one.example.invalid") {
				t.Fatal("Home did not return to the destination")
			}
			m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 34})
			assertCanvas(t, m.screen, m.width, m.height)
			assertRegions(t, m.targets, m.width, m.height)
			m, _ = update(m, special(tea.KeyEscape))
			if m.connections.detail != nil || m.focus != "connection.first" || m.scroll != scroll {
				t.Fatal("resizing or closing the detail changed list selection or scroll")
			}
		})
	}
}
