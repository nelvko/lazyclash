package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestPageNavigationCardFocusPreservesOverlappingTaskNotice(t *testing.T) {
	m := pageNavigationModel()
	m.snapshot.GuideDismissed = true
	m.focus = "overview.card.dashboard.control.routingMode"
	m.newJob("action")
	defer m.cancel()
	m.message = "Updating subscription; existing connections remain available while this task runs. The saved configuration remains available until the update finishes."
	m.syncNotification()
	m.notification.items[0].showAfter = time.Time{} // Exercise the visible, long-running progress card.
	m.layout()

	o := m.navigationOptions()
	g := dashboardGeometry(o)
	// Choose a card from the actual layout whose focus border the notice
	// covers; shorter, independently flowing cards can move between columns.
	focused := false
	for _, card := range overviewCardRegionsFromModel(m) {
		bounds := card
		bounds.X += g.x
		bounds.Y += g.y - m.scroll
		edges := []hitRegion{
			{X: bounds.X, Y: bounds.Y, W: bounds.W, H: 1},
			{X: bounds.X, Y: bounds.Y + bounds.H - 1, W: bounds.W, H: 1},
			{X: bounds.X, Y: bounds.Y, W: 1, H: bounds.H},
			{X: bounds.X + bounds.W - 1, Y: bounds.Y, W: 1, H: bounds.H},
		}
		for _, overlay := range notificationLayout(o) {
			for _, edge := range edges {
				if regionsOverlap(edge, overlay.Bounds) {
					m.focus, focused = card.ID, true
				}
			}
		}
		if focused {
			break
		}
	}
	if !focused {
		t.Fatal("fixture has no card focus border underneath the task notice")
	}
	m.layout()
	o = m.navigationOptions()
	g = dashboardGeometry(o)
	o.Body, o.BodyTargets = m.pageBody(g.width)
	rows := strings.Split(m.screen, "\n")
	intersects := false
	for _, overlay := range notificationLayout(o) {
		for _, card := range overviewCardRegionsFromModel(m) {
			if card.ID != m.focus {
				continue
			}
			card.X += g.x
			card.Y += g.y - m.scroll
			intersects = intersects || regionsOverlap(card, overlay.Bounds)
		}
		for offset, expected := range strings.Split(overlay.Content, "\n") {
			y := overlay.Bounds.Y + offset
			actual := ansi.Cut(rows[y], overlay.Bounds.X, overlay.Bounds.X+overlay.Bounds.W)
			if ansi.Strip(actual) != ansi.Strip(expected) {
				t.Fatalf("card focus overwrote task notice row %d: got %q, want %q", offset, ansi.Strip(actual), ansi.Strip(expected))
			}
		}
	}
	if !intersects {
		t.Fatal("fixture did not overlap the focused card and task notice")
	}
}
