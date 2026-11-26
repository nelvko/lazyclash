package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestNotificationListReflowsDiagnosticsWhenTheTerminalGrows(t *testing.T) {
	locale := i18n.New(i18n.Chinese)
	items := []notificationItem{
		{ID: 1, Kind: "error", Message: "启动服务超时", Details: strings.Repeat("重试中\n", 10) + "最后一步"},
		{ID: 2, Kind: "done", Message: "已保存"},
	}
	var p notificationPanel
	p.show(0)
	box, hits := p.render(items, 40, 13, locale)
	if !hasAction(hits, "notification.panel.details.1") || strings.Contains(ansi.Strip(box), "最后一步") {
		t.Fatal("an overflowing diagnostic should open in its own detail viewport")
	}
	p.show(1)
	box, hits = p.render(items, 40, 13, locale)
	if strings.Contains(ansi.Strip(box), "Esc") || hasAction(hits, p.backAction()) ||
		p.key(special(tea.KeyEscape)) != "notification.panel.back" {
		t.Fatal("detail Escape must return to the list without an in-card Back control")
	}
	p.show(0)
	box, hits = p.render(items, 160, 45, locale)
	plain := ansi.Strip(box)
	if !strings.Contains(plain, "最后一步") || !strings.Contains(plain, "已保存") ||
		hasAction(hits, "notification.panel.details.1") || hasAction(hits, "notification.panel.close") ||
		strings.Contains(plain, "Esc") {
		t.Fatalf("expected both complete results without in-card navigation:\n%s", plain)
	}
	if p.key(special(tea.KeyEnter)) != "notification.panel.dismiss.1" ||
		p.key(special(tea.KeyEscape)) != "notification.panel.close" {
		t.Fatal("removing Details on resize lost its card selection or Escape action")
	}
}

func TestCompleteNotificationUsesFullListViewportBeforeShowingDetails(t *testing.T) {
	locale := i18n.New(i18n.Chinese)
	items := []notificationItem{
		{ID: 1, Kind: "error", Message: "启动服务超时", Details: strings.Repeat("重试中\n", 5) + "最后一步"},
		{ID: 2, Kind: "done", Message: "已保存"},
	}
	var p notificationPanel
	p.show(0)
	box, hits := p.render(items, 40, 13, locale)
	if !strings.Contains(ansi.Strip(box), "最后一步") || hasAction(hits, "notification.panel.details.1") {
		t.Fatalf("the neighboring notice displaced readable diagnostic text:\n%s", ansi.Strip(box))
	}
	p.key(special(tea.KeyEnd))
	box, hits = p.render(items, 40, 13, locale)
	if !strings.Contains(ansi.Strip(box), "已保存") || !hasAction(hits, "notification.panel.dismiss.2") ||
		p.key(special(tea.KeyEnter)) != "notification.panel.dismiss.2" {
		t.Fatalf("could not reach the following notice:\n%s", ansi.Strip(box))
	}
}
