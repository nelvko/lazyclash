package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestNotificationSuccessPaintsOneSurfaceThroughItsControls(t *testing.T) {
	for _, compact := range []bool{false, true} {
		item := notificationItem{ID: 7, Kind: "done", Message: "Saved"}
		o := dashboardOptions{Locale: i18n.New(i18n.English), Focus: "notification.close.7"}
		card := notificationCard(item, o, 34, 4, compact)
		screen := uv.NewScreenBuffer(card.Bounds.W, card.Bounds.H)
		uv.NewStyledString(card.Content).Draw(screen, screen.Bounds())
		for y := range card.Bounds.H {
			for x := range card.Bounds.W {
				cell := screen.CellAt(x, y)
				// Compare the actual painted cells, including padding between
				// nested styled spans and the focused close button.
				color := noticeText
				switch cell.Content {
				case "✓":
					color = noticeCheck
				case "›":
					color = focus
				case " ":
					color = "" // Only the surface is visible here.
				}
				want := uv.Style{Fg: cell.Style.Fg, Bg: lipgloss.Color(noticeSlate)}
				if color != "" {
					want.Fg = lipgloss.Color(color)
				}
				if !cell.Style.Equal(&want) {
					t.Fatalf("compact=%t: cell %d,%d (%q) has style %+v, want %+v", compact, x, y, cell.Content, cell.Style, want)
				}
			}
		}
	}
}

func TestNotificationButtonsKeepTheirBoundsAndMessageWhenFocused(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{16, 20, 32, 54} {
			for _, plain := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/plain=%v", language, width, plain), func(t *testing.T) {
					o := dashboardOptions{Locale: i18n.New(language), Busy: true, PlainButtons: plain}
					item := notificationItem{ID: 7, Kind: "working", Message: "Saved.", Details: "Full details"}
					before := notificationCard(item, o, width, 5, width < 54)
					for _, target := range before.Targets {
						o.Focus, o.Hover = target.ID, target.ID
						after := notificationCard(item, o, width, 5, width < 54)
						if after.Bounds != before.Bounds || len(after.Targets) != len(before.Targets) {
							t.Fatal("focusing a toast action changed its geometry")
						}
						for index, hit := range after.Targets {
							if hit != before.Targets[index] {
								t.Fatalf("toast control moved after focus: before=%+v after=%+v", before.Targets[index], hit)
							}
							for _, other := range after.Targets[index+1:] {
								if regionsOverlap(hit, other) {
									t.Fatalf("toast buttons overlap: %+v %+v", hit, other)
								}
							}
							painted := ansi.Strip(ansi.Cut(strings.Split(after.Content, "\n")[hit.Y], hit.X, hit.X+hit.W))
							marker := "  "
							if hit.ID == target.ID {
								marker = " ›"
							}
							face := strings.TrimPrefix(painted, marker)
							padded := strings.HasSuffix(face, " ")
							if plain && o.Hover == hit.ID && (strings.Contains(face, "×") || strings.Contains(face, "⋯")) {
								padded = face == "[×]" || face == "[⋯]"
							}
							if !strings.HasPrefix(painted, marker) || !padded || strings.TrimSpace(face) == "" {
								t.Fatalf("toast button hit lost its marker, label, or padding: %q", painted)
							}
						}
						if !strings.Contains(ansi.Strip(after.Content), item.Message) {
							t.Fatal("toast controls displaced a short complete message")
						}
						assertCanvas(t, after.Content, after.Bounds.W, after.Bounds.H)
						assertRegions(t, after.Targets, after.Bounds.W, after.Bounds.H)
					}
				})
			}
		}
	}
}

func TestNotificationDetailsUsesIconHoverOnlyForCompactLabel(t *testing.T) {
	item := notificationItem{ID: 1, Kind: "done", Message: strings.Repeat("Long message. ", 16)}
	for _, tc := range []struct {
		width int
		want  string
	}{
		{54, "Details"},
		{16, "[⋯]"},
	} {
		o := dashboardOptions{Locale: i18n.New(i18n.English), PlainButtons: true, Hover: "notification.details.1"}
		card := notificationCard(item, o, tc.width, 2, true)
		var face string
		for _, hit := range card.Targets {
			if hit.ID == o.Hover {
				row := strings.Split(card.Content, "\n")[hit.Y]
				face = strings.TrimSpace(ansi.Strip(ansi.Cut(row, hit.X, hit.X+hit.W)))
			}
		}
		if face != tc.want {
			t.Fatalf("hovered Details at width %d rendered %q, want %q", tc.width, face, tc.want)
		}
	}
}

func TestNotificationPanelTabMovesMarkerWithoutMovingButtons(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{80, 24}, {32, 12}} {
			for _, plain := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%dx%d/plain=%v", language, size[0], size[1], plain), func(t *testing.T) {
					items := []notificationItem{{ID: 1, Kind: "done", Message: "Saved.", Details: strings.Repeat("Diagnostic line.\n", 24)}}
					var panel notificationPanel
					panel.show(0)
					_, before := panel.render(items, size[0], size[1], i18n.New(language), plain)
					panel.key(special(tea.KeyTab))
					if panel.actions[panel.focus].ID != "notification.panel.dismiss.1" {
						t.Fatal("Tab did not move to Close")
					}
					screen, after := panel.render(items, size[0], size[1], i18n.New(language), plain)
					for index, target := range after {
						if target != before[index] {
							t.Fatalf("notification button moved after Tab: before=%+v after=%+v", before[index], target)
						}
						x, y := target.X-panel.bounds.X, target.Y-panel.bounds.Y
						painted := ansi.Strip(ansi.Cut(strings.Split(screen, "\n")[y], x, x+target.W))
						if target.ID == "notification.panel.dismiss.1" && !strings.HasPrefix(painted, " › ") {
							t.Fatalf("focused Close button lost its separate gutter: %q", painted)
						}
						if action := panel.mouse(tea.MouseClickMsg{X: target.X + target.W - 1, Y: target.Y, Button: tea.MouseLeft}); action != target.ID {
							t.Fatalf("button's final painted cell did not activate %s: %q", target.ID, action)
						}
					}
				})
			}
		}
	}
}
