package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func assertAuxiliaryCentered(t *testing.T, lines []string, label string, width int) int {
	t.Helper()
	for row, line := range lines {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, label) {
			continue
		}
		left := ansi.StringWidth(plain[:strings.Index(plain, label)])
		if want := (width - ansi.StringWidth(label)) / 2; left != want {
			t.Fatalf("%q starts at %d, want centered at %d", label, left, want)
		}
		return row
	}
	t.Fatalf("missing empty-state title %q:\n%s", label, ansi.Strip(strings.Join(lines, "\n")))
	return -1
}

func TestAuxiliaryProviderEmptyStatesUseTheirContentViewport(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, kind := range []string{"empty", "filtered", "stopped", "failed", "loading"} {
			m, _ := ruleProvidersModel(t, 1)
			m.locale = i18n.New(language)
			m.providers.bodyBounds.H = 12
			m.providers.snapshot.Providers = nil
			want, retry := "empty.providers.none", false
			switch kind {
			case "filtered":
				m.providers.search.SetValue("missing")
				want = "providers.no_matches"
			case "stopped":
				m.providers.snapshot.State = "stopped"
				m.providers.search.SetValue("missing")
				want = "runtime.status.stopped"
			case "failed":
				m.providers.err = errors.New("fixture provider read failed")
				m.providers.search.SetValue("missing")
				want, retry = "empty.providers.read_failed", true
			case "loading":
				m.providers.received = false
				want = "providers.loading"
			}
			doc := m.providersBodyWindow(74, 0, -1)
			if len(doc.lines) != 12 {
				t.Fatalf("%s/%s used %d rows outside the provider viewport", language, kind, len(doc.lines))
			}
			if row := assertAuxiliaryCentered(t, doc.lines, m.tr(want), 74); row <= 0 || row >= 10 {
				t.Fatalf("%s/%s placed its title at row %d", language, kind, row)
			}
			if hasAction(doc.targets, "providers.reload") != retry {
				t.Fatalf("%s/%s advertised the wrong recovery action", language, kind)
			}
			if kind == "failed" && !strings.Contains(ansi.Strip(strings.Join(doc.lines, "\n")), "fixture provider read failed") {
				t.Fatal("provider failure lost its diagnostic")
			}
		}
	}
}

func TestAuxiliaryProfilesKeepBuiltinCardAboveEmptyCustomSpace(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := ready(&fakeBackend{})
		m.page, m.width, m.height, m.locale = "profiles", 80, 24, i18n.New(language)
		width := dashboardGeometry(m.dashboardOptions()).width
		body, targets := m.profilesBody(width)
		lines := strings.Split(body, "\n")
		row := assertAuxiliaryCentered(t, lines, m.tr("empty.profiles.none"), width)
		if !hasAction(targets, "profile.0") || row <= findAuxiliaryLine(lines, m.tr("profiles.builtin_origin")) {
			t.Fatal("empty custom library replaced or covered the built-in card")
		}
		m.snapshotReady = false
		body, targets = m.unavailableProfiles(width)
		lines = strings.Split(body, "\n")
		if row := assertAuxiliaryCentered(t, lines, m.tr("profiles.unavailable"), width); row <= 0 || !hasAction(targets, "page.refresh") {
			t.Fatal("profile read failure lost centered explanation or retry")
		}
		if strings.Contains(ansi.Strip(body), m.tr("empty.profiles.none")) {
			t.Fatal("profile read failure was presented as an empty library")
		}
	}
}

func findAuxiliaryLine(lines []string, label string) int {
	for row, line := range lines {
		if strings.Contains(ansi.Strip(line), label) {
			return row
		}
	}
	return -1
}

func TestAuxiliaryEmptyNotificationsCenterInsidePanelContent(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{80, 24}, {60, 18}} {
			var panel notificationPanel
			panel.show(0)
			locale := i18n.New(language)
			screen, hits := panel.render(nil, size[0], size[1], locale)
			row := assertAuxiliaryCentered(t, strings.Split(screen, "\n"), locale.T("notification.empty"), size[0])
			if want := panel.contentY + (panel.viewport.Height()-1)/2; row != want {
				t.Fatalf("empty notifications at %v used row %d, want %d", size, row, want)
			}
			assertCanvas(t, screen, size[0], size[1])
			assertRegions(t, hits, size[0], size[1])
		}
	}
}

func TestAuxiliaryEmptyPanelsDiscardOldListScroll(t *testing.T) {
	m, _ := ruleProvidersModel(t, 40)
	m.renderProviders("")
	m.providers.viewport.SetYOffset(80)
	m.providers.snapshot.Providers = nil
	view, _ := m.renderProviders("")
	if m.providers.viewport.YOffset() != 0 || !strings.Contains(ansi.Strip(view), m.tr("empty.providers.none")) {
		t.Fatal("empty provider panel retained its old list scroll")
	}
	var panel notificationPanel
	panel.show(0)
	items := make([]notificationItem, 40)
	for index := range items {
		items[index] = notificationItem{ID: uint64(index + 1), Kind: "error", Message: "Fixture notification"}
	}
	locale := i18n.New(i18n.English)
	panel.render(items, 80, 24, locale)
	panel.viewport.SetYOffset(80)
	view, _ = panel.render(nil, 80, 24, locale)
	if panel.viewport.YOffset() != 0 || !strings.Contains(ansi.Strip(view), locale.T("notification.empty")) {
		t.Fatal("empty notification panel retained its old list scroll")
	}
}
