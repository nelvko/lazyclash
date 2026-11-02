package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestPageNavigationCardFocusPreservesOverlappingTaskNotice(t *testing.T) {
	m := pageNavigationModel()
	m.snapshot.GuideDismissed = true
	m.focus = "overview.card.dashboard.control.routingMode"
	m.newJob("action")
	defer m.cancel()
	m.message = "Updating subscription; existing connections remain available while this task runs."
	m.syncNotification()
	m.layout()

	o := m.navigationOptions()
	g := dashboardGeometry(o)
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

func TestPageNavigationClosingHelpRevealsFocusAfterRestoringHints(t *testing.T) {
	for _, closeWithMouse := range []bool{false, true} {
		t.Run(fmt.Sprintf("mouse=%v", closeWithMouse), func(t *testing.T) {
			m := pageNavigationModel()
			m.page, m.keyHints.hidden = "proxies", true
			for i := 0; i < 40; i++ {
				m.snapshot.Groups = append(m.snapshot.Groups, mihomo.Group{Name: fmt.Sprintf("Group %02d", i), Type: "Selector", Now: "DIRECT"})
			}
			m.focus = "group.30"
			m.revealContentFocus()
			m.layout()
			if !hasAction(m.targets, m.focus) {
				t.Fatal("fixture did not reveal the focused row")
			}
			m, _ = update(m, key("?"))
			m, _ = click(m, findTarget(t, m, "key-hints.toggle"))
			if !m.helpOpen || m.keyHints.hidden {
				t.Fatal("help did not restore the footer")
			}
			if closeWithMouse {
				m, _ = click(m, findTarget(t, m, "help.close"))
			} else {
				m, _ = update(m, special(tea.KeyEscape))
			}
			if m.helpOpen || m.focus != "group.30" || !hasAction(m.targets, m.focus) {
				t.Fatal("closing help left the restored content focus behind the footer")
			}
		})
	}
}
