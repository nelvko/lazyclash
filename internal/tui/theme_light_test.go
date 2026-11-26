package tui

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func themeViewCells(m Model) uv.ScreenBuffer {
	screen := uv.NewScreenBuffer(m.width, m.height)
	uv.NewStyledString(m.View().Content).Draw(screen, screen.Bounds())
	return screen
}

func expectThemeColor(t *testing.T, label string, got color.Color, want string) {
	t.Helper()
	if got == nil || color.RGBAModel.Convert(got) != color.RGBAModel.Convert(lipgloss.Color(want)) {
		t.Errorf("%s = %v, want %s", label, got, want)
	}
}

func TestLightThemePaintsEveryPageWithoutMovingControls(t *testing.T) {
	for _, page := range navPages() {
		t.Run(page, func(t *testing.T) {
			m, _ := proxiesModel(t, 140, 38)
			m.theme = app.ThemeAuto
			m.navigate(page)
			m.layout()
			before, focus, targets := ansi.Strip(m.View().Content), m.focus, slices.Clone(m.targets)
			m, _ = update(m, tea.BackgroundColorMsg{Color: color.White})
			// Auto's explanatory label on Settings reflects the new resolved theme.
			after := ansi.Strip(m.View().Content)
			if page != "settings" && after != before {
				t.Fatal("background response changed the page text")
			}
			if m.focus != focus || !slices.Equal(targets, m.targets) {
				t.Fatal("background response moved the focus or hit regions")
			}
			screen := themeViewCells(m)
			nav := findTarget(t, m, "nav."+page)
			expectThemeColor(t, "sidebar", screen.CellAt(0, 0).Style.Bg, "#e0e6eb")
			expectThemeColor(t, "current navigation row", screen.CellAt(nav.X, nav.Y).Style.Bg, "#eef2f5")
			expectThemeColor(t, "body gutter", screen.CellAt(nav.X+nav.W, nav.Y).Style.Bg, "#eef2f5")
			if page == "proxies" {
				selected := findTarget(t, m, "node.0.0")
				row := selected.Y + selected.H/2
				expectThemeColor(t, "selected node", screen.CellAt(selected.X+2, row).Style.Bg, "#d0e1ed")
			}
		})
	}
}

func TestLightThemeProxyCardsKeepMetadataAndDelayColors(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.theme = app.ThemeLight
	m.snapshot.Groups[0].Now = "HK 01"
	m.snapshot.Proxies["HK 02"].History[0].Delay = 250
	m.hover = "node.0.2"
	m.layout()
	screen := themeViewCells(m)
	for _, tc := range []struct{ index, protocol, delayColor, background string }{
		{"0.1", "S", mistGreen, mistSelection},
		{"0.2", "T", mistAmber, "#d9e3eb"},
		{"0.3", "V", mistGreen, mistCard},
	} {
		card := findTarget(t, m, "node."+tc.index)
		delay := findTarget(t, m, "proxies.delay."+tc.index)
		meta := screen.CellAt(card.X+2, delay.Y+1)
		value := screen.CellAt(delay.X+delay.W-1, delay.Y)
		if meta.Content != tc.protocol || value.Content != "s" {
			t.Fatalf("node %s: expected protocol and latency cells, got %q and %q", tc.index, meta.Content, value.Content)
		}
		expectThemeColor(t, "protocol", meta.Style.Fg, mistMuted)
		expectThemeColor(t, "delay", value.Style.Fg, tc.delayColor)
		expectThemeColor(t, "card surface", value.Style.Bg, tc.background)
	}
}

func TestLightThemeRolesAndTerminalProfiles(t *testing.T) {
	// Each pair represents a component style sent through the same View as a page.
	pairs := []struct{ fg, bg, wantFg, wantBg string }{
		{text, bg, "#283746", "#eef2f5"},
		{muted, sidebarBackground, "#5e6d7c", "#e0e6eb"},
		{divider, panel, "#ccd5dd", "#e2e8ee"},
		{accent, buttonSelectedBackground, "#306a9e", "#d0e1ed"},
		{routingFocus, routingSelectedBackground, mistBlue, mistSelection},
		{success, panel, "#3d713e", "#e2e8ee"},
		{warning, panel, "#8d641a", "#e2e8ee"},
		{errorColor, panel, "#a84a4a", "#e2e8ee"},
	}
	m := Model{width: len(pairs), height: 1, theme: app.ThemeLight}
	for _, pair := range pairs {
		m.screen += themed("X", pair.fg, pair.bg, false)
	}
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
		t.Run(profile.String(), func(t *testing.T) {
			m.colorProfile = profile
			view := m.View()
			if ansi.Strip(view.Content) != strings.Repeat("X", len(pairs)) || view.BackgroundColor != nil || view.ForegroundColor != nil {
				t.Fatal("View changed content or terminal defaults")
			}
			if profile == colorprofile.ASCII {
				if strings.Contains(view.Content, "\x1b") {
					t.Fatal("ASCII retained styling")
				}
				return
			}
			cells := themeViewCells(m)
			for i, pair := range pairs {
				style := cells.CellAt(i, 0).Style
				if profile == colorprofile.TrueColor {
					expectThemeColor(t, pair.fg, style.Fg, pair.wantFg)
					expectThemeColor(t, pair.bg, style.Bg, pair.wantBg)
				} else {
					for _, c := range []color.Color{style.Fg, style.Bg} {
						_, indexed := c.(ansi.IndexedColor)
						_, basic := c.(ansi.BasicColor)
						if profile == colorprofile.ANSI256 && !indexed || profile == colorprofile.ANSI && !basic {
							t.Fatalf("role %s emitted %T for %s", pair.fg, c, profile)
						}
					}
				}
			}
			body := cells.CellAt(0, 0).Style
			if terminalContrast(body.Bg, color.Black) <= terminalContrast(body.Fg, color.Black) || terminalContrast(body.Bg, body.Fg) < 4.5 {
				t.Errorf("expected readable dark text on a light background, got %+v", body)
			}
		})
	}
}

func TestSavedLightAndAutoPaintStartupBeforeSnapshot(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	backend := &app.App{Home: t.TempDir()}
	for _, theme := range []string{app.ThemeLight, app.ThemeAuto} {
		if err := backend.SaveThemePreference(t.Context(), theme); err != nil {
			t.Fatal(err)
		}
		m := New(t.Context(), backend, "en")
		m, _ = update(m, tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
		m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 30})
		sidebar := func() color.Color {
			screen := themeViewCells(m)
			return screen.CellAt(0, 0).Style.Bg
		}
		if theme == app.ThemeAuto {
			expectThemeColor(t, "auto before terminal response", sidebar(), sidebarBackground)
		} else {
			expectThemeColor(t, "saved light before terminal response", sidebar(), "#e0e6eb")
		}
		m, _ = update(m, tea.BackgroundColorMsg{Color: color.White})
		expectThemeColor(t, "reported light", sidebar(), "#e0e6eb")
		m, _ = update(m, tea.BackgroundColorMsg{Color: color.Black})
		if theme == app.ThemeAuto {
			expectThemeColor(t, "reported dark", sidebar(), sidebarBackground)
		} else {
			expectThemeColor(t, "explicit light on dark terminal", sidebar(), "#e0e6eb")
		}
	}
}

func TestLightThemeUpdatesActiveSearchCursor(t *testing.T) {
	m, id := cursorSearchModel(t, "logs")
	m.theme, m.colorProfile, m.noColor = app.ThemeAuto, colorprofile.TrueColor, false
	m, _ = click(m, findTarget(t, m, id))
	before := m.View().Cursor
	m, _ = update(m, tea.BackgroundColorMsg{Color: color.White})
	after := m.View().Cursor
	if before == nil || after == nil || before.Position != after.Position || before.Shape != after.Shape {
		t.Fatalf("changing the theme moved the cursor: dark=%+v light=%+v", before, after)
	}
	expectThemeColor(t, "light cursor", after.Color, "#8d641a")
	m.noColor = true
	uncolored := m.View()
	if uncolored.Cursor == nil || uncolored.Cursor.Color != nil || strings.Contains(uncolored.Content, "\x1b") {
		t.Fatal("NO_COLOR must keep the input cursor visible and uncolored")
	}
}

func TestLightThemeOutlinesFloatingProviderDialog(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height int
		floating      bool
	}{{"floating", 140, 38, true}, {"full-screen", 90, 27, false}} {
		for _, theme := range []string{app.ThemeLight, app.ThemeAuto} {
			t.Run(size.name+"/"+theme, func(t *testing.T) {
				m, _ := ruleProvidersModel(t, 3)
				m.width, m.height = size.width, size.height
				m.colorProfile, m.noColor, m.theme = colorprofile.TrueColor, false, app.ThemeDark
				m.layout()
				before := ansi.Strip(m.View().Content)
				bounds, controls := m.providers.bounds, slices.Clone(m.providers.controls)
				m.theme = theme
				m, _ = update(m, tea.BackgroundColorMsg{Color: color.White})
				after := ansi.Strip(m.View().Content)
				if m.providers.bounds != bounds || !slices.Equal(m.providers.controls, controls) {
					t.Fatal("changing the theme moved the dialog or its controls")
				}
				if !size.floating {
					if bounds.W != m.width || bounds.H != m.height || before != after {
						t.Fatal("full-screen dialog gained an outline or its content moved")
					}
					return
				}
				rows := strings.Split(after, "\n")
				edge := strings.Repeat("─", bounds.W-2)
				top := ansi.Cut(rows[bounds.Y], bounds.X, bounds.X+bounds.W)
				bottom := ansi.Cut(rows[bounds.Y+bounds.H-1], bounds.X, bounds.X+bounds.W)
				if top != "┌"+edge+"┐" || bottom != "└"+edge+"┘" {
					t.Fatalf("floating light dialog has no single-line border: %q / %q", top, bottom)
				}
			})
		}
	}
}
