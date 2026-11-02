package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func newLogoIntroModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	return New(context.Background(), &fakeBackend{}, "en")
}

func logoIntroInitialResult() resultMsg {
	return resultMsg{id: 1, hasSnapshot: true, snapshot: app.Snapshot{GuideDismissed: true}}
}

func waitingLogoIntro(t *testing.T) Model {
	t.Helper()
	m := newLogoIntroModel(t)
	for _, msg := range []tea.Msg{logoIntroInitialResult(), tea.WindowSizeMsg{Width: 120, Height: 24}, tea.ColorProfileMsg{Profile: colorprofile.TrueColor}} {
		m, _ = update(m, msg)
	}
	if !m.logoIntro.used || !m.logoIntro.waiting || m.logoIntro.frame != 0 {
		t.Fatalf("intro did not wait after initial messages: %+v", m.logoIntro)
	}
	return m
}

func TestSidebarLogoIntroWaitsForAllInitialMessagesInAnyOrder(t *testing.T) {
	for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			m := newLogoIntroModel(t)
			messages := []tea.Msg{tea.WindowSizeMsg{Width: 120, Height: 24}, tea.ColorProfileMsg{Profile: colorprofile.TrueColor}, logoIntroInitialResult()}
			for index, event := range order {
				m, _ = update(m, messages[event])
				if m.logoFrame != 0 || m.logoIntro.frame != 0 {
					t.Fatal("initialization changed logo characters or brightness before the delay")
				}
				if want := index == len(order)-1; m.logoIntro.used != want || m.logoIntro.waiting != want {
					t.Fatalf("after event %d, got intro %+v; ready=%v", event, m.logoIntro, want)
				}
			}
			before, generation := m.screen, m.logoGeneration
			for _, msg := range []tea.Msg{tea.WindowSizeMsg{Width: 120, Height: 24}, tea.ColorProfileMsg{Profile: colorprofile.TrueColor}, logoIntroMsg{generation: generation + 1}} {
				m, _ = update(m, msg)
			}
			if m.screen != before || m.logoIntro.frame != 0 || !m.logoIntro.waiting || m.logoGeneration != generation {
				t.Fatal("duplicate initialization or stale tick disturbed the static delay")
			}
			if m.syncLogoIntro() != nil {
				t.Fatal("initialization scheduled the intro more than once")
			}
		})
	}
}

func TestSidebarLogoIntroDelayAndNaturalCompletionDoNotReplay(t *testing.T) {
	m := newLogoIntroModel(t)
	m, _ = update(m, logoIntroInitialResult())
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	m.logoIntro.colored, m.colorProfile = true, colorprofile.TrueColor
	before := m.screen
	cmd := m.syncLogoIntro()
	if cmd == nil || !m.logoIntro.waiting || m.logoIntro.frame != 0 || m.screen != before {
		t.Fatal("intro did not schedule a static initial delay")
	}
	if logoIntroDelay != 200*time.Millisecond || time.Duration(logoIntroFrames)*logoIntroInterval != 800*time.Millisecond {
		t.Fatal("intro timing changed from a 200ms pause and 800ms breath")
	}
	generation := m.logoGeneration
	msg, ok := cmd().(logoIntroMsg)
	if !ok || msg.generation != generation {
		t.Fatalf("delay returned an unrelated or stale message: %#v", msg)
	}
	for frame := 1; frame <= logoIntroFrames; frame++ {
		m, cmd = update(m, msg)
		if m.logoIntro.waiting || m.logoIntro.frame != frame || cmd == nil || m.logoFrame != 0 {
			t.Fatalf("intro frame %d did not advance independently: %+v", frame, m.logoIntro)
		}
	}
	if cmd := m.advanceLogoIntro(msg); cmd != nil || m.logoIntro.waiting || m.logoIntro.frame != 0 || !m.logoIntro.used {
		t.Fatal("completed intro retained a timer or replay opportunity")
	}
	m.layout()
	for _, event := range []string{"page", "resize", "overlay", "restore"} {
		switch event {
		case "page":
			m, _ = click(m, findTarget(t, m, "nav.profiles"))
		case "resize":
			m, _ = update(m, tea.WindowSizeMsg{Width: 121, Height: 25})
		case "overlay":
			m.helpOpen = true
			m, _ = update(m, struct{}{})
		case "restore":
			m.helpOpen = false
			m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 24})
		}
		m, _ = update(m, msg)
		if m.logoIntro.waiting || m.logoIntro.frame != 0 || m.syncLogoIntro() != nil {
			t.Fatalf("%s replayed a completed intro", event)
		}
	}
}

func TestSidebarLogoIntroCancelsWhileWaitingOrPlaying(t *testing.T) {
	for _, playing := range []bool{false, true} {
		for _, event := range []string{"click", "resize", "page", "overlay", "busy", "color downgrade"} {
			t.Run(fmt.Sprintf("playing=%v/%s", playing, event), func(t *testing.T) {
				m := waitingLogoIntro(t)
				generation := m.logoGeneration
				if playing {
					m, _ = update(m, logoIntroMsg{generation: generation})
					if m.logoIntro.frame != 1 {
						t.Fatal("intro did not begin playing")
					}
				}
				switch event {
				case "click":
					m, _ = click(m, findTarget(t, m, "logo"))
					if m.logoFrame != 1 || !strings.Contains(ansi.Strip(m.screen), "la!!yClash") {
						t.Fatal("logo click did not take priority over the intro")
					}
				case "resize":
					m, _ = update(m, tea.WindowSizeMsg{Width: 121, Height: 24})
				case "page":
					m, _ = click(m, findTarget(t, m, "nav.profiles"))
					if m.page != "profiles" {
						t.Fatal("test did not change the page")
					}
				case "overlay":
					m.helpOpen = true
					m, _ = update(m, struct{}{})
				case "busy":
					m.busy = true
					m, _ = update(m, struct{}{})
				case "color downgrade":
					m, _ = update(m, tea.ColorProfileMsg{Profile: colorprofile.ANSI})
				}
				if m.logoIntro.waiting || m.logoIntro.frame != 0 || !m.logoIntro.used || m.logoGeneration == generation {
					t.Fatalf("%s did not immediately cancel the intro: %+v", event, m.logoIntro)
				}
				pulse := m.logoFrame
				m, _ = update(m, logoIntroMsg{generation: generation})
				if m.logoIntro.frame != 0 || m.logoFrame != pulse {
					t.Fatal("old intro tick changed the canceled or clicked animation")
				}
				m.helpOpen, m.busy = false, false
				m, _ = update(m, tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
				m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 24})
				if m.logoIntro.waiting || m.logoIntro.frame != 0 || m.syncLogoIntro() != nil {
					t.Fatal("restoring the sidebar replayed the canceled intro")
				}
			})
		}
	}
}

func TestSidebarLogoIntroSkipsIneligibleStartupWithoutCatchingUp(t *testing.T) {
	for _, reason := range []string{"NO_COLOR", "ANSI16", "ASCII", "compact", "short", "overlay"} {
		t.Run(reason, func(t *testing.T) {
			m := newLogoIntroModel(t)
			if reason == "NO_COLOR" {
				t.Setenv("NO_COLOR", "1")
				m = New(context.Background(), &fakeBackend{}, "en")
			}
			m, _ = update(m, logoIntroInitialResult())
			size := tea.WindowSizeMsg{Width: 120, Height: 24}
			profile := colorprofile.TrueColor
			switch reason {
			case "ANSI16":
				profile = colorprofile.ANSI
			case "ASCII":
				profile = colorprofile.ASCII
			case "compact":
				size.Width = 80
			case "short":
				size.Height = 18
			case "overlay":
				m.helpOpen = true
			}
			m, _ = update(m, size)
			m, _ = update(m, tea.ColorProfileMsg{Profile: profile})
			if !m.logoIntro.used || m.logoIntro.waiting || m.logoIntro.frame != 0 {
				t.Fatalf("ineligible startup did not skip the intro: %+v", m.logoIntro)
			}
			m.helpOpen, m.noColor = false, false
			m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 24})
			m, _ = update(m, tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
			m, _ = update(m, logoIntroMsg{generation: m.logoGeneration})
			if m.logoIntro.waiting || m.logoIntro.frame != 0 || m.syncLogoIntro() != nil {
				t.Fatal("improved terminal conditions caught up a skipped intro")
			}
		})
	}
}

func TestSidebarLogoIntroInitialFailureDoesNotReplayAfterRefresh(t *testing.T) {
	m := newLogoIntroModel(t)
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	m, _ = update(m, tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
	m, _ = update(m, resultMsg{id: 1, err: errors.New("initial snapshot unavailable")})
	if !m.logoIntro.loaded || !m.logoIntro.used || m.logoIntro.waiting || m.logoIntro.frame != 0 {
		t.Fatalf("failed initial snapshot did not consume the intro: %+v", m.logoIntro)
	}
	m.refresh()
	m, _ = update(m, resultMsg{id: m.id, hasSnapshot: true, snapshot: app.Snapshot{GuideDismissed: true}})
	if !m.snapshotReady || m.busy || m.logoIntro.waiting || m.logoIntro.frame != 0 || m.syncLogoIntro() != nil {
		t.Fatal("successful refresh replayed the failed startup intro")
	}
}

func TestSidebarLogoIntroKeepsWordmarkFixedWhileGrowingSleepyCenter(t *testing.T) {
	centers := []string{
		"💤",
		"z ", "z ", "z ", "z ", "z ", "z ",
		" Z", " Z", " Z", " Z", " Z", " Z", " Z",
		"💤", "💤", "💤", "💤", "💤", "💤", "💤",
	}
	for frame, center := range centers {
		if got := logoIntroCenter(frame); got != center || ansi.StringWidth(got) != 2 {
			t.Fatalf("intro frame %d center = %q, want two cells %q", frame, got, center)
		}
	}
	for _, frame := range []int{-10, -1, logoIntroFrames + 1, 100} {
		if got := logoIntroCenter(frame); got != "💤" {
			t.Fatalf("invalid intro frame %d center = %q, want the resting emoji", frame, got)
		}
	}
	if logoIntroColor(0) != accent || logoIntroColor(logoIntroFrames) != accent || logoIntroColor(logoIntroFrames/2) != "#c5efff" {
		t.Fatal("intro did not start and finish at the accent around its pale cyan peak")
	}
	brightness := func(frame int) uint32 {
		r, g, b, _ := lipgloss.Color(logoIntroColor(frame)).RGBA()
		return r + g + b
	}
	for frame := 1; frame <= logoIntroFrames; frame++ {
		previous, current := brightness(frame-1), brightness(frame)
		if frame <= logoIntroFrames/2 && current < previous || frame > logoIntroFrames/2 && current > previous {
			t.Fatalf("brightness reversed direction at frame %d", frame)
		}
		if logoIntroColor(frame) != logoIntroColor(logoIntroFrames-frame) {
			t.Fatalf("intro faded out differently from its rise at frame %d", frame)
		}
	}
	for _, size := range [][2]int{{100, 20}, {120, 24}, {160, 45}} {
		o := dashboardOptions{Width: size[0], Height: size[1], Page: "overview", Focus: "nav.overview"}
		baseline, _, _ := renderDashboard(o)
		baseRows := strings.Split(baseline, "\n")
		basePlain := strings.Split(ansi.Strip(baseline), "\n")
		headerY := dashboardGeometry(o).headerY
		logoX := strings.Index(basePlain[headerY], "la")
		if logoX < 0 {
			t.Fatal("baseline sidebar wordmark is missing")
		}
		for frame, center := range centers {
			o.LogoIntroFrame = frame
			screen, hits, _ := renderDashboard(o)
			assertCanvas(t, screen, o.Width, o.Height)
			assertRegions(t, hits, o.Width, o.Height)
			rows := strings.Split(ansi.Strip(screen), "\n")
			if want := "la" + center + "yClash"; ansi.Cut(rows[headerY], logoX, logoX+10) != want {
				t.Fatalf("%dx%d frame %d moved or widened the wordmark: %q", size[0], size[1], frame, rows[headerY])
			}
			if ansi.Cut(rows[headerY], 0, logoX) != ansi.Cut(basePlain[headerY], 0, logoX) || ansi.Cut(rows[headerY], logoX+10, o.Width) != ansi.Cut(basePlain[headerY], logoX+10, o.Width) {
				t.Fatalf("frame %d changed cells outside the fixed wordmark", frame)
			}
			for row, value := range strings.Split(screen, "\n") {
				if row != headerY && value != baseRows[row] {
					t.Fatalf("frame %d added a bubble or changed another row %d", frame, row)
				}
			}
			if frame == logoIntroFrames/2 && screen == baseline {
				t.Fatal("peak frame did not visibly change the wordmark color")
			}
			if (frame == 0 || frame == logoIntroFrames) && screen != baseline {
				t.Fatal("intro did not restore the original sidebar styling")
			}
		}
	}
}
