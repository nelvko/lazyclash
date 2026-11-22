package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/configuration"
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

func TestProfileCardOriginAndAgeAlignBelowTitleAndActions(t *testing.T) {
	m := profilesPreview(false)
	m.width, m.height = 160, 40
	info := &m.snapshot.ProfileDetails[2]
	info.Kind, info.Origin = "remote", "subscription.example"
	info.UpdatedAt = time.Now().Add(-32 * 24 * time.Hour)
	for _, width := range []int{55, 32, 16} {
		row := ansi.Strip(m.profileCard(1, width).lines[2])
		if !strings.HasSuffix(row, "32d ago  ") || !strings.HasPrefix(row, "  sub") || strings.Contains(row, "Updated") {
			t.Fatalf("width %d misplaced origin or age: %q", width, row)
		}
	}
	info.UpdatedAt = time.Time{}
	if row := ansi.Strip(m.profileCard(1, 55).lines[2]); strings.TrimSpace(row) != info.Origin {
		t.Fatalf("missing timestamp produced metadata %q", row)
	}
}

func TestProfileCardShowsRemainingFractionAndOptionalExpiry(t *testing.T) {
	m := profilesPreview(false)
	m.width, m.height = 160, 40
	info := &m.snapshot.ProfileDetails[1]
	info.Pending = false
	date := time.Date(2030, 1, 2, 12, 0, 0, 0, time.Local)
	for _, tc := range []struct {
		used, total   uint64
		expire        int64
		want, expiry  string
		filled, empty int
	}{
		{25, 100, date.Unix(), "75 B / 100 B left", "Expires 2030-01-02", 39, 13},
		{100, 100, 0, "0 B / 100 B left", "", 0, 52},
		{101, 100, 0, "0 B / 100 B left", "", 0, 52},
		{0, 100, 0, "100 B / 100 B left", "", 52, 0},
		{0, 0, date.Unix(), "", "Expires 2030-01-02", 0, 0},
		{0, 0, 0, "", "", 0, 0},
	} {
		info.Quota = &configuration.SubscriptionQuota{UsedBytes: tc.used, TotalBytes: tc.total, ExpiresAt: tc.expire}
		card := m.profileCard(0, 56)
		row, bar := ansi.Strip(card.lines[3]), ansi.Strip(card.lines[4])
		if !strings.Contains(row, tc.want) || strings.Contains(row, "left") != (tc.want != "") || !strings.Contains(row, tc.expiry) || strings.Contains(row, "Expires") != (tc.expiry != "") ||
			strings.Count(bar, "━") != tc.filled || strings.Count(bar, "─") != tc.empty {
			t.Fatalf("card misrepresents %+v: %q / %q", tc, row, bar)
		}
	}
	info.Pending = true
	info.Quota = &configuration.SubscriptionQuota{TotalBytes: 100, UsedBytes: 50, ExpiresAt: date.Unix()}
	narrow := m.profileCard(0, 34)
	if p := ansi.Strip(strings.Join(narrow.lines, "\n")); !strings.Contains(p, "50 B / 100 B left") || strings.Contains(p, "Expires") || !strings.Contains(p, "CHANGES PENDING") {
		t.Fatal("narrow card lost balance or action", p)
	}
	if p := ansi.Strip(strings.Join(m.profileDetailsPane(info.ID, 50).lines, "\n")); !strings.Contains(p, "Expires 2030-01-02") || !strings.Contains(p, "50 B / 100 B left") {
		t.Fatal("detail lost quota metadata", p)
	}
}

func TestAppliedProfileHasOnlyLeftEdgeMarker(t *testing.T) {
	m := profilesPreview(false)
	for _, size := range [][2]int{{160, 40}, {80, 24}} {
		m.width, m.height = size[0], size[1]
		for _, width := range []int{1, 12, 32, 55} {
			for i := 0; i < 2; i++ {
				card := m.profileCard(i, width)
				for _, row := range card.lines {
					line := ansi.Strip(row)
					if strings.HasPrefix(line, "▌") != (i == 0) || ansi.StringWidth(line) != width || strings.Contains(line, "IN USE") {
						t.Fatalf("incorrect left edge at %v/%d: %q", size, width, line)
					}
				}
			}
		}
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
			Installed: true,
			Profiles:  []string{"work"},
			ProfileDetails: []app.ProfileInfo{
				{ID: "work", Name: "Work subscription", Validated: true},
			},
		}})
		m.locale, m.page = i18n.New(language), "profiles"
		body, targets := m.profilesBody(112)
		view := ansi.Strip(body)
		if !hasAction(targets, "profile.0") || hasAction(targets, "profile.1") ||
			strings.Count(view, "Work subscription") != 1 || strings.Contains(view, m.tr("profiles.none_selected")) ||
			strings.Contains(view, "▌") || strings.Contains(view, m.tr("empty.profiles.none")) {
			t.Fatalf("unexpected grid without a selected profile: %s", view)
		}
	}
}
