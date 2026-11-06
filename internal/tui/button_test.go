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
						if state == "focus" && !strings.HasPrefix(plain, " ›") {
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
		{"page", "Edit", text, raised, buttonOptions{}},
		{"card", "Edit", text, raised, buttonOptions{Surface: panel}},
		{"modal", "Edit", text, panel, buttonOptions{Surface: raised}},
		{"primary", "Apply", primaryButtonForeground, primaryButtonBackground, buttonOptions{Kind: buttonPrimary}},
		{"selected", "Rule", text, buttonSelectedBackground, buttonOptions{Selected: true, Selectable: true}},
		{"danger", "Delete", buttonDangerForeground, raised, buttonOptions{Kind: buttonDanger}},
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
			bold := sample.options.Kind == buttonPrimary || sample.options.Kind == buttonDangerConfirm || sample.options.Selected
			want := themed(lipgloss.NewStyle().Bold(bold).Render(plain), sample.foreground, sample.background, false)
			if !strings.Contains(face, want) {
				t.Fatalf("button lost its role colors: got %q want %q", face, want)
			}
		})
	}
	value := renderButton("Rule", buttonOptions{Selected: true, Selectable: true, Focused: true})
	if !strings.Contains(value, themed(" ›", focus, bg, false)) {
		t.Fatal("selected state replaced the separate orange focus marker")
	}
}

func TestSelectableButtonCentersLabelsWithoutMovingAcrossStates(t *testing.T) {
	for _, label := range []string{"Rule", "全局", "English", "简体中文"} {
		for _, plain := range []bool{false, true} {
			o := buttonOptions{Selectable: true, Surface: panel, Width: 8, Plain: plain}
			idle := ansi.Strip(renderButton(label, o))
			for _, state := range []string{"selected", "focused", "hovered", "busy", "disabled"} {
				o.Selected = true
				o.Focused, o.Hovered = state == "focused", state == "hovered"
				o.Busy, o.Disabled = state == "busy", state == "disabled"
				value := renderButton(label, o)
				stripped := ansi.Strip(value)
				if ansi.StringWidth(stripped) != 12 || strings.Contains(stripped, "✓") {
					t.Fatalf("%s/%s changed the group's width or restored the check: %q", label, state, stripped)
				}
				faceText := ansi.Cut(stripped, 2, 12)
				before, after, _ := strings.Cut(faceText, label)
				if delta := ansi.StringWidth(after) - ansi.StringWidth(before); delta < 0 || delta > 1 {
					t.Fatalf("%s/%s has asymmetric label padding: %q", label, state, stripped)
				}
				idleBefore, _, _ := strings.Cut(idle, label)
				selectedBefore, _, _ := strings.Cut(stripped, label)
				if ansi.StringWidth(idleBefore) != ansi.StringWidth(selectedBefore) {
					t.Fatalf("%s/%s moved the label", label, state)
				}
				if plain && (!strings.HasPrefix(faceText, "<") || !strings.HasSuffix(faceText, ">")) {
					t.Fatalf("%s/%s lost its monochrome selection: %q", label, state, stripped)
				}
				if !plain && (o.Busy || o.Disabled) {
					face := ansi.Cut(value, 2, 12)
					want := themed(lipgloss.NewStyle().Bold(true).Render(ansi.Strip(face)), text, buttonSelectedBackground, false)
					if !strings.Contains(face, want) {
						t.Fatalf("%s/%s cleared or dimmed the current selection", label, state)
					}
				}
			}
		}
	}
}

func TestButtonGutterKeepsFocusNextToItsOwnFace(t *testing.T) {
	first := renderButton("规则", buttonOptions{Selectable: true, Surface: panel, Width: 6})
	second := renderButton("全局", buttonOptions{Selectable: true, Surface: panel, Width: 6, Focused: true})
	width := ansi.StringWidth(first)
	row := ansi.Strip(first + second)
	if got := ansi.Cut(row, width, width+2); got != " ›" {
		t.Fatalf("focus needs one empty cell after the preceding face: %q", got)
	}
	if !strings.HasPrefix(second, themed(" ›", focus, panel, false)) {
		t.Fatal("focus is not immediately followed by its own painted face")
	}
	if ansi.StringWidth(first) != 10 || ansi.StringWidth(second) != 10 {
		t.Fatal("selectable buttons no longer have an eight-cell face and two-cell gutter")
	}
}

func TestButtonHoverKeepsItsSemanticRole(t *testing.T) {
	for _, sample := range []struct {
		kind       buttonKind
		selected   bool
		foreground string
		background string
	}{
		{buttonSecondary, false, text, buttonHoverBackground},
		{buttonSecondary, true, text, buttonSelectedHoverBackground},
		{buttonPrimary, false, primaryButtonForeground, buttonPrimaryHoverBackground},
		{buttonDanger, false, buttonDangerForeground, buttonDangerHoverBackground},
		{buttonDangerConfirm, false, primaryButtonForeground, buttonDangerForeground},
	} {
		o := buttonOptions{Kind: sample.kind, Hovered: true, Selected: sample.selected, Selectable: sample.selected, Surface: panel}
		face := ansi.Cut(renderButton("Action", o), 2, 100)
		bold := sample.kind == buttonPrimary || sample.kind == buttonDangerConfirm || sample.selected
		want := themed(lipgloss.NewStyle().Bold(bold).Render(ansi.Strip(face)), sample.foreground, sample.background, false)
		if !strings.Contains(face, want) {
			t.Fatalf("kind %d selected=%t lost its hover role", sample.kind, sample.selected)
		}
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
