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
	for _, width := range []int{3, 5, 6, 7, 8} {
		for _, style := range []string{"", app.SwitchStyleSlanted, app.SwitchStyleSlider} {
			for _, state := range []struct {
				hidden         bool
				label, slanted string
			}{{true, "OFF", slantedFrames[0]}, {false, " ON", slantedFrames[2]}} {
				m := Model{width: width, height: 5, helpOpen: true, switchStyle: style, colorProfile: colorprofile.TrueColor}
				m.keyHints.hidden = state.hidden
				screen, hits := m.renderHelp("")
				hit := findTarget(t, Model{targets: hits}, "key-hints.toggle")
				face := ansi.Strip(ansi.Cut(strings.Split(screen, "\n")[hit.Y], hit.X, hit.X+hit.W))
				want := state.slanted
				if width < switchSlotWidth {
					want = cellPad(state.label, width)
				} else if style == app.SwitchStyleSlider {
					want = "      "
				}
				if face != want || hit.X != switchGutterWidth(width) || hit.W != ansi.StringWidth(want) {
					t.Fatalf("%q at width %d: help switch %+v shows %q, want %q", style, width, hit, face, want)
				}
			}
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
							if target.W != switchSlotWidth || painted != slantedFrames[2] {
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
