package tui

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestInputFieldKeepsTextClearAndControlsIndependent(t *testing.T) {
	for _, width := range []int{60, 40, 24, 12, 5, 1} {
		for _, value := range []string{"", "中文 github"} {
			t.Run(fmt.Sprint(width, "/", value), func(t *testing.T) {
				input := textinput.New()
				input.SetValue(value)
				controls := dashboardBlock{lines: []string{"Aa ab .*"}, targets: []hitRegion{{ID: "case", W: 2, H: 1}}}
				field := renderInputField(input, inputFieldOptions{ID: "input", ClearID: "clear", Width: width, Surface: raised, Trailing: controls})
				if len(field.lines) != inputFieldRows(width, 8, true) {
					t.Fatalf("unexpected frame height: %v", field.lines)
				}
				for _, line := range field.lines {
					if ansi.StringWidth(line) > width {
						t.Fatalf("frame overflows %d cells: %q", width, ansi.Strip(line))
					}
				}
				var frame, clear, option hitRegion
				for _, target := range field.targets {
					switch target.ID {
					case "input":
						frame = target
					case "clear":
						clear = target
					case "case":
						option = target
					}
				}
				if frame.W != width || frame.H != 3 || frame.InputWidth != inputFieldTextWidth(width, 8, true) && width >= 5 {
					t.Fatalf("incorrect editor geometry: %+v", frame)
				}
				if want := value != "" && width >= 12; (clear.ID != "") != want {
					t.Fatalf("clear visibility: %+v, want %t", clear, want)
				}
				if clear.ID != "" && clear.X <= frame.X+2+frame.InputWidth {
					t.Fatalf("clear overlaps editable viewport: %+v / %+v", clear, frame)
				}
				if option.ID != "" && option.Y == 1 && clear.ID != "" && option.X < clear.X+clear.W+2 {
					t.Fatalf("option overlaps the clear action or separator on its left: %+v / %+v", option, clear)
				}
			})
		}
	}
}

// Clear follows the editor before inline search options. Ordinary fields and
// searches with wrapped options retain a one-cell inset from the right border.
func TestInputFieldKeepsClearBeforeOptionsAndGenericRightInset(t *testing.T) {
	for _, width := range []int{12, 24, 40, 60} {
		for _, withControls := range []bool{false, true} {
			t.Run(fmt.Sprint(width, "/controls=", withControls), func(t *testing.T) {
				input := textinput.New()
				input.SetValue("needle")
				options := inputFieldOptions{ID: "input", ClearID: "clear", Width: width, Surface: bg}
				if withControls {
					options.Trailing = dashboardBlock{lines: []string{" Aa ab .*"}, targets: []hitRegion{{ID: "option", W: 3, H: 1}}}
				}
				var baseline []hitRegion
				for _, state := range []string{"idle", "input-focus", "clear-focus", "hover"} {
					options.Focused, options.ClearFocused, options.ClearHovered = state == "input-focus", state == "clear-focus", state == "hover"
					field := renderInputField(input, options)
					if baseline == nil {
						baseline = field.targets
					} else if !reflect.DeepEqual(field.targets, baseline) {
						t.Fatalf("%s moved field controls: got %+v want %+v", state, field.targets, baseline)
					}
					inline := withControls && len(field.lines) == 3
					for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ASCII} {
						line := ansi.Strip(terminalTheme(field.lines[1], profile))
						if inline {
							if !strings.Contains(line, "✖ │ Aa ab .*") {
								t.Fatalf("%s/%s clear no longer precedes matching options: %q", state, profile, line)
							}
						} else if got := ansi.Cut(line, width-3, width); got != "✖ │" {
							t.Fatalf("%s/%s ordinary clear icon lost its one-cell inset: %q", state, profile, line)
						}
					}
					for _, hit := range field.targets {
						if hit.ID != "clear" {
							continue
						}
						wantX := width - 4
						if inline {
							wantX -= 2 + 9 // separator and matching options follow clear.
						}
						if hit.X != wantX || hit.W != 2 || hit.Y != 1 {
							t.Fatalf("clear action moved from its reserved slot: %+v, want x=%d", hit, wantX)
						}
					}
				}
			})
		}
	}
}

func TestInputFieldEmptyQueryKeepsOptionsStillAndHidesSeparator(t *testing.T) {
	input := textinput.New()
	options := inputFieldOptions{ID: "input", ClearID: "clear", Width: 40,
		Trailing: dashboardBlock{lines: []string{" Aa ab .*"}, targets: []hitRegion{{ID: "option", W: 3, H: 1}}}}
	empty := renderInputField(input, options)
	input.SetValue("needle")
	populated := renderInputField(input, options)
	var emptyOption, populatedOption, emptyFrame, populatedFrame hitRegion
	for _, hit := range empty.targets {
		switch hit.ID {
		case "clear":
			t.Fatal("empty input exposed a clear action")
		case "option":
			emptyOption = hit
		case "input":
			emptyFrame = hit
		}
	}
	for _, hit := range populated.targets {
		switch hit.ID {
		case "option":
			populatedOption = hit
		case "input":
			populatedFrame = hit
		}
	}
	if emptyOption.ID == "" || emptyOption != populatedOption || emptyFrame != populatedFrame {
		t.Fatal("showing the clear action moved the editor or matching options")
	}
	emptyLine, populatedLine := ansi.Strip(empty.lines[1]), ansi.Strip(populated.lines[1])
	if strings.Count(emptyLine, "│") != 2 || strings.Count(populatedLine, "│") != 3 {
		t.Fatalf("separator should appear only beside a visible clear action: empty=%q populated=%q", emptyLine, populatedLine)
	}
	if !strings.HasSuffix(populatedLine, "✖ │ Aa ab .* │") {
		t.Fatalf("clear, separator and matching options are out of order: %q", populatedLine)
	}
}

func TestInputFieldSharesSurfaceAndPreservesMaskedEditor(t *testing.T) {
	input := textinput.New()
	input.EchoMode, input.EchoCharacter = textinput.EchoPassword, '•'
	input.SetValue("private-value")
	input.Focus()
	field := renderInputField(input, inputFieldOptions{ID: "input", ClearID: "clear", Width: 40, Surface: raised})
	view := strings.Join(field.lines, "\n")
	if strings.Contains(ansi.Strip(view), "private-value") || !strings.Contains(ansi.Strip(view), "••••") {
		t.Fatal("shared renderer changed Bubbles password echo")
	}
	if input.Value() != "private-value" || !input.Focused() {
		t.Fatal("renderer changed editor state")
	}
	// Verify each printable cell, including padding, uses the requested modal
	// surface; nested text spans must not restore the former input fill.
	background := regexp.MustCompile(`48;2;[0-9]+;[0-9]+;[0-9]+`)
	want := background.FindString(ansi.NewStyle().BackgroundColor(lipgloss.Color(raised)).String())
	colors := background.FindAllString(view, -1)
	if want == "" || len(colors) == 0 {
		t.Fatal("fixture did not emit surface colors")
	}
	for _, got := range colors {
		if got != want {
			t.Fatalf("input surface %s differs from modal %s", got, want)
		}
	}
}

func TestInputFieldClippingRemovesHardwareCursorGeometry(t *testing.T) {
	field := hitRegion{ID: "input", X: 4, Y: 3, W: 30, H: 3, InputWidth: 22}
	for _, clip := range [][4]int{{5, 0, 40, 20}, {0, 4, 40, 20}, {0, 0, 30, 20}, {0, 0, 40, 5}} {
		got, ok := clipRegion(field, clip[0], clip[1], clip[2], clip[3])
		if !ok || got.InputWidth != 0 {
			t.Fatalf("partial frame retained cursor geometry: %+v", got)
		}
	}
	got, ok := clipRegion(field, 0, 0, 40, 20)
	if !ok || got.InputWidth != field.InputWidth {
		t.Fatal("complete frame lost cursor geometry")
	}
}

func TestInputFieldCursorAcrossAllEditorsAndResize(t *testing.T) {
	for _, scenario := range []string{"profile-url", "generic-form", "masked-form", "create-name", "create-description", "create-source", "ports-mixed", "ports-controller", "wizard-local", "wizard-version", "wizard-prefix", "inline-local", "inline-version"} {
		t.Run(scenario, func(t *testing.T) {
			m, id, _ := inputFieldsScenario(t, scenario, "zh-Hans")
			fixture := inputFieldsPTY{model: m, scenario: scenario}
			input := fixture.input()
			input.CharLimit = 8192
			input.SetValue(strings.Repeat("中文abcdefgh", 15) + "VISIBLE-TAIL")
			input.CursorEnd()
			input.SetCursor(input.Position() - 3)
			m = fixture.model
			for _, size := range [][2]int{{160, 45}, {80, 24}, {60, 20}, {40, 16}, {160, 45}} {
				m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				field := findTarget(t, m, id)
				cursor := m.View().Cursor
				if field.InputWidth <= 0 || cursor == nil || cursor.Y != field.Y+1 || cursor.X < field.X+2 || cursor.X > field.X+2+field.InputWidth {
					t.Fatalf("cursor outside editor at %v: field=%+v cursor=%+v", size, field, cursor)
				}
				if scenario != "masked-form" {
					assertSearchCursorCell(t, m, "A")
				}
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
			}
		})
	}
}
