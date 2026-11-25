package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestModalSurfaceUsesPaddingForLowColorOutline(t *testing.T) {
	for _, mode := range []struct {
		name    string
		profile colorprofile.Profile
		noColor bool
		framed  bool
	}{
		{"truecolor", colorprofile.TrueColor, false, false},
		{"256", colorprofile.ANSI256, false, false},
		{"16", colorprofile.ANSI, false, true},
		{"ascii", colorprofile.ASCII, false, true},
		{"NO_COLOR", colorprofile.TrueColor, true, true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			m := Model{width: 19, height: 9, colorProfile: mode.profile, noColor: mode.noColor}
			for _, size := range [][2]int{{11, 5}, {19, 5}, {11, 9}, {19, 9}} {
				w, h := size[0], size[1]
				s := m.newModalSurface(w, h, raised)
				s.put(2, 1, "按钮")
				hit := s.screen(hitRegion{ID: "action", X: 2, Y: 1, W: 4, H: 1})
				x, y := (m.width-w)/2, (m.height-h)/2
				if hit != (hitRegion{ID: "action", X: x + 2, Y: y + 1, W: 4, H: 1}) {
					t.Fatalf("wrong screen coordinates at %v: %+v", size, hit)
				}
				view := s.render(strings.Repeat(strings.Repeat(".", m.width)+"\n", m.height))
				assertCanvas(t, view, m.width, m.height)
				rows := strings.Split(ansi.Strip(view), "\n")
				if got := ansi.Cut(rows[hit.Y], hit.X, hit.X+hit.W); got != "按钮" {
					t.Fatalf("target at %v contains %q", size, got)
				}
				top, bottom := strings.Repeat(" ", w), strings.Repeat(" ", w)
				if mode.framed && (w < m.width || h < m.height) {
					edge := strings.Repeat("─", w-2)
					top, bottom = "┌"+edge+"┐", "└"+edge+"┘"
				}
				if ansi.Cut(rows[y], x, x+w) != top || ansi.Cut(rows[y+h-1], x, x+w) != bottom {
					t.Fatalf("wrong perimeter at %v:\n%s", size, strings.Join(rows, "\n"))
				}
				if y > 0 && rows[y-1] != strings.Repeat(".", m.width) {
					t.Fatalf("modal changed the backdrop at %v", size)
				}
			}
		})
	}
}

func TestModalSurfaceRestoresBackgroundBetweenControls(t *testing.T) {
	m := Model{width: 24, height: 8, colorProfile: colorprofile.TrueColor}
	s := m.newModalSurface(16, 4, raised)
	action := themed("按钮", text, primaryButtonBackground, false)
	s.put(2, 1, action+"  "+action)
	screen := uv.NewScreenBuffer(m.width, m.height)
	uv.NewStyledString(s.render("")).Draw(screen, screen.Bounds())
	for _, sample := range []struct {
		x    int
		want string
	}{{0, raised}, {2, primaryButtonBackground}, {6, raised}, {7, raised}, {8, primaryButtonBackground}, {12, raised}} {
		cell := screen.CellAt(s.bounds.X+sample.x, s.bounds.Y+1)
		style := cell.Style
		style.Bg = lipgloss.Color(sample.want)
		if !cell.Style.Equal(&style) {
			t.Fatalf("cell at local column %d lost its %s background", sample.x, sample.want)
		}
	}
}
