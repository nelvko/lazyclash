package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestWizardNetworkChecksKeepIndependentSemanticStates(t *testing.T) {
	m := ready(&fakeBackend{})
	m.openNetwork(false)
	m.wizard.versionTest = checkState{Status: "Failed", Detail: "Version information unavailable"}
	m.wizard.fileTest = checkState{Status: "Passed", Detail: "Asset probe passed"}
	doc, _, _ := m.wizardDocument(120)
	screen := strings.Join(doc.lines, "\n")
	for _, sample := range []struct{ value, color string }{
		{"Failed", errorColor}, {"Version information unavailable", errorColor},
		{"Passed", success}, {"Asset probe passed", success},
	} {
		if !strings.Contains(screen, themed(sample.value, sample.color, raised, false)) {
			t.Fatalf("network result %q did not retain its semantic state", sample.value)
		}
	}
}

func TestWizardFocusAndCurrentStepRemainVisibleWithoutColor(t *testing.T) {
	m := ready(&fakeBackend{})
	m.openManagementWizard(true)
	m.noColor = true
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {120, 18}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.wizard.step, m.wizard.focus = stepVersion, "install"
		m.layout()
		plain := ansi.Strip(m.screen)
		if !strings.Contains(plain, "Install Mihomo") {
			t.Fatalf("size %v lost the installation title", size)
		}
		r := findTarget(t, m, "install")
		row := strings.Split(m.screen, "\n")[r.Y]
		if !strings.Contains(ansi.Strip(ansi.Cut(row, r.X, r.X+r.W)), "› [Install]") {
			t.Fatalf("size %v lost the focused footer marker", size)
		}
	}
}

func TestWizardFailureHasOnePrimaryAction(t *testing.T) {
	m := themeSample("failure")
	screen, _ := m.renderWizard("")
	primary := renderButton(m.tr("wizard.overview"), buttonOptions{Kind: buttonPrimary, Surface: raised, Plain: m.buttonPlain()})
	if strings.Contains(screen, primary) {
		t.Fatal("failure page made both retry and return primary actions")
	}
}

func TestWizardFooterFocusHoverAndColorFallbackKeepHitRegionsStable(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 20}, {40, 16}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := themeSample("failure")
			m.abortLookup()
			m.width, m.height = size[0], size[1]
			m.wizard.focus, m.hover = "done", ""
			m.layout()
			want := map[string]hitRegion{"done": findTarget(t, m, "done"), "retry": findTarget(t, m, "retry")}
			for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
				m.colorProfile = profile
				for _, focused := range []string{"done", "retry"} {
					m.wizard.focus, m.hover = focused, "retry"
					m.layout()
					assertCanvas(t, m.View().Content, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
					for id, target := range want {
						if got := findTarget(t, m, id); got != target {
							t.Fatalf("%s moved after focus/hover/color change: got %+v, want %+v", id, got, target)
						}
					}
				}
			}
		})
	}
}

func TestWizardCancellingLeavesNoClickableCancelButton(t *testing.T) {
	m := themeSample("disabled")
	m.width, m.height = 120, 32
	m.layout()
	if hasAction(m.targets, "cancel") || slices.Contains(m.wizard.controls, "cancel") {
		t.Fatal("cancellation cleanup retained an executable or tabbable Cancel button")
	}
	if !strings.Contains(ansi.Strip(m.screen), m.tr("wizard.cancelling")) {
		t.Fatal("cancellation cleanup lost its explicit progress state")
	}
	cancelCalls := 0
	m.cancel = func() { cancelCalls++ }
	if m.wizardActivate("cancel") != nil || cancelCalls != 0 {
		t.Fatal("cancellation cleanup accepted a duplicate Cancel action")
	}
}
