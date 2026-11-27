package tui

import "strings"

const (
	noticeSlate     = "#4e6377"
	noticeText      = "#eff4f8"
	noticeCheck     = "#c5e8b9"
	noticeWarning   = "#ffe3a7"
	noticeErrorFill = "#81524f"
	noticeErrorText = "#fff2ef"
	noticeErrorMark = "#ffd2c8"
)

type noticePaint struct {
	background, foreground, signal, marker string
}

func notificationPaint(kind string) noticePaint {
	switch kind {
	case "done":
		return noticePaint{noticeSlate, noticeText, noticeCheck, "✓"}
	case "error":
		return noticePaint{noticeErrorFill, noticeErrorText, noticeErrorMark, "!"}
	case "warning":
		return noticePaint{noticeSlate, noticeText, noticeWarning, "!"}
	case "info":
		return noticePaint{noticeSlate, noticeText, noticeText, "i"}
	default: // Working notifications already include a spinner.
		return noticePaint{noticeSlate, noticeText, noticeText, ""}
	}
}

func (p noticePaint) button(id, label string, kind buttonKind, width int, o dashboardOptions) string {
	label = cellCrop(strings.Trim(label, "[] "), max(1, width-4))
	return cellCrop(renderButton(label, buttonOptions{
		Kind: kind, Surface: p.background, TextColor: p.foreground,
		Focused: o.Focus == id, Hovered: o.Hover == id, Plain: o.PlainButtons,
	}), width)
}
