package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type fakeBackend struct {
	snapshot    app.Snapshot
	snapshotErr error
	install     func(context.Context, string, func(string)) error
	add         func(context.Context, string, string) error
}

func (f *fakeBackend) Snapshot(context.Context) (app.Snapshot, error) {
	return f.snapshot, f.snapshotErr
}
func (f *fakeBackend) Install(c context.Context, s string, p func(string)) error {
	if f.install != nil {
		return f.install(c, s, p)
	}
	return nil
}
func (f *fakeBackend) Add(c context.Context, n, s string) error {
	if f.add != nil {
		return f.add(c, n, s)
	}
	return nil
}
func (f *fakeBackend) Apply(context.Context, string) error          { return nil }
func (f *fakeBackend) Control(context.Context, string) error        { return nil }
func (f *fakeBackend) Select(context.Context, string, string) error { return nil }

func update(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}
func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func ready(b *fakeBackend) Model {
	m := New(context.Background(), b)
	m, _ = update(m, resultMsg{id: 1, snapshot: b.snapshot, hasSnapshot: true})
	return m
}
func actionCmd(t *testing.T, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatal("expected operation batch")
	}
	return batch[0]
}

func TestFirstStartAndProfiles(t *testing.T) {
	b := &fakeBackend{}
	m := ready(b)
	if view := m.View(); !strings.Contains(view, "Mihomo is not installed") || !strings.Contains(view, "Download official") {
		t.Fatal(view)
	}
	b.snapshot = app.Snapshot{Installed: true, Profiles: []string{"default", "work", "work"}}
	m = ready(b)
	if got := strings.Join(m.profiles(), ","); got != "default,work" {
		t.Fatal(got)
	}
	m, _ = update(m, key("2"))
	if !strings.Contains(m.View(), "default") {
		t.Fatal(m.View())
	}
}

func TestBusyCancelWaitsForCleanupAndPreventsDuplicate(t *testing.T) {
	started := make(chan struct{})
	cleaned := make(chan struct{})
	b := &fakeBackend{install: func(ctx context.Context, _ string, p func(string)) error {
		close(started)
		p("verifying checksum")
		<-ctx.Done()
		close(cleaned)
		return ctx.Err()
	}}
	m := ready(b)
	m, cmd := update(m, key("i"))
	if !m.busy {
		t.Fatal("operation must be busy before command starts")
	}
	run := actionCmd(t, cmd)
	result := make(chan tea.Msg, 1)
	go func() { result <- run() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("install not started")
	}
	oldID := m.id
	m, cmd = update(m, key("i"))
	if cmd != nil || m.id != oldID {
		t.Fatal("duplicate operation accepted")
	}
	m, cmd = update(m, key("q"))
	if cmd != nil || !m.busy || !m.quitting {
		t.Fatal("quit must await cleanup")
	}
	var done tea.Msg
	select {
	case done = <-result:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not propagate")
	}
	select {
	case <-cleaned:
	default:
		t.Fatal("cleanup not complete")
	}
	m, cmd = update(m, done)
	if m.busy || cmd == nil {
		t.Fatal("expected quit only after result")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected QuitMsg")
	}
}

func TestParentCancellationWaitsAndStaleResultsAreIgnored(t *testing.T) {
	m := ready(&fakeBackend{})
	m, cmd := update(m, key("i"))
	_ = cmd
	id := m.id
	m, cmd = update(m, parentCanceledMsg{})
	if cmd != nil || !m.quitting || !m.busy {
		t.Fatal("parent cancellation bypassed cleanup")
	}
	m, _ = update(m, resultMsg{id: id - 1, snapshot: app.Snapshot{Installed: true}, hasSnapshot: true})
	if !m.busy || m.snapshot.Installed {
		t.Fatal("stale result changed current state")
	}
	m, cmd = update(m, resultMsg{id: id, err: context.Canceled})
	if m.busy || cmd == nil {
		t.Fatal("current result must finish pending exit")
	}
}

func TestRefreshFailureKeepsLastCompleteSnapshot(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Version: "known"}, snapshotErr: errors.New("lock timeout")}
	m := ready(b)
	m, cmd := update(m, key("r"))
	m, _ = update(m, actionCmd(t, cmd)())
	if !m.snapshot.Installed || m.snapshot.Version != "known" {
		t.Fatal("failed refresh erased installed state")
	}
	if !strings.Contains(m.failure, "refresh current state") || !strings.Contains(m.failure, "lock timeout") {
		t.Fatal(m.failure)
	}
}

func TestSubscriptionSourceIsHiddenAndEscDoesNotSubmit(t *testing.T) {
	calls := 0
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true}, add: func(context.Context, string, string) error { calls++; return nil }}
	m := ready(b)
	m, _ = update(m, key("a"))
	m.input.SetValue("work")
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.input.SetValue("https://secret.example/token-top-secret")
	m.layout()
	if strings.Contains(m.View(), "top-secret") || strings.Contains(m.View(), "secret.example") {
		t.Fatal("subscription URL leaked")
	}
	m, _ = update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.form != "" || calls != 0 {
		t.Fatal("cancel submitted form")
	}
}

func TestNodeNamesSanitizedAndViewportScrolls(t *testing.T) {
	profiles := make([]string, 30)
	for i := range profiles {
		profiles[i] = strings.Repeat("x", i+1)
	}
	m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true, Profiles: profiles, Groups: []mihomo.Group{{Name: "safe\x1b]52;c;payload\a", Type: "Selector", Now: "DIRECT", All: []string{"DIRECT"}}}}})
	m, _ = update(m, tea.WindowSizeMsg{Width: 25, Height: 8})
	m, _ = update(m, key("2"))
	for range 20 {
		m, _ = update(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.viewport.YOffset == 0 {
		t.Fatal("list did not follow cursor")
	}
	m, _ = update(m, key("3"))
	if strings.Contains(m.View(), "payload") {
		t.Fatal("OSC payload leaked")
	}
	if strings.Count(m.View(), "\n")+1 > 8 {
		t.Fatal("view exceeds terminal height")
	}
	for _, line := range strings.Split(m.View(), "\n") {
		if lipgloss.Width(line) > 25 {
			t.Fatalf("line too wide: %q", line)
		}
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 12, Height: 2})
	if strings.Count(m.View(), "\n")+1 > 2 {
		t.Fatal("tiny terminal exceeds height")
	}
}
