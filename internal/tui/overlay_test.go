package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestOverlayKeepsCellPositionsAcrossPartialWideCharacters(t *testing.T) {
	for _, tc := range []struct {
		name, base, box, want string
		x, width              int
	}{
		{"left edge", "a中文bc", "X", "a X文bc", 2, 7},
		{"right edge", "a中文bc", "X", "aX 文bc", 1, 7},
		{"both edges", "a中文bc", "XX", "a XX bc", 2, 7},
		{"emoji right edge", "a👩‍💻bc", "X", "aX bc", 1, 5},
		{"clipped overlay", "abcdef", "中文X", " 文Xef", -1, 6},
		{"clipped final glyph", "abcdef", "中文", "abcd中", 4, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := themed(tc.base, text, bg, false)
			box := themed(tc.box, focus, raised, false)
			view := overlay(base, box, tc.x, 0, tc.width, 1)
			if got := ansi.Strip(view); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			assertCanvas(t, view, tc.width, 1)
		})
	}
}

func TestCenteredOverlayDoesNotShiftChineseBackgroundRows(t *testing.T) {
	const width, height = 60, 18
	base := strings.Repeat(strings.Repeat("中文", width/4)+"\n", height-1) + strings.Repeat("中文", width/4)
	box := "┌───────────────────┐\n│ Dialog            │\n└───────────────────┘"
	x := (width - 21) / 2
	view := overlay(base, box, x, 7, width, height)
	for row, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) != width {
			t.Fatalf("row %d shifted to %d cells", row, ansi.StringWidth(line))
		}
		if row >= 7 && row <= 9 && ansi.Strip(ansi.Cut(line, x, x+1)) != string([]rune(strings.Split(box, "\n")[row-7])[0]) {
			t.Fatalf("dialog row %d shifted from column %d", row, x)
		}
	}
}
