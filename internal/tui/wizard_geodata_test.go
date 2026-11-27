package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestInstallResultShowsCommittedCoreAfterPartialGeoCancellation(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		m := ready(&fakeBackend{})
		m.locale = i18n.New(lang)
		m.width, m.height = 140, 40
		result := app.InstallResult{
			InstalledVersion: "v1.19.33",
			GeoData:          &app.GeoPreparation{Prepared: []string{"GeoSite.dat"}, Cancelled: true},
		}
		m.handleWizardResult(wizardMsg{kind: "install", result: result})
		modal, _, _ := m.wizardDocument(120)
		inline, _ := m.inlineInstallBody(120)
		for _, output := range []string{m.message, strings.Join(modal.lines, "\n"), inline} {
			plain := ansi.Strip(output)
			if !strings.Contains(plain, "GeoSite.dat") || !strings.Contains(plain, m.tr("geo.preparation.cancelled")) {
				t.Fatalf("%s omitted the partial result: %s", lang, plain)
			}
		}
		if m.wizard.err != "" || m.wizard.focus != "done" {
			t.Fatal("optional preparation sent an installed core to the installation retry flow")
		}
	}
}
