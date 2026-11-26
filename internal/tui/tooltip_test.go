package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTooltipLabelUsesRenderedCells(t *testing.T) {
	tip := tooltipBinding{owner: "toggle", text: "Help"}
	for _, width := range []int{40, 8, 3} {
		for _, hover := range []string{"", tip.id()} {
			block := tip.label("\x1b[1m内核 e\u0301 👩‍💻\x1b[m", dashboardOptions{Hover: hover, tooltips: &tooltipState{}}, panel, width)
			icon := block.targets[0]
			line := block.lines[icon.Y]
			if icon.tip != tip || ansi.Strip(ansi.Cut(line, icon.X, icon.X+icon.W)) != tooltipIcon {
				t.Fatalf("width %d: icon cells %+v on %q", width, icon, line)
			}
			ink := muted
			if hover != "" {
				ink = accent
			}
			if !strings.Contains(line, themed(tooltipIcon, ink, panel, false)) {
				t.Fatalf("expected icon color %s for hover %q: %q", ink, hover, line)
			}
		}
	}
}

func TestTooltipHoverDelayAndKeyboardOwnership(t *testing.T) {
	first := tooltipBinding{owner: "first", text: "A"}
	second := tooltipBinding{owner: "second", text: "B"}
	tip := tooltipState{anchors: []hitRegion{
		{ID: first.id(), tip: first, W: 1, H: 1},
		{ID: second.id(), tip: second, X: 4, W: 1, H: 1},
	}}
	if tip.motion(first.id()) == nil || tip.open != "" {
		t.Fatal("entering the icon should arm one timer")
	}
	stale := tip.generation
	if tip.motion(first.id()) != nil || tip.generation != stale {
		t.Fatal("repeated motion over the icon rescheduled its timer")
	}
	tip.motion("")
	tip.motion(first.id())
	if tip.showAfterDelay(stale) {
		t.Fatal("re-entering an icon accepted an older timer")
	}
	if !tip.showAfterDelay(tip.generation) || tip.open != first.id() {
		t.Fatal("dwelling on an icon did not open its tooltip")
	}
	tip.motion("")
	if tip.open != "" {
		t.Fatal("leaving the icon kept a mouse tooltip open")
	}
	tip.key("f1", first.owner)
	tip.motion(second.id())
	stale = tip.generation
	tip.motion(first.id())
	tip.showAfterDelay(stale)
	tip.motion("")
	if !tip.keyboard || tip.open != first.id() {
		t.Fatal("passing another icon stole the keyboard tooltip")
	}
	tip.motion(second.id())
	tip.showAfterDelay(tip.generation)
	if tip.keyboard || tip.open != second.id() {
		t.Fatal("dwelling on another icon did not switch to its explanation")
	}
	tip.key("f1", first.owner)
	if !tip.keyboard || tip.open != first.id() {
		t.Fatal("keyboard did not take ownership of the focused control's tooltip")
	}
}
