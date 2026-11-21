package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestProfileCardsKeepBoundedMetadataAndAlignedStatus(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{12, 16, 28, 55} {
			t.Run(fmt.Sprintf("%s/%d", language, width), func(t *testing.T) {
				m := profilesPreview(false)
				m.width, m.height = 160, 40
				m.locale = i18n.New(language)
				m.snapshot.ProfileDetails[1].Origin = strings.Repeat("long-source.", 12) + "example"
				for index := range m.profiles() {
					card := m.profileCard(index, width)
					if len(card.lines) != 6 {
						t.Fatalf("card %d changed the grid height: %d rows", index, len(card.lines))
					}
					for row, line := range card.lines {
						if ansi.StringWidth(line) != width {
							t.Fatalf("card %d row %d exceeds its column: %q", index, row, ansi.Strip(line))
						}
					}
					plain := ansi.Strip(strings.Join(card.lines, "\n"))
					if strings.Contains(plain, m.tr("profiles.kind")) {
						t.Fatalf("card retains detail-only metadata: %s", plain)
					}
					for _, target := range card.targets {
						if target.X+target.W > width || target.Y+target.H > len(card.lines) {
							t.Fatalf("card control left its painted bounds: %+v", target)
						}
					}
				}
			})
		}
	}
}

func TestProfileMetadataWrapsWithoutEmptySeparatorAndUsesReadableColors(t *testing.T) {
	m := profilesPreview(false)
	m.width, m.height = 160, 40
	m.focus, m.hover = "", ""
	info := &m.snapshot.ProfileDetails[2]
	info.Kind, info.Origin = "remote", "subscription.example"
	info.UpdatedAt = time.Now()
	card := m.profileCard(1, 32)
	metadata := ansi.Strip(card.lines[2] + card.lines[3])
	if !strings.Contains(metadata, info.Origin) || !strings.Contains(metadata, "Updated just now") {
		t.Fatalf("source and timestamp did not wrap within two rows: %q", metadata)
	}
	if !strings.Contains(card.lines[2], themed(info.Origin+" ·", muted, panel, false)) {
		t.Fatal("source description did not use secondary text")
	}
	if !strings.Contains(card.lines[4], themed(m.tr("profiles.ready"), text, panel, false)) {
		t.Fatal("a ready, unselected profile did not use readable body text")
	}
	info.Origin = "  "
	card = m.profileCard(1, 55)
	metadata = ansi.Strip(card.lines[2] + card.lines[3])
	if strings.Contains(metadata, "·") || !strings.Contains(metadata, "Updated just now") {
		t.Fatalf("missing origin left an empty separator: %q", metadata)
	}
}

func TestProfileDetailsRetainTypeOverridesAndFullSafeOrigin(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := profilesPreview(false)
		m.locale = i18n.New(language)
		origin := strings.Repeat("safe-host.", 8) + "example"
		m.snapshot.ProfileDetails[1].Origin = origin
		detail := m.profileDetailsPane("Everyday", 32)
		plain := ansi.Strip(strings.Join(detail.lines, "\n"))
		if !strings.Contains(plain, m.tr("profiles.kind_overrides", m.tr("profiles.kind"))) || !strings.Contains(strings.ReplaceAll(plain, "\n", ""), origin) {
			t.Fatalf("details lost type, overrides or full safe origin: %s", plain)
		}
		for _, source := range []struct{ kind, key string }{{"local", "profiles.create_local"}, {"imported", "profiles.imported_origin"}} {
			m.snapshot.ProfileDetails[2].Kind = source.kind
			card := ansi.Strip(strings.Join(m.profileCard(1, 55).lines, "\n"))
			if !strings.Contains(card, m.tr(source.key)) {
				t.Fatalf("%s source label did not follow %s: %s", source.kind, language, card)
			}
		}
	}
}

func TestProfilesGridListsOnlySavedProfiles(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := ready(&fakeBackend{snapshot: app.Snapshot{
			Installed: true, Applied: "default",
			Profiles: []string{"default", "work"},
			ProfileDetails: []app.ProfileInfo{
				{ID: "default", Name: "default", Kind: "builtin"},
				{ID: "work", Name: "Work subscription", Validated: true},
			},
		}})
		m.locale, m.page = i18n.New(language), "profiles"
		body, targets := m.profilesBody(112)
		view := ansi.Strip(body)
		if !hasAction(targets, "profile.0") || hasAction(targets, "profile.1") ||
			strings.Count(view, "Work subscription") != 1 || strings.Contains(view, m.tr("profiles.builtin_name")) ||
			strings.Contains(view, m.tr("profiles.in_use")) || strings.Contains(view, m.tr("empty.profiles.none")) {
			t.Fatalf("unexpected grid while default is applied: %s", view)
		}
	}
}
