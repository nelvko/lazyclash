package tui

import (
	"fmt"
	"math"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

const logo = "la💤yClash"

const (
	logoIntroDelay    = 200 * time.Millisecond
	logoIntroInterval = 40 * time.Millisecond
	logoIntroFrames   = 20
)

type logoIntroState struct {
	sized, colored, loaded bool
	used, waiting          bool
	frame                  int
}

type logoAnimationFrame struct {
	center, bubble string
	offset         int
	dim, wake      bool
	duration       time.Duration
}

// Every center occupies the emoji's two cells, keeping both halves of the
// wordmark fixed. The two dream bubbles follow after it has fallen asleep.
var logoFrames = [...]logoAnimationFrame{
	{center: "💤"},
	{center: "!!", wake: true, duration: 120 * time.Millisecond},
	{center: "..", duration: 240 * time.Millisecond},
	{center: "z ", duration: 110 * time.Millisecond},
	{center: " Z", duration: 130 * time.Millisecond},
	{center: "💤", duration: 100 * time.Millisecond},
	{center: "💤", bubble: "z", duration: 150 * time.Millisecond},
	{center: "💤", bubble: "Z", offset: 1, duration: 150 * time.Millisecond},
	{center: "💤", bubble: "Z", offset: 2, duration: 120 * time.Millisecond},
	{center: "💤", duration: 80 * time.Millisecond},
	{center: "💤", bubble: "z", dim: true, duration: 120 * time.Millisecond},
	{center: "💤", bubble: "Z", offset: 1, dim: true, duration: 120 * time.Millisecond},
	{center: "💤", duration: 160 * time.Millisecond},
}

type logoPulseMsg struct{ generation uint64 }
type logoIntroMsg struct{ generation uint64 }

func logoFrameAt(index int) logoAnimationFrame {
	if index < 0 || index >= len(logoFrames) {
		return logoFrames[0]
	}
	return logoFrames[index]
}

func logoText(background string, dimmed bool) string {
	return logoTextFrame(0, 0, background, dimmed)
}

func logoTextFrame(index, intro int, background string, dimmed bool) string {
	frame := logoFrameAt(index)
	color := accent
	if frame.wake {
		color = focus
	} else if index == 0 {
		frame.center = logoIntroCenter(intro)
		color = logoIntroColor(intro)
	}
	return themed(lipgloss.NewStyle().Bold(true).Render("la"+frame.center+"yClash"), color, background, dimmed)
}

func logoIntroCenter(frame int) string {
	switch {
	case frame >= 1 && frame <= 6:
		return "z "
	case frame >= 7 && frame <= 13:
		return " Z"
	default:
		return "💤"
	}
}

func logoIntroColor(frame int) string {
	if frame <= 0 || frame >= logoIntroFrames {
		return accent
	}
	// Ease to a lighter cyan and back without changing the wordmark's cells.
	amount := math.Pow(math.Sin(math.Pi*float64(frame)/logoIntroFrames), 2)
	r, g, b, _ := lipgloss.Color(accent).RGBA()
	pr, pg, pb, _ := lipgloss.Color("#c5efff").RGBA()
	mix := func(base, peak uint32) uint8 {
		return uint8(math.Round((float64(base) + (float64(peak)-float64(base))*amount) / 257))
	}
	return fmt.Sprintf("#%02x%02x%02x", mix(r, pr), mix(g, pg), mix(b, pb))
}

func (m Model) logoVisible() bool {
	o := m.dashboardOptions()
	return dashboardGeometry(o).sidebar && !o.Dim && !m.menu.open && !m.busy && !m.quitting && m.ctx.Err() == nil
}

func (m Model) logoIntroVisible() bool {
	return m.logoVisible() && !m.noColor && m.colorProfile >= colorprofile.ANSI256
}

// Wait for the actual terminal size/profile and initial load; New's provisional
// geometry must not consume the one startup opportunity. Hidden sidebars skip it.
func (m *Model) syncLogoIntro() tea.Cmd {
	intro := &m.logoIntro
	if intro.used || !intro.sized || !intro.colored || !intro.loaded {
		return nil
	}
	intro.used = true
	if !m.logoIntroVisible() {
		return nil
	}
	intro.waiting = true
	m.logoGeneration++
	return m.logoIntroTick(logoIntroDelay)
}

func (m *Model) stopLogoIntro() {
	if m.logoIntro.waiting || m.logoIntro.frame != 0 {
		m.logoGeneration++
		m.logoIntro.waiting = false
		m.logoIntro.frame = 0
	}
}

func (m *Model) advanceLogoIntro(v logoIntroMsg) tea.Cmd {
	if v.generation != m.logoGeneration || (!m.logoIntro.waiting && m.logoIntro.frame == 0) {
		return nil
	}
	if !m.logoIntroVisible() || m.logoIntro.frame >= logoIntroFrames {
		m.stopLogoIntro()
		return nil
	}
	m.logoIntro.waiting = false
	m.logoIntro.frame++
	return m.logoIntroTick(logoIntroInterval)
}

func (m Model) logoIntroTick(delay time.Duration) tea.Cmd {
	generation := m.logoGeneration
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return logoIntroMsg{generation: generation}
	})
}

func (m *Model) stopLogoPulse() {
	if m.logoFrame != 0 {
		m.logoGeneration++
		m.logoFrame = 0
	}
}

func (m *Model) startLogoPulse() tea.Cmd {
	m.logoIntro.used = true
	m.stopLogoIntro()
	if !m.logoVisible() {
		m.stopLogoPulse()
		return nil
	}
	m.logoGeneration++
	m.logoFrame = 1
	return m.logoTick()
}

func (m *Model) advanceLogoPulse(v logoPulseMsg) tea.Cmd {
	if v.generation != m.logoGeneration || m.logoFrame == 0 {
		return nil
	}
	if !m.logoVisible() || m.logoFrame >= len(logoFrames)-1 {
		m.stopLogoPulse()
		return nil
	}
	m.logoFrame++
	return m.logoTick()
}

func (m Model) logoTick() tea.Cmd {
	generation := m.logoGeneration
	return tea.Tick(logoFrameAt(m.logoFrame).duration, func(time.Time) tea.Msg {
		return logoPulseMsg{generation: generation}
	})
}
