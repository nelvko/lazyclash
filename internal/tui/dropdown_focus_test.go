package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func TestSettingsDropdownShowsPendingChoiceAcrossColorProfiles(t *testing.T) {
	for _, tc := range []struct {
		name, theme string
		profile     colorprofile.Profile
		wantCursor  bool
	}{
		{"Mist ANSI16", app.ThemeLight, colorprofile.ANSI, true},
		{"Mariana ANSI16", app.ThemeDark, colorprofile.ANSI, false},
		{"Mist ANSI256", app.ThemeLight, colorprofile.ANSI256, false},
		{"Mariana truecolor", app.ThemeDark, colorprofile.TrueColor, false},
		{"No color", app.ThemeDark, colorprofile.ASCII, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
			directionCleanup(t, &m)
			m.theme, m.colorProfile, m.noColor = tc.theme, tc.profile, tc.profile == colorprofile.ASCII
			m.page, m.focus, m.width, m.height = "settings", "settings.language", 120, 32
			m.layout()
			ownerMarker := func() bool {
				trigger := findTarget(t, m, "settings.language")
				line := strings.Split(m.View().Content, "\n")[trigger.Y]
				caption := ansi.Strip(ansi.Cut(line, 0, trigger.X))
				return strings.Contains(strings.Join(strings.Fields(caption), " "), "› "+m.tr("dashboard.language.title"))
			}
			if m.noColor && !ownerMarker() {
				t.Fatal("closed language selector has no keyboard focus marker")
			}
			m, _ = update(m, special(tea.KeyEnter))
			if !m.menu.open || m.noColor && ownerMarker() {
				t.Fatal("opening the menu did not transfer visible focus to the popup")
			}
			m, _ = update(m, special(tea.KeyHome))
			popupRow := func(index int) string {
				b := m.menu.bounds
				row := strings.Split(m.View().Content, "\n")[b.Y+1+index]
				return ansi.Cut(row, b.X, b.X+b.W)
			}
			before := popupRow(1)
			m, _ = update(m, special(tea.KeyDown))
			after := popupRow(1)
			if m.menu.selected != 1 || before == after || strings.Contains(ansi.Strip(after), "›") != tc.wantCursor ||
				strings.Contains(ansi.Strip(popupRow(0)), "›") {
				t.Fatalf("arrow navigation did not indicate the pending option: before %q, after %q", before, after)
			}
			m, _ = update(m, special(tea.KeyEscape))
			if m.menu.open || m.focus != "settings.language" || m.noColor && !ownerMarker() {
				t.Fatal("closing the menu did not restore focus to its field")
			}
		})
	}
}
