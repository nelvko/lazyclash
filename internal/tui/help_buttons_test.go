package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestHelpSwitchRetainsEveryVisibleCellAtSmallWidths(t *testing.T) {
	for _, tc := range []struct {
		width int
		face  string
	}{
		{3, " ON"}, {5, " ON  "}, {6, "      "}, {7, "      "}, {8, "      "},
	} {
		m := Model{width: tc.width, height: 5, helpOpen: true, switchStyle: app.SwitchStyleSlider, colorProfile: colorprofile.TrueColor}
		screen, hits := m.renderHelp("")
		hit := findTarget(t, Model{targets: hits}, "key-hints.toggle")
		face := ansi.Strip(ansi.Cut(strings.Split(screen, "\n")[hit.Y], hit.X, hit.X+hit.W))
		if face != tc.face || hit.X != switchGutterWidth(tc.width) {
			t.Fatalf("width %d: help switch %+v shows %q, want %q", tc.width, hit, face, tc.face)
		}
	}
}

func TestHelpButtonsKeepTheirHitBoundsWhenFocusMoves(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{120, 32}, {60, 20}, {26, 8}, {20, 7}} {
			for _, plain := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%dx%d/plain=%v", language, size[0], size[1], plain), func(t *testing.T) {
					m := Model{locale: i18n.New(language), page: "settings", width: size[0], height: size[1], helpOpen: true, noColor: plain}
					m.keyHints.helpFocus = "help.close"
					_, before := m.renderHelp("")
					m.helpKey(special(tea.KeyTab))
					if m.keyHints.helpFocus != "key-hints.toggle" {
						t.Fatal("Tab did not move to the shortcut hint control")
					}
					screen, after := m.renderHelp("")
					if len(before) != len(after) {
						t.Fatal("focusing a help button changed the available controls")
					}
					for index, target := range after {
						if target != before[index] {
							t.Fatalf("focus moved the hit bounds: before=%+v after=%+v", before[index], target)
						}
						painted := ansi.Strip(ansi.Cut(strings.Split(screen, "\n")[target.Y], target.X, target.X+target.W))
						if target.ID == "key-hints.toggle" {
							if target.W != switchSlotWidth || painted != "▰▰●   " {
								t.Fatalf("shortcut Switch hit misses its slot: %q %+v", painted, target)
							}
							continue
						}
						marker := "  "
						if target.ID == m.keyHints.helpFocus {
							marker = " ›"
						}
						if !strings.HasPrefix(painted, marker) || ansi.StringWidth(painted) != target.W {
							t.Fatalf("button hit does not cover its fixed focus gutter: %q %+v", painted, target)
						}
					}
					m.targets = after
					close := after[0]
					m.helpMouse(tea.MouseClickMsg{X: close.X + close.W - 1, Y: close.Y, Button: tea.MouseLeft})
					if m.helpOpen {
						t.Fatal("the last painted close-button cell did not activate Close")
					}
				})
			}
		}
	}
}
