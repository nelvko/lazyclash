package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func selectMenuItems() []menuItem {
	return []menuItem{{ID: "all", Label: "All"}, {ID: "info", Label: "Info"}, {ID: "warn", Label: "Warn"}, {ID: "error", Label: "Error"}}
}

func TestSelectMenuKeepsCurrentCheckIndependentFromPendingChoice(t *testing.T) {
	items := selectMenuItems()
	var menu actionMenu
	(dropdown{Items: items, Value: "info"}).show(&menu, hitRegion{X: 10, Y: 4, W: 8, H: 1})
	if menu.selected != 1 || !menu.items[1].Checked || items[1].Checked {
		t.Fatal("selection did not focus and check a copied current item")
	}
	menu.key(special(tea.KeyDown))
	for _, mono := range []bool{false, true} {
		view := menu.render("", 80, 24, 23, mono)
		cursor := " "
		if mono {
			cursor = "›"
		}
		if menu.selected != 2 || !strings.Contains(view, themed(" Info  ✓ ", text, raised, false)) ||
			!strings.Contains(view, themed(cursor+"Warn    ", text, selectionBackground, false)) {
			t.Fatalf("moving the cursor changed the current value or lost pending focus:\n%s", view)
		}
	}
	if result := menu.key(special(tea.KeyEnter)); result.Action != "warn" || !result.Closed || menu.open {
		t.Fatalf("confirming did not return the pending value: %+v", result)
	}
}

func TestDropdownTriggerAndPopupShareGeometry(t *testing.T) {
	d := dropdown{Items: languageMenuItems}
	for _, language := range languageMenuItems {
		d.Value = language.ID
		for _, expanded := range []bool{false, true} {
			value, hit := d.render("language", buttonOptions{Focused: true, Surface: panel}, expanded, 0)
			plain := ansi.Strip(value)
			if hit.W != 16 || ansi.StringWidth(plain) != hit.W || !strings.Contains(plain, language.Label) ||
				strings.Contains(plain, "›") == expanded || strings.Contains(plain, "▴") != expanded {
				t.Fatalf("unexpected trigger for %s, expanded=%v: %q", language.ID, expanded, plain)
			}
			x, w := d.popupGeometry(hit, 80)
			if x != 2 || w != 14 || x+w != hit.X+hit.W {
				t.Fatalf("popup [%d,%d) and trigger face differ: %+v", x, x+w, hit)
			}
		}
	}
	for _, right := range []bool{false, true} {
		hit := hitRegion{X: 24, W: 8, DropdownRight: right}
		x, w := d.popupGeometry(hit, 80)
		if right && x+w != hit.X+hit.W || !right && x != hit.X+dropdownGutter {
			t.Fatalf("popup missed clipped trigger: right=%v x=%d w=%d", right, x, w)
		}
	}
}

func TestSelectMenuCancellationAndPointerMovementDoNotCommit(t *testing.T) {
	for _, close := range []string{"esc", "tab", "outside"} {
		t.Run(close, func(t *testing.T) {
			var menu actionMenu
			(dropdown{Items: selectMenuItems(), Value: "info"}).show(&menu, hitRegion{X: 10, Y: 4, W: 8, H: 1})
			menu.render("", 80, 24, 23, false)
			pointer := menuMouse(menu, 3, tea.MouseLeft)
			for _, event := range []tea.MouseMsg{tea.MouseMotionMsg(pointer), tea.MouseReleaseMsg(pointer), tea.MouseWheelMsg{X: pointer.X, Y: pointer.Y, Button: tea.MouseWheelUp}} {
				if result := menu.mouse(event); result != (menuResult{}) || !menu.open || !menu.items[1].Checked || menu.items[menu.selected].Checked {
					t.Fatalf("pointer navigation committed a pending choice: %+v", result)
				}
			}
			var result menuResult
			if close == "outside" {
				result = menu.mouse(tea.MouseClickMsg{X: 79, Y: 23, Button: tea.MouseLeft})
			} else {
				result = menu.key(key(close))
			}
			if result.Action != "" || !result.Closed || menu.open || !menu.items[1].Checked {
				t.Fatalf("cancel committed or changed the current value: %+v", result)
			}
		})
	}
}

func TestSelectMenuAnchorsBelowOrAboveWithoutMovingPage(t *testing.T) {
	const width, height = 80, 24
	footer := cellPad("? Help · q Quit", width)
	base := strings.Repeat(strings.Repeat(" ", width)+"\n", height-1) + footer
	for _, anchor := range []hitRegion{{X: 10, Y: 4, W: 8, H: 1}, {X: 72, Y: 20, W: 8, H: 1}} {
		var menu actionMenu
		(dropdown{Items: selectMenuItems(), Value: "error"}).show(&menu, anchor)
		view := menu.render(base, width, height, height-1, false)
		assertCanvas(t, view, width, height)
		if anchor.Y < 10 && menu.bounds.Y != anchor.Y+anchor.H {
			t.Fatalf("menu did not open below its trigger: %+v", menu.bounds)
		}
		if anchor.Y > 10 && menu.bounds.Y+menu.bounds.H > anchor.Y {
			t.Fatalf("menu did not flip above the low trigger: %+v", menu.bounds)
		}
		if got := strings.Split(view, "\n")[height-1]; got != footer {
			t.Fatal("selection popup changed the footer")
		}
		for y, row := range strings.Split(view, "\n") {
			if y < menu.bounds.Y || y >= menu.bounds.Y+menu.bounds.H {
				if row != strings.Split(base, "\n")[y] {
					t.Fatalf("popup changed page row %d outside its bounds", y)
				}
			}
		}
	}
}

func TestSelectMenuResizeKeepsCurrentAndCursorVisible(t *testing.T) {
	var menu actionMenu
	(dropdown{Items: selectMenuItems(), Value: "info"}).show(&menu, hitRegion{X: 70, Y: 20, W: 8, H: 1})
	menu.key(special(tea.KeyEnd))
	for _, size := range [][2]int{{80, 24}, {40, 16}, {24, 7}, {8, 4}, {3, 2}, {1, 1}, {80, 24}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			menu.setAnchor(hitRegion{X: size[0] - 8, Y: size[1] / 2, W: 8, H: 1})
			view := menu.render("", size[0], size[1], size[1], false)
			assertCanvas(t, view, size[0], size[1])
			bounds := menu.bounds
			if bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > size[0] || bounds.Y+bounds.H > size[1] {
				t.Fatalf("resized menu left the terminal: %+v", bounds)
			}
			rows := bounds.H
			if menu.framed {
				rows -= 2
			}
			if menu.selected != 3 || !menu.items[1].Checked || menu.selected < menu.offset || menu.selected >= menu.offset+rows {
				t.Fatalf("resize lost current or pending choice: selected=%d offset=%d rows=%d", menu.selected, menu.offset, rows)
			}
			if size[0] >= 8 && !strings.Contains(ansi.Strip(view), "Err") {
				t.Fatalf("resize hid the pending last value:\n%s", ansi.Strip(view))
			}
			click := menuMouse(menu, menu.selected-menu.offset, tea.MouseLeft)
			copy := menu
			if result := copy.mouse(click); result.Action != "error" || !result.Closed {
				t.Fatalf("resized pending value was not clickable: %+v", result)
			}
		})
	}
}

func TestSelectMenuUnavailableCurrentFallsBackToAvailableCursor(t *testing.T) {
	items := selectMenuItems()
	items[1].DisabledReason = "Unavailable"
	var menu actionMenu
	(dropdown{Items: items, Value: "info"}).show(&menu, hitRegion{H: 1})
	if menu.selected != 0 || !menu.items[1].Checked {
		t.Fatal("disabled current value became actionable or lost its check")
	}
	(dropdown{Items: nil, Value: "info"}).show(&menu, hitRegion{H: 1})
	if menu.open {
		t.Fatal("empty selection menu opened")
	}
}
