package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

// All notification surfaces use the same result text. Errors keep the operation
// next to its cause so independent failures are readable even without color.
func notificationBody(item notificationItem, locale i18n.Localizer, spinner string) string {
	message := strings.TrimSpace(notificationText(item.Message))
	switch item.Kind {
	case "error":
		label := clean(item.Context)
		if label == "" {
			label = locale.T("notification.error")
		}
		return locale.T("notification.message", label, message)
	case "warning":
		return locale.T("notification.message", locale.T("notification.warning"), message)
	case "working":
		return strings.TrimSpace(clean(spinner) + " " + message)
	default:
		return message
	}
}

func notificationExtra(item notificationItem) string {
	extra := strings.TrimSpace(notificationText(item.Details))
	if strings.Contains(notificationText(item.Message), extra) {
		return ""
	}
	return extra
}

// Preserve diagnostic paragraphs while excluding terminal control sequences.
func notificationText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(value))
}
