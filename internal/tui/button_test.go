package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestButtonFocusAndColorFallbackPreserveGeometry(t *testing.T) {
	for _, label := range []string{"Apply", "应用配置", "⋯", "↻"} {
		for _, kind := range []buttonKind{buttonSecondary, buttonPrimary, buttonText, buttonDanger, buttonDangerConfirm} {
			for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
				t.Run(fmt.Sprintf("%s/%d/%s", label, kind, profile), func(t *testing.T) {
					o := buttonOptions{Kind: kind, Surface: panel, Plain: profile <= colorprofile.ANSI}
					idle := renderButton(label, o)
					for _, state := range []string{"focus", "hover", "disabled", "working"} {
						o.Focused, o.Hovered, o.Disabled, o.Busy = state == "focus", state == "hover", state == "disabled", state == "working"
						value := renderButton(label, o)
						if ansi.StringWidth(value) != ansi.StringWidth(idle) {
							t.Fatalf("%s moved adjacent controls", state)
						}
						prefix, _, _ := strings.Cut(ansi.Strip(value), label)
						idlePrefix, _, _ := strings.Cut(ansi.Strip(idle), label)
						if ansi.StringWidth(prefix) != ansi.StringWidth(idlePrefix) {
							t.Fatalf("%s moved the button label", state)
						}
						plain := ansi.Strip(terminalTheme(value, profile))
						if state == "focus" && !strings.HasPrefix(plain, "› ") {
							t.Fatal("focus marker does not own its fixed gutter")
						}
						if o.Plain && kind != buttonText && !strings.Contains(plain, "["+label+"]") {
							t.Fatal("low-color button lost its textual boundary")
						}
					}
				})
			}
		}
	}
}

func TestButtonRolesAndSurfacesRemainSeparate(t *testing.T) {
	for _, sample := range []struct {
		name, label, foreground, background string
		options                             buttonOptions
	}{
		{"page", "Edit", text, panel, buttonOptions{}},
		{"card", "Edit", text, raised, buttonOptions{Surface: panel}},
		{"modal", "Edit", text, raised, buttonOptions{Surface: raised}},
		{"primary", "Apply", primaryButtonForeground, primaryButtonBackground, buttonOptions{Kind: buttonPrimary}},
		{"selected", "✓ Rule", selectionForeground, selectionBackground, buttonOptions{Selected: true, Selectable: true}},
		{"danger", "Delete", errorColor, panel, buttonOptions{Kind: buttonDanger}},
		{"confirmation", "Delete", primaryButtonForeground, errorColor, buttonOptions{Kind: buttonDangerConfirm}},
		{"text", "More", accent, panel, buttonOptions{Kind: buttonText, Surface: panel}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			label := sample.label
			if sample.options.Selectable {
				label = "Rule"
			}
			face := ansi.Cut(renderButton(label, sample.options), 2, 100)
			plain := ansi.Strip(face)
			if !strings.Contains(plain, sample.label) || strings.ContainsAny(plain, "[]") {
				t.Fatalf("colored button has the wrong face: %q", plain)
			}
			bold := sample.options.Kind == buttonPrimary || sample.options.Kind == buttonDangerConfirm
			want := themed(lipgloss.NewStyle().Bold(bold).Render(plain), sample.foreground, sample.background, false)
			if !strings.Contains(face, want) {
				t.Fatalf("button lost its role colors: got %q want %q", face, want)
			}
		})
	}
	value := renderButton("Rule", buttonOptions{Selected: true, Selectable: true, Focused: true})
	if !strings.Contains(value, themed("› ", focus, bg, false)) {
		t.Fatal("selected state replaced the separate orange focus marker")
	}
}

func TestHeaderButtonFocusDoesNotMoveOtherActionsOrHits(t *testing.T) {
	for _, size := range [][2]int{{30, 12}, {60, 20}, {80, 24}, {160, 45}, {216, 55}} {
		m := headerModel("logs", size[0], size[1])
		m.focus = "logs.follow"
		m.layout()
		first, second := findTarget(t, m, "logs.follow"), findTarget(t, m, "logs.retry")
		m.focus, m.hover = "logs.retry", "logs.follow"
		m.layout()
		if findTarget(t, m, "logs.follow") != first || findTarget(t, m, "logs.retry") != second {
			t.Fatalf("focus/hover moved header hit regions at %v", size)
		}
	}
}
