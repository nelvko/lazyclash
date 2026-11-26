package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestTooltipSurfacePlacement(t *testing.T) {
	m := Model{width: 20, height: 9, colorProfile: colorprofile.ANSI}
	m.screen = strings.TrimSuffix(strings.Repeat(strings.Repeat(".", m.width)+"\n", m.height), "\n")
	for _, tc := range []struct {
		name   string
		anchor hitRegion
		toast  hitRegion
		want   hitRegion
	}{
		{"top left", hitRegion{W: 1, H: 1}, hitRegion{}, hitRegion{Y: 1, W: 8, H: 3}},
		{"bottom right", hitRegion{X: 19, Y: 8, W: 1, H: 1}, hitRegion{}, hitRegion{X: 12, Y: 5, W: 8, H: 3}},
		{"toast below", hitRegion{X: 5, Y: 4, W: 1, H: 1}, hitRegion{X: 3, Y: 6, W: 6, H: 1}, hitRegion{X: 5, Y: 1, W: 8, H: 3}},
		{"covered icon", hitRegion{X: 5, Y: 4, W: 1, H: 1}, hitRegion{X: 3, Y: 4, W: 6, H: 1}, hitRegion{}},
		{"offscreen", hitRegion{X: 20, Y: 4, W: 1, H: 1}, hitRegion{}, hitRegion{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var notifications []notificationOverlay
			if tc.toast.W > 0 {
				notifications = []notificationOverlay{{Bounds: tc.toast}}
			}
			screen, bounds := m.renderTooltip("说明", tc.anchor, m.height, notifications)
			if bounds != tc.want {
				t.Fatalf("expected %+v, got %+v", tc.want, bounds)
			}
			rows := strings.Split(ansi.Strip(screen), "\n")
			for y, row := range rows {
				want := strings.Repeat(".", m.width)
				if y >= bounds.Y && y < bounds.Y+bounds.H {
					box := []string{"┌──────┐", "│ 说明 │", "└──────┘"}[y-bounds.Y]
					want = strings.Repeat(".", bounds.X) + box + strings.Repeat(".", m.width-bounds.X-bounds.W)
				}
				if row != want {
					t.Fatalf("row %d: got %q, want %q", y, row, want)
				}
			}
		})
	}
}

func TestTooltipFrameMatchesTerminalPalette(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile colorprofile.Profile
		noColor bool
		framed  bool
	}{
		{"truecolor", colorprofile.TrueColor, false, false},
		{"256", colorprofile.ANSI256, false, false},
		{"16", colorprofile.ANSI, false, true},
		{"monochrome", colorprofile.ASCII, false, true},
		{"NO_COLOR", colorprofile.TrueColor, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{width: 12, height: 7, colorProfile: tc.profile, noColor: tc.noColor}
			row := themed(strings.Repeat(".", m.width), text, raised, false)
			m.screen = strings.TrimSuffix(strings.Repeat(row+"\n", m.height), "\n")
			// One row remains below this icon before the footer.
			anchor := hitRegion{X: 10, Y: 4, W: 1, H: 1}
			var bounds hitRegion
			m.screen, bounds = m.renderTooltip("说明", anchor, m.height-1, nil)
			want := hitRegion{X: 6, Y: 5, W: 6, H: 1}
			lines := []string{" 说明 "}
			if tc.framed {
				want = hitRegion{X: 4, Y: 1, W: 8, H: 3}
				lines = []string{"┌──────┐", "│ 说明 │", "└──────┘"}
			}
			if bounds != want {
				t.Fatalf("got bounds %+v, want %+v", bounds, want)
			}
			view := m.View().Content
			rows := strings.Split(ansi.Strip(view), "\n")
			for i, line := range lines {
				if got := ansi.Cut(rows[bounds.Y+i], bounds.X, bounds.X+bounds.W); got != line {
					t.Fatalf("row %d: got %q, want %q", i, got, line)
				}
			}
			if !tc.framed {
				cells := uv.NewScreenBuffer(m.width, m.height)
				uv.NewStyledString(view).Draw(cells, cells.Bounds())
				body := cells.CellAt(bounds.X+1, bounds.Y).Style
				padding := cells.CellAt(bounds.X, bounds.Y).Style
				base := cells.CellAt(anchor.X, anchor.Y).Style
				if padding.Bg != body.Bg || terminalContrast(base.Bg, body.Bg) < 1.5 || terminalContrast(body.Fg, body.Bg) < 4.5 {
					t.Fatalf("borderless surface lost its fill or contrast: text %v, fill %v, padding %v, base %v", body.Fg, body.Bg, padding.Bg, base.Bg)
				}
			}
		})
	}
}

func TestTooltipSurfaceRewrapsStyledGraphemesOnResize(t *testing.T) {
	content := "\x1b[1m甲乙👩‍💻e\u0301丙丁\x1b[0m"
	m := Model{height: 8, colorProfile: colorprofile.TrueColor}
	anchor := hitRegion{W: 1, H: 1}
	for _, width := range []int{24, 10} {
		m.width = width
		screen, bounds := m.renderTooltip(content, anchor, m.height, nil)
		assertCanvas(t, screen, m.width, m.height)
		cells := uv.NewScreenBuffer(m.width, m.height)
		cells.Method = ansi.GraphemeWidth
		uv.NewStyledString(screen).Draw(cells, cells.Bounds())
		var body strings.Builder
		for y := bounds.Y; y < bounds.Y+bounds.H; y++ {
			for x := bounds.X + 1; x < bounds.X+bounds.W-1; x++ {
				cell := cells.CellAt(x, y)
				if cell.Content == "" || cell.Content == " " {
					continue
				}
				if cell.Style.Attrs&uv.AttrBold == 0 {
					t.Fatalf("style lost at (%d,%d) on width %d: %+v", x, y, width, cell)
				}
				body.WriteString(cell.Content)
			}
		}
		if body.String() != ansi.Strip(content) {
			t.Fatalf("wrapped text at width %d: %q", width, body.String())
		}
	}

	m.width, m.height = 10, 2
	screen, bounds := m.renderTooltip(content, anchor, m.height, nil)
	assertCanvas(t, screen, m.width, m.height)
	if bounds.H != 1 || !strings.Contains(ansi.Strip(screen), "…") {
		t.Fatalf("expected clipped text in a single row:\n%s", ansi.Strip(screen))
	}
}
