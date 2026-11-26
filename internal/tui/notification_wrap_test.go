package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestNotificationWrapUsesTheActualCornerHeight(t *testing.T) {
	item := notificationItem{
		ID: 9, Kind: "error", Context: "启动服务",
		Message: "Mihomo API 未能就绪：context deadline exceeded",
		Details: "检查内核状态。\n检查配置。\n检查运行日志。\n随后重试。",
	}
	for _, size := range [][2]int{{160, 45}, {40, 24}} {
		o := dashboardOptions{Locale: i18n.New(i18n.Chinese), Width: size[0], Height: size[1], Page: "overview", Notifications: []notificationItem{item}}
		layout := notificationLayout(o)
		if len(layout) != 1 || len(layout[0].IDs) != 1 || len(layout[0].Targets) != 1 || layout[0].Targets[0].ID != "notification.close.9" {
			t.Fatalf("%v: a readable result should expose only its close control: %+v", size, layout)
		}
		visible := ansi.Strip(layout[0].Content)
		if layout[0].Bounds.H <= 3 || !strings.Contains(visible, "随后重试。") {
			t.Fatalf("%v: the corner stopped before the end of the message: %s", size, visible)
		}
	}
}

func TestWrappedNotificationPreservesNamesAcrossDisplayCells(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English)}
	for _, message := range []string{
		strings.Repeat("abcdefghijklmnopqrstuvwxyz", 5),
		strings.Repeat("很长的订阅名称", 12),
		strings.Repeat("ab工作-🇨🇳/e\u0301", 12),
		strings.Repeat("中　- /🇨🇳-中👩‍💻", 8),
	} {
		for _, width := range []int{16, 25, 36, 52} {
			card := notificationCard(notificationItem{ID: 4, Kind: "done", Message: message}, o, width, 40, true)
			plain := ansi.Strip(card.Content)
			body := strings.Join(strings.Fields(strings.NewReplacer("✓", "", "×", "").Replace(plain)), "")
			if body != strings.Join(strings.Fields(message), "") || len(card.IDs) != 1 || hasAction(card.Targets, "notification.details.4") {
				t.Fatalf("width %d: wrapped notification lost content:\n%s", width, plain)
			}
		}
	}
}

func TestNotificationLinesUseChineseAndEnglishBreaks(t *testing.T) {
	body := "2 个配置无法更新：测试订阅"
	lines := notificationLines(body, 14, 18)
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "2 个配置无法") || strings.Join(lines, "") != body {
		t.Fatalf("Chinese text should fill the space after the count: %q", lines)
	}
	lines = notificationLines("Mihomo API deadline exceeded\n\n  Try again", 12, 12)
	if strings.Join(lines, "\n") != "Mihomo API\ndeadline\nexceeded\n\n  Try again" {
		t.Fatalf("English words, explicit paragraphs or indentation changed: %q", lines)
	}
}

func TestNotificationCardReflowsWhenItsRowBudgetChanges(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English), Busy: true}
	for _, kind := range []string{"done", "working"} {
		t.Run(kind, func(t *testing.T) {
			item := notificationItem{ID: 7, Kind: kind, Message: "First\nSecond\nThird", Details: "Fourth\nFifth"}
			needed := 5
			if kind == "working" {
				needed++ // Keep Cancel available throughout the resize.
			}
			for _, budget := range []int{needed, needed - 1, needed} {
				card := notificationCard(item, o, 32, budget, false)
				wantFull := budget == needed
				if card.Bounds.H > budget || (len(card.IDs) == 1) != wantFull || hasAction(card.Targets, "notification.details.7") == wantFull {
					t.Fatalf("%d rows: unexpected visibility or control geometry: %+v", budget, card)
				}
				if strings.Contains(ansi.Strip(card.Content), "Fifth") != wantFull {
					t.Fatalf("%d rows: visible text and expiry state disagree", budget)
				}
				for _, target := range card.Targets {
					face := ansi.Strip(cellSlice(strings.Split(card.Content, "\n")[target.Y], target.X, target.X+target.W))
					if strings.TrimSpace(face) == "" || target.ID == "notification.close.7" && strings.TrimSpace(face) != "×" {
						t.Fatalf("%d rows: misplaced %s: %q", budget, target.ID, face)
					}
				}
				if hasAction(card.Targets, "notification.cancel.7") != (kind == "working") {
					t.Fatal("resizing changed the task controls")
				}
			}
		})
	}
}

func TestNotificationStackPrioritizesVisibleTextOverPadding(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English)}
	long := notificationItem{ID: 1, Kind: "done", Message: "One\nTwo\nThree\nFour\nFive\nSix\nSeven"}
	short := notificationItem{ID: 2, Kind: "done", Message: "Saved"}
	for _, order := range [][]notificationItem{{short, long}, {long, short}} {
		cards := notificationStack(order, o, 40, 10, false)
		for _, card := range cards {
			if len(card.IDs) != 1 || len(card.Targets) != 1 {
				t.Fatal("vertical padding displaced readable text", card)
			}
		}
		if cards[0].Bounds.H+cards[1].Bounds.H+1 > 10 {
			t.Fatal("stack exceeded the available height")
		}
	}
}
