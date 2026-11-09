package tui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Use the same clear/draw/render cycle as Bubble Tea. The changed counter
// causes a real frame while the title remains identical to the previous frame.
func TestRendererLeavesUnchangedWideRowsAlone(t *testing.T) {
	for _, title := range []string{"STATIC ROUTE", "代理路径", "💤 lazyClash", "👩‍💻 连接"} {
		for _, grapheme := range []bool{false, true} {
			for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ASCII} {
				t.Run(fmt.Sprintf("%s/grapheme=%t/profile=%v", title, grapheme, profile), func(t *testing.T) {
					var output bytes.Buffer
					renderer := uv.NewTerminalRenderer(&output, []string{"TERM=xterm-256color"})
					renderer.SetFullscreen(true)
					renderer.SetGraphemeWidth(grapheme)
					renderer.SetColorProfile(profile)
					renderer.Resize(60, 4)
					screen := uv.NewScreenBuffer(60, 4)
					draw := func(counter int) {
						t.Helper()
						screen.Clear()
						uv.NewStyledString(fmt.Sprintf("\x1b[48;2;48;56;65m%s\ncounter: %d\x1b[m", title, counter)).Draw(screen, screen.Bounds())
						renderer.Render(screen.RenderBuffer)
						if err := renderer.Flush(); err != nil {
							t.Fatal(err)
						}
					}
					draw(1)
					output.Reset()
					draw(2)
					if got := output.String(); strings.Contains(got, title) || strings.Contains(got, ansi.EraseLineRight) {
						t.Fatalf("counter update erased or rewrote an unchanged row: %q", got)
					} else if !strings.Contains(got, "2") {
						t.Fatalf("counter update was lost: %q", got)
					}
					output.Reset()
					draw(2)
					if got := output.String(); got != "" {
						t.Fatalf("identical frame emitted output: %q", got)
					}
				})
			}
		}
	}
}

func TestRendererStillRepaintsChangedWideRows(t *testing.T) {
	for _, change := range []struct {
		name, before, after string
	}{
		{"text", "代理路径", "代理规则"},
		{"style", "\x1b[31m代理路径\x1b[m", "\x1b[32m代理路径\x1b[m"},
		{"hyperlink", "\x1b]8;;https://before.example\x1b\\代理\x1b]8;;\x1b\\", "\x1b]8;;https://after.example\x1b\\代理\x1b]8;;\x1b\\"},
		{"shorter", "代理路径", "代理"},
		{"narrow", "代理路径", "proxy"},
	} {
		t.Run(change.name, func(t *testing.T) {
			var output bytes.Buffer
			renderer := uv.NewTerminalRenderer(&output, []string{"TERM=xterm-256color"})
			renderer.SetFullscreen(true)
			renderer.SetColorProfile(colorprofile.TrueColor)
			renderer.Resize(20, 2)
			screen := uv.NewScreenBuffer(20, 2)
			draw := func(content string) {
				t.Helper()
				screen.Clear()
				uv.NewStyledString(content).Draw(screen, screen.Bounds())
				renderer.Render(screen.RenderBuffer)
				if err := renderer.Flush(); err != nil {
					t.Fatal(err)
				}
			}
			draw(change.before)
			output.Reset()
			draw(change.after)
			got := output.String()
			if strings.Count(got, ansi.EraseLineRight) != 1 || !strings.Contains(got, ansi.Strip(change.after)) {
				t.Fatalf("changed wide row was not fully repainted: %q", got)
			}
			if !strings.Contains(got, ansi.ResetModeAutoWrap) || !strings.Contains(got, ansi.SetModeAutoWrap) {
				t.Fatalf("changed wide row lost autowrap protection: %q", got)
			}
		})
	}
}
