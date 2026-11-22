package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestConnectionsCloseAllReplacesRefreshAndDefaultsToCancel(t *testing.T) {
	calls := 0
	m := connectionsModel(t, &fakeBackend{closeAllConnections: func(context.Context) error { calls++; return nil }})
	if !m.pageHeader().enabled("conn.close-all") || m.pageHeader().owns("conn.retry") || hasAction(m.targets, "conn.retry") {
		t.Fatal("connection header must replace refresh with enabled Close all")
	}
	for _, dismiss := range []tea.KeyPressMsg{special(tea.KeyEnter), special(tea.KeyEscape), key("n")} {
		m, _ = click(m, findTarget(t, m, "conn.close-all"))
		if !m.connections.confirming || m.focus != "conn.cancel" || m.busy || calls != 0 {
			t.Fatal("Close all must first show a confirmation focused on Cancel")
		}
		m, _ = update(m, dismiss)
		if m.connections.confirming || m.focus != "conn.close-all" || m.busy || calls != 0 {
			t.Fatal("default Enter, Esc and n must dismiss without closing connections")
		}
	}
}

func TestConnectionsCloseAllConfirmsOnceWithoutRestartingMonitor(t *testing.T) {
	all, single := 0, 0
	m := connectionsModel(t, &fakeBackend{
		closeAllConnections: func(context.Context) error { all++; return nil },
		closeConnection:     func(context.Context, string, time.Time) error { single++; return nil },
	})
	generation := m.runtime.generation
	m, _ = click(m, findTarget(t, m, "conn.close-all"))
	m, cmd := update(m, key("y"))
	if !m.busy || m.connections.confirming {
		t.Fatal("explicit confirmation must start an asynchronous operation")
	}
	work := actionCmd(t, cmd)
	m, _ = update(m, key("y"))
	if duplicate := m.connectionActivate("conn.close-all"); duplicate != nil || m.connections.confirming {
		t.Fatal("busy operation allowed a second Close all confirmation")
	}
	result := work()
	if all != 1 || single != 0 {
		t.Fatalf("close APIs called incorrectly: all=%d single=%d", all, single)
	}
	m, _ = update(m, result)
	if m.busy || m.failure != "" || !m.runtime.active || m.runtime.generation != generation {
		t.Fatal("successful Close all must leave the live monitor running")
	}
}

func TestConnectionsCloseAllUsesLiveScopeOutsideSearchAndPause(t *testing.T) {
	calls := 0
	m := connectionsModel(t, &fakeBackend{closeAllConnections: func(context.Context) error { calls++; return nil }})
	m.connections.search.SetValue("one.example.invalid")
	m.reconcileConnections()
	m.connectionActivate("conn.pause")
	m.runtime.snapshot.Connections.Connections = []mihomo.Connection{fixtureConnection("unseen", "unseen.example.invalid", 5)}
	m.layout()
	if len(m.filteredConnections()) != 1 || !m.connections.paused || !m.pageHeader().enabled("conn.close-all") {
		t.Fatal("fixture must show a frozen filtered row while a different live connection exists")
	}
	m, _ = click(m, findTarget(t, m, "conn.close-all"))
	m, cmd := update(m, key("y"))
	if m.connections.paused || m.connections.frozen != nil || m.connections.search.Value() != "one.example.invalid" {
		t.Fatal("Close all must resume live display while preserving the search")
	}
	_ = actionCmd(t, cmd)()
	if calls != 1 {
		t.Fatal("Close all did not invoke the batch API for the live scope")
	}
}

func TestConnectionsCloseAllUnavailableAndEmptyAreDisabled(t *testing.T) {
	for _, state := range []string{"empty", "unavailable", "unreceived", "nil-snapshot", "frozen-only"} {
		t.Run(state, func(t *testing.T) {
			calls := 0
			m := connectionsModel(t, &fakeBackend{closeAllConnections: func(context.Context) error { calls++; return nil }})
			switch state {
			case "empty":
				m.runtime.snapshot.Connections.Connections = nil
			case "unavailable":
				m.runtime.snapshot.State = "unavailable"
			case "unreceived":
				m.runtime.received = false
			case "nil-snapshot":
				m.runtime.snapshot.Connections = nil
			case "frozen-only":
				m.connectionActivate("conn.pause")
				m.runtime.snapshot.Connections.Connections = nil
			}
			m.layout()
			if m.pageHeader().enabled("conn.close-all") || hasAction(m.targets, "conn.close-all") {
				t.Fatal("Close all must be disabled when there are no available live connections")
			}
			m.connectionActivate("conn.close-all")
			if m.connections.confirming || m.busy || calls != 0 {
				t.Fatal("direct activation bypassed the disabled action")
			}
		})
	}
}

func TestConnectionsCloseAllRechecksLiveAvailabilityBeforeConfirming(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprint(unavailable), func(t *testing.T) {
			calls := 0
			m := connectionsModel(t, &fakeBackend{closeAllConnections: func(context.Context) error { calls++; return nil }})
			m, _ = click(m, findTarget(t, m, "conn.close-all"))
			if unavailable {
				m.runtime.snapshot.State = "unavailable"
			} else {
				m.runtime.snapshot.Connections.Connections = nil
			}
			m, _ = update(m, key("y"))
			if m.busy || calls != 0 || m.connections.confirming {
				t.Fatal("confirmation executed after all live connections became unavailable")
			}
		})
	}
}

func TestConnectionsCloseAllFailureCanRetry(t *testing.T) {
	attempts := 0
	m := connectionsModel(t, &fakeBackend{closeAllConnections: func(context.Context) error {
		attempts++
		if attempts == 1 {
			return errors.New("controller unavailable")
		}
		return nil
	}})
	for attempt := 1; attempt <= 2; attempt++ {
		m, _ = click(m, findTarget(t, m, "conn.close-all"))
		var cmd tea.Cmd
		m, cmd = update(m, key("y"))
		result := actionCmd(t, cmd)()
		m, _ = update(m, result)
		if m.busy || m.connections.confirming || attempts != attempt {
			t.Fatal("operation did not finish in a retryable state")
		}
		if attempt == 1 {
			if !strings.Contains(m.failure, "controller unavailable") {
				t.Fatal("close failure was not reported")
			}
			for _, target := range m.targets {
				if strings.HasPrefix(target.ID, "notification.close.") {
					m, _ = click(m, target)
					break
				}
			}
		}
		if attempt == 2 && m.failure != "" {
			t.Fatal("successful retry did not clear the failure")
		}
	}
}

func TestConnectionsDetailBlocksCloseAll(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m, _ = update(m, special(tea.KeyEnter))
	m.connectionActivate("conn.close-all")
	if m.connections.confirming || m.connections.detail == nil || hasAction(m.targets, "conn.close-all") {
		t.Fatal("detail allowed a background close-all action")
	}
}

func TestConnectionsCloseAllConfirmationBlocksBackgroundMouseAndBounds(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {20, 7}, {5, 2}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			background := findTarget(t, m, "nav.settings")
			m, _ = click(m, findTarget(t, m, "conn.close-all"))
			m, _ = click(m, background)
			if m.page != "connections" || !m.connections.confirming {
				t.Fatal("Close all confirmation allowed background navigation")
			}
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
			m, _ = update(m, special(tea.KeyEscape))
			if m.connections.confirming || m.busy {
				t.Fatal("confirmation could not be dismissed after resize")
			}
		})
	}
}

func TestConnectionsBulkConfirmationFloatsOverTheScrolledList(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m.width, m.height = 120, 32
	for i := range 40 {
		c := fixtureConnection(fmt.Sprintf("id-%02d", i), fmt.Sprintf("test-%02d.example", i), i)
		m.runtime.snapshot.Connections.Connections = append(m.runtime.snapshot.Connections.Connections, c)
	}
	m.connections.search.SetValue("test-")
	m.connections.selected, m.focus = "id-14", "connection.id-14"
	m.reconcileConnections()
	m.revealConnection(0)
	m.layout()
	scroll := m.scroll
	row := findTarget(t, m, m.focus)
	if scroll == 0 {
		t.Fatal("fixture list must be scrolled")
	}
	m, _ = click(m, findTarget(t, m, "conn.close-all"))
	screen := strings.Split(ansi.Strip(m.screen), "\n")
	if !strings.Contains(screen[row.Y], "test-14.example") || !strings.Contains(ansi.Strip(m.screen), m.tr("connections.confirm_all_title")) {
		t.Fatal("confirmation must overlay the current list without moving its rows")
	}
	cancel := findTarget(t, m, "conn.cancel")
	confirm := findTarget(t, m, "conn.confirm")
	if cancel.Y != confirm.Y || cancel.X+cancel.W >= confirm.X || len(m.targets) != 2 {
		t.Fatal("confirmation buttons should be adjacent and own all click targets")
	}
	for _, msg := range []tea.Msg{key("v"), key("/"), key("1"), tea.MouseWheelMsg{X: 100, Y: 28, Button: tea.MouseWheelDown}, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseRight}} {
		m, _ = update(m, msg)
	}
	if m.page != "connections" || !m.connections.confirming || m.focus != "conn.cancel" || m.scroll != scroll || m.connections.selected != "id-14" || m.connections.search.Value() != "test-" {
		t.Fatal("an underlying control changed while bulk confirmation was open")
	}
	m, _ = click(m, cancel)
	if m.connections.confirming || m.focus != "conn.close-all" || m.scroll != scroll {
		t.Fatal("cancel must return to the header action at the same list offset")
	}
}

func TestConnectionsCloseAllCannotSubmitHiddenConfirmationAfterResize(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m, _ = click(m, findTarget(t, m, "conn.close-all"))
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "conn.confirm" {
		t.Fatal("fixture must start with visible Confirm focused")
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 16, Height: 5})
	if hasAction(m.targets, "conn.confirm") {
		t.Fatal("fixture should hide Confirm")
	}
	m, _ = update(m, key("y"))
	m, _ = update(m, special(tea.KeyEnter))
	if !m.connections.confirming || m.busy {
		t.Fatal("a shortcut or stale focus submitted a hidden destructive action")
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "conn.cancel" {
		t.Fatal("Tab focused a hidden confirmation action")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.connections.confirming || m.busy {
		t.Fatal("keyboard navigation failed to cancel the tiny confirmation")
	}
}

func TestConnectionInspectorCannotActivateHiddenCloseAfterResize(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m, _ = update(m, key("x"))
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "conn.close" {
		t.Fatal("fixture should have the disconnect button focused")
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 16, Height: 5})
	if hasAction(m.targets, "conn.close") {
		t.Fatal("fixture should hide the disconnect button")
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "conn.back" {
		t.Fatal("tabbing in the compact inspector must focus its only visible action")
	}
	m, cmd := update(m, special(tea.KeyEnter))
	if cmd != nil || m.busy || m.connections.detail != nil {
		t.Fatal("Enter should dismiss the compact inspector without closing the connection")
	}
}

type connectionsRefreshProbe struct {
	*fakeBackend
	snapshots int
}

func (b *connectionsRefreshProbe) Snapshot(ctx context.Context) (app.Snapshot, error) {
	b.snapshots++
	return b.fakeBackend.Snapshot(ctx)
}

func TestConnectionsRetiredRefreshKeysCannotReadSnapshotOrRestart(t *testing.T) {
	for _, detail := range []bool{false, true} {
		for _, input := range []tea.KeyPressMsg{key("g"), {Code: 'r', Mod: tea.ModCtrl}} {
			t.Run(fmt.Sprintf("detail=%v/%s", detail, input.String()), func(t *testing.T) {
				b := &connectionsRefreshProbe{fakeBackend: &fakeBackend{}}
				m := connectionsModel(t, b.fakeBackend)
				m.backend = b
				if detail {
					m, _ = update(m, special(tea.KeyEnter))
				}
				generation, operation := m.runtime.generation, m.id
				m, cmd := update(m, input)
				if cmd != nil || m.busy || m.runtime.generation != generation || m.id != operation || b.snapshots != 0 {
					t.Fatal("retired connection refresh shortcut triggered work")
				}
				if cmd := m.connectionActivate("conn.retry"); cmd != nil || m.busy || b.snapshots != 0 {
					t.Fatal("stale connection refresh action remained callable")
				}
			})
		}
	}
}
