package tui

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/help"
	bubbleskey "charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

// Keep the footer toggle visible while whole context hints compete for the remaining
// cells. Bubbles renders the selected bindings; the shell owns the pinned tail.
func dashboardKeyHints(o dashboardOptions) (string, hitRegion) {
	width := max(0, o.Width-4)
	tail := o.Locale.T("dashboard.footer.hideHints")
	hasTip := o.tooltips != nil && o.tooltips.focused(o.Focus).ID != ""
	if o.OverlayOwnsKeys {
		tail = o.Locale.T("dashboard.footer.hideHintsAfterClose")
		if hasTip {
			tail = o.Locale.T("tooltip.shortcut")
		}
	} else if o.EditingInput {
		tail = "Esc → " + tail
	} else if width >= 36 {
		tail += " · " + o.Locale.T("dashboard.footer.quit")
	}
	if ansi.StringWidth(tail) > width {
		fallback := "?"
		if o.EditingInput || o.OverlayOwnsKeys {
			fallback = "Esc"
		}
		return helpText(cellCrop(fallback, width), shellBackground, o.Dim), hitRegion{}
	}
	context := o.Help
	if o.tooltips != nil && o.tooltips.open != "" {
		context = o.Locale.T("tooltip.dismiss")
	}
	if context == "" {
		context = o.Locale.T("dashboard.footer.default")
	}
	if hasTip && !o.OverlayOwnsKeys {
		context += " · " + o.Locale.T("tooltip.shortcut")
	}
	var bindings []bubbleskey.Binding
	for _, item := range strings.Split(context, " · ") {
		label, description, _ := strings.Cut(strings.TrimSpace(item), " ")
		if label == "" || label == "?" || label == "q" {
			continue
		}
		bindings = append(bindings, bubbleskey.NewBinding(bubbleskey.WithKeys(label), bubbleskey.WithHelp(label, description)))
	}
	sort.SliceStable(bindings, func(i, j int) bool {
		return keyHintPriority(bindings[i].Help().Key) < keyHintPriority(bindings[j].Help().Key)
	})
	view := help.Model{ShortSeparator: " · "}
	available := max(0, width-ansi.StringWidth(tail)-2)
	var selected []bubbleskey.Binding
	var searchHint hitRegion
	searchEnabled := o.Page == "proxies" && !o.Busy && !o.Dim && !o.SidebarOpen && !o.EditingInput && !o.OverlayOwnsKeys
	for _, binding := range bindings {
		candidate := append(append([]bubbleskey.Binding(nil), selected...), binding)
		lineWidth := ansi.StringWidth(strings.TrimSpace(view.ShortHelpView(candidate)))
		if lineWidth > available {
			continue
		}
		selected = candidate
		if searchEnabled && binding.Help().Key == "/" {
			itemWidth := ansi.StringWidth(strings.TrimSpace(view.ShortHelpView([]bubbleskey.Binding{binding})))
			searchHint = hitRegion{ID: "proxies.search-hint", X: lineWidth - itemWidth, W: itemWidth, H: 1}
		}
	}
	left := strings.TrimSpace(view.ShortHelpView(selected))
	leftWidth := ansi.StringWidth(left)
	contextHints := helpText(left, shellBackground, o.Dim)
	if searchHint.ID != "" && o.Hover == searchHint.ID {
		start, end := searchHint.X, searchHint.X+searchHint.W
		contextHints = ansi.Cut(contextHints, 0, start) + themed(ansi.Cut(left, start, end), text, shellBackground, false) + ansi.Cut(contextHints, end, leftWidth)
	}
	gap := width - leftWidth - ansi.StringWidth(tail)
	return contextHints + themed(strings.Repeat(" ", gap), muted, shellBackground, o.Dim) + helpText(tail, shellBackground, o.Dim), searchHint
}

func keyHintPriority(label string) int {
	switch label {
	case "Enter":
		return 0
	case "/":
		return 1
	case "↑↓", "↑↓←→", "←→", "→":
		return 2
	case "Esc", "←":
		return 3
	case "Tab":
		return 4
	default:
		return 5
	}
}
