package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestWizardNetworkChecksKeepIndependentSemanticStates(t *testing.T) {
	m := ready(&fakeBackend{})
	m.openNetwork(false)
	m.wizard.versionTest = checkState{Status: "Failed", Detail: "API rate limit"}
	m.wizard.fileTest = checkState{Status: "Passed", Detail: "Asset probe passed"}
	doc, _, _ := m.wizardDocument(120)
	screen := strings.Join(doc.lines, "\n")
	for _, sample := range []struct{ value, color string }{
		{"Failed", errorColor}, {"API rate limit", errorColor},
		{"Passed", success}, {"Asset probe passed", success},
	} {
		if !strings.Contains(screen, themed(sample.value, sample.color, raised, false)) {
			t.Fatalf("network result %q did not retain its semantic state", sample.value)
		}
	}
}

func TestWizardFocusAndCurrentStepRemainVisibleWithoutColor(t *testing.T) {
	m := ready(&fakeBackend{})
	m.openWizard(false)
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {120, 18}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.wizard.step, m.wizard.focus = stepVersion, "continue"
		m.layout()
		plain := ansi.Strip(m.screen)
		if !strings.Contains(plain, "[2 Version]") {
			t.Fatalf("size %v lost current-step marker", size)
		}
		r := findTarget(t, m, "continue")
		row := strings.Split(m.screen, "\n")[r.Y]
		if !strings.Contains(ansi.Strip(ansi.Cut(row, r.X, r.X+r.W)), "› [ Continue ]") {
			t.Fatalf("size %v lost the focused footer marker", size)
		}
	}
}

func TestWizardFailureHasOnePrimaryAction(t *testing.T) {
	m := themeSample("failure")
	screen, _ := m.renderWizard("")
	if strings.Contains(screen, themed("  [ Back to overview ]", primaryButtonForeground, primaryButtonBackground, false)) {
		t.Fatal("failure page made both retry and return primary actions")
	}
}
