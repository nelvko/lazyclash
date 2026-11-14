package tui

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/help"
	bubbleskey "charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

// Keep help discoverable while whole context hints compete for the remaining
// cells. Bubbles renders the selected bindings; the shell owns the pinned tail.
func dashboardKeyHints(o dashboardOptions) string {
	width := max(0, o.Width-4)
	tail := o.Locale.T("dashboard.footer.help")
	if width >= 36 {
		tail += " · " + o.Locale.T("dashboard.footer.quit")
	}
	if ansi.StringWidth(tail) > width {
		return helpText(cellCrop("?", width), bg, o.Dim)
	}
	context := o.Help
	if context == "" {
		context = o.Locale.T("dashboard.footer.default")
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
	for _, binding := range bindings {
		candidate := append(append([]bubbleskey.Binding(nil), selected...), binding)
		if ansi.StringWidth(strings.TrimSpace(view.ShortHelpView(candidate))) <= available {
			selected = candidate
		}
	}
	left := strings.TrimSpace(view.ShortHelpView(selected))
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(tail)
	return helpText(left, bg, o.Dim) + themed(strings.Repeat(" ", gap), muted, bg, o.Dim) + helpText(tail, bg, o.Dim)
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
