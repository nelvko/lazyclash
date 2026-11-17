package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestEmptyStateCentersTextAndItsActionTogether(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{100, 30}, {76, 16}, {36, 6}, {20, 3}} {
			m := Model{locale: i18n.New(language), focus: "fixture.retry"}
			b := m.emptyState(size[0], size[1], emptyStateOptions{
				Title: m.tr("empty.read_failed"), Detail: "fixture: 中文诊断",
				ActionID: "fixture.retry", ActionLabel: m.tr("empty.retry"), Error: true,
			})
			if len(b.lines) != size[1] || len(b.targets) != 1 {
				t.Fatalf("%s %v lost its viewport or action: %+v", language, size, b)
			}
			first, last := -1, -1
			for y, row := range b.lines {
				if ansi.StringWidth(row) != size[0] {
					t.Fatalf("%s %v row escaped its viewport: %q", language, size, row)
				}
				plain := ansi.Strip(row)
				if strings.TrimSpace(plain) == "" {
					continue
				}
				if first < 0 {
					first = y
				}
				last = y
				left := len(plain) - len(strings.TrimLeft(plain, " "))
				right := len(plain) - len(strings.TrimRight(plain, " "))
				// The button reserves two leading cells for its focus marker.
				if max(left-right, right-left) > 2 {
					t.Fatalf("%s %v row is not centered: %q", language, size, plain)
				}
			}
			if max(first-(size[1]-1-last), (size[1]-1-last)-first) > 1 {
				t.Fatalf("%s %v block is not vertically centered", language, size)
			}
			hit := b.targets[0]
			row := ansi.Strip(b.lines[hit.Y])
			if !strings.Contains(ansi.Cut(row, hit.X, hit.X+hit.W), m.tr("empty.retry")) {
				t.Fatal("centered action no longer matches its hit region")
			}
		}
	}
}

func TestEmptyStateKeepsRecoveryVisibleInShortViewports(t *testing.T) {
	m := Model{locale: i18n.New(i18n.English)}
	for _, height := range []int{1, 2, 3, 4} {
		b := m.emptyState(24, height, emptyStateOptions{
			Title: "Could not read data", Detail: strings.Repeat("long diagnostic ", 40),
			ActionID: "retry", ActionLabel: "Retry",
		})
		if len(b.lines) != height || len(b.targets) != 1 || b.targets[0].Y >= height {
			t.Fatal("diagnostic pushed the recovery action out of view", height)
		}
	}
	m.busy = true
	if b := m.emptyState(30, 10, emptyStateOptions{Title: "Unavailable", ActionID: "retry", ActionLabel: "Retry"}); len(b.targets) != 0 {
		t.Fatal("busy empty state exposed an enabled action")
	}
}

func TestInitialSnapshotFailureIsNotAnEmptyOrLoadingLivePage(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, page := range []string{"rules", "connections", "logs", "proxies"} {
			b := &fakeBackend{snapshotErr: errors.New("fixture initial state read failed")}
			m := ready(b)
			m.locale, m.page, m.width, m.height = i18n.New(language), page, 80, 24
			m.notification.items = nil
			m.layout()
			if !strings.Contains(ansi.Strip(m.screen), m.tr("empty.read_failed")) || !hasAction(m.targets, "snapshot.retry") {
				t.Fatalf("%s %s hid an initial read failure:\n%s", language, page, ansi.Strip(m.screen))
			}
			if page == "logs" {
				status, _ := m.logsStatus()
				if status != m.tr("empty.read_failed") || m.pageHeader().summary != status || hasAction(m.targets, "logs.retry") {
					t.Fatal("initial log read failure advertised an active or connecting stream")
				}
			}
			m.focus = "snapshot.retry"
			if m.contextHelp() != m.tr("navigation.default") {
				t.Fatal("snapshot retry advertised an unrelated page action")
			}
			cmd := m.activate("snapshot.retry")
			if cmd == nil || !m.busy {
				t.Fatal("initial read retry did not start the snapshot request")
			}
			if state, _ := m.coreEmptyState(); state.Title != m.tr("empty.loading") || state.ActionID != "" {
				t.Fatal("pending initial retry retained its failure action")
			}
			b.snapshotErr = nil
			b.snapshot = app.Snapshot{Installed: true}
			m, _ = update(m, actionCmd(t, cmd)())
			if !m.snapshotReady || m.activate("snapshot.retry") != nil {
				t.Fatal("snapshot retry failed to recover or stayed enabled after loading")
			}
			m.stopRuntime()
			m.stopRules()
			m.stopCoreStatus()
		}
	}
}
