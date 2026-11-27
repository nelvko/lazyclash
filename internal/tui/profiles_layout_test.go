package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestSubscriptionInputAlignsWithRulesSearch(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {80, 24}, {60, 18}} {
		m := profileNavigationModel(size[0], size[1])
		field := findTarget(t, m, "profiles.url")
		rules, id := searchFieldModel(t, "rules", size[0], size[1])
		search := findTarget(t, rules, id)
		if field.Y != search.Y {
			t.Errorf("%v: subscription input starts at row %d, rules search at %d", size, field.Y, search.Y)
		}
	}
}

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
					if len(card.lines) != 5 {
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
	info.Description = strings.Repeat("出差备用", 12)
	for _, width := range []int{32, 55} {
		card := m.profileCard(1, width)
		row := ansi.Strip(card.lines[2])
		if ansi.StringWidth(row) != width || !strings.HasPrefix(row, "  出差备用") ||
			!strings.Contains(row, "…") || !strings.HasSuffix(row, "32d ago  ") || strings.Contains(row, info.Origin) {
			t.Fatalf("description displaced the age or changed the card layout at %d: %q", width, row)
		}
	}
	info.Description = " "
	info.UpdatedAt = time.Time{}
	if row := ansi.Strip(m.profileCard(1, 55).lines[2]); strings.TrimSpace(row) != info.Origin {
		t.Fatalf("missing timestamp produced metadata %q", row)
	}
}

func TestProfileGridEndsEachCardAtItsQuotaBar(t *testing.T) {
	for _, size := range []struct{ width, height, topPadding, columns int }{{160, 40, 1, 2}, {80, 24, 0, 1}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			m := profilesPreview(false)
			m.width, m.height, m.focus = size.width, size.height, "profiles.url"
			info := &m.snapshot.ProfileDetails[1]
			info.Pending, m.snapshot.ProfilePending = false, false
			info.Quota = &configuration.SubscriptionQuota{TotalBytes: 100}
			m.layout()

			first := findTarget(t, m, "profile.0")
			field := findTarget(t, m, "profiles.url")
			if first.X != field.X || first.H != size.topPadding+4 {
				t.Fatalf("card and input geometry: card=%+v field=%+v", first, field)
			}
			if size.columns == 2 {
				neighbor := findTarget(t, m, "profile.1")
				if first.Y != neighbor.Y || first.H != neighbor.H || first.W == field.W {
					t.Fatalf("wide grid should align card rows independently of the input width: %+v %+v %+v", first, neighbor, field)
				}
			}
			rows := strings.Split(ansi.Strip(m.screen), "\n")
			title := ansi.Cut(rows[first.Y+size.topPadding], first.X, first.X+first.W)
			if !strings.HasPrefix(title, "▌ "+info.Name) {
				t.Fatalf("misplaced title: %q", title)
			}
			bottom := first.Y + first.H - 1
			bar := ansi.Cut(rows[bottom], first.X, first.X+first.W)
			if want := "▌ " + strings.Repeat("━", first.W-4) + "  "; bar != want {
				t.Fatalf("quota bar missing from the last card row or misaligned with content: %q", bar)
			}
			nextRow := findTarget(t, m, fmt.Sprintf("profile.%d", size.columns))
			if nextRow.X != first.X || nextRow.Y != bottom+2 {
				t.Fatalf("expected one gutter row between cards: %+v %+v", first, nextRow)
			}
			x := first.X + first.W/2
			m, _ = update(m, tea.MouseClickMsg{X: x, Y: bottom + 1, Button: tea.MouseRight})
			if m.menu.open {
				t.Fatal("right-click in the row gap opened a card menu")
			}
			m, _ = update(m, tea.MouseClickMsg{X: x, Y: bottom, Button: tea.MouseRight})
			if !m.menu.open || m.profilesUI.menuProfile != info.ID {
				t.Fatal("right-click on the progress bar did not open its card menu")
			}
		})
	}
}

func TestProfileCardShowsRemainingFractionAndOptionalExpiry(t *testing.T) {
	m := profilesPreview(false)
	m.width, m.height = 160, 40
	info := &m.snapshot.ProfileDetails[1]
	info.Pending, m.snapshot.ProfilePending = false, false
	date := time.Now().AddDate(1, 0, 0)
	dateText := date.Format("2006-01-02")
	for _, tc := range []struct {
		used, total   uint64
		expire        int64
		want, expiry  string
		filled, empty int
	}{
		{25, 100, date.Unix(), "75 B / 100 B", dateText, 39, 13},
		{100, 100, 0, "0 B / 100 B", "", 0, 52},
		{101, 100, 0, "0 B / 100 B", "", 0, 52},
		{0, 100, 0, "100 B / 100 B", "", 52, 0},
		{0, 0, date.Unix(), "", dateText, 0, 0},
		{0, 0, 0, "", "", 0, 0},
	} {
		info.Quota = &configuration.SubscriptionQuota{UsedBytes: tc.used, TotalBytes: tc.total, ExpiresAt: tc.expire}
		card := m.profileCard(0, 56)
		row, bar := ansi.Strip(card.lines[3]), ansi.Strip(card.lines[4])
		// Strip only the fixed selection marker and layout whitespace; this
		// catches stale date labels and unexpected text in the empty state.
		content := strings.Fields(strings.TrimPrefix(row, "▌"))
		if strings.Join(content, " ") != strings.TrimSpace(tc.want+" "+tc.expiry) ||
			strings.Count(bar, "━") != tc.filled || strings.Count(bar, "─") != tc.empty {
			t.Fatalf("card misrepresents %+v: %q / %q", tc, row, bar)
		}
	}
	info.Pending = true
	info.Quota = &configuration.SubscriptionQuota{TotalBytes: 100, UsedBytes: 50, ExpiresAt: date.Unix()}
	for _, lang := range []struct{ id, balance, expiry string }{
		{i18n.English, "Remaining / total", "Expiry date"},
		{i18n.Chinese, "剩余流量 / 总量", "到期日期"},
	} {
		m.locale = i18n.New(lang.id)
		for _, width := range []int{25, 31} {
			card := m.profileCard(0, width)
			content := strings.Fields(strings.TrimPrefix(ansi.Strip(card.lines[3]), "▌"))
			want := "50 B / 100 B"
			if width == 31 {
				want += " " + dateText
			}
			if row := strings.Join(content, " "); row != want {
				t.Errorf("%s at width %d: got quota row %q, want %q", lang.id, width, row, want)
			}
			if !strings.Contains(ansi.Strip(card.lines[4]), m.tr("profiles.refresh_failed")) {
				t.Errorf("%s at width %d: missing pending status", lang.id, width)
			}
		}
		if p := ansi.Strip(strings.Join(m.profileSettingsMetadata(*info, 72).lines, "\n")); !strings.Contains(p, lang.expiry) || !strings.Contains(p, dateText) || !strings.Contains(p, lang.balance) || !strings.Contains(p, "50 B / 100 B") {
			t.Fatal("detail lost quota metadata", p)
		}
	}
}

func TestProfileSettingsMetadataFitsWideAndNarrowForms(t *testing.T) {
	updated := time.Date(2025, 10, 30, 17, 6, 3, 0, time.Local)
	info := app.ProfileInfo{UpdatedAt: updated, Quota: &configuration.SubscriptionQuota{
		UsedBytes: 50, TotalBytes: 100, ExpiresAt: updated.AddDate(1, 0, 0).Unix(),
	}}
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := profilesPreview(false)
		m.locale = i18n.New(language)
		labels := []string{m.tr("profiles.updated_date"), m.tr("profiles.remaining"), m.tr("profiles.expires")}
		values := []string{"2025-10-30 17:06:03", "50 B / 100 B", "2026-10-30"}
		rows := func(width, wantRows int) []string {
			t.Helper()
			lines := m.profileSettingsMetadata(info, width).lines
			if len(lines) != wantRows {
				t.Fatalf("%s at %d cells: got %d metadata rows, want %d", language, width, len(lines), wantRows)
			}
			for i, line := range lines {
				if ansi.StringWidth(line) > width {
					t.Fatalf("%s: metadata exceeded %d cells: %q", language, width, ansi.Strip(line))
				}
				lines[i] = ansi.Strip(line)
			}
			return lines
		}
		wide, narrow, stacked := rows(72, 2), rows(38, 3), rows(22, 6)
		for i, label := range labels {
			left, _, ok := strings.Cut(wide[0], label)
			if !ok || !strings.HasPrefix(ansi.Cut(wide[1], ansi.StringWidth(left), 72), values[i]) {
				t.Fatalf("%s: %q was not aligned below %q: %q", language, values[i], label, wide)
			}
			if !strings.HasPrefix(narrow[i], label) || !strings.HasSuffix(narrow[i], values[i]) {
				t.Fatalf("%s: metadata did not fit beside its label: %q", language, narrow)
			}
			if stacked[2*i] != label || stacked[2*i+1] != values[i] {
				t.Fatalf("%s: stacked metadata lost %q or %q: %q", language, label, values[i], stacked)
			}
		}
		if lines := m.profileSettingsMetadata(app.ProfileInfo{}, 72).lines; len(lines) != 0 {
			t.Fatalf("empty metadata rendered %q", lines)
		}
	}
}

func TestProfileCardExpiryUsesExactInstantAndLocalizedExpiredLabel(t *testing.T) {
	expiry := time.Date(2030, 1, 2, 23, 5, 0, 0, time.Local)
	for _, language := range []struct{ name, expired string }{
		{i18n.English, "Expired"},
		{i18n.Chinese, "已过期"},
	} {
		locale := i18n.New(language.name)
		for _, tc := range []struct {
			name      string
			expiresAt int64
			now       time.Time
			want      string
		}{
			{"unknown", 0, expiry, ""},
			{"before", expiry.Unix(), expiry.Add(-time.Second), "2030-01-02"},
			{"at", expiry.Unix(), expiry, language.expired},
			{"after", expiry.Unix(), expiry.Add(time.Second), language.expired},
		} {
			if got := profileCardExpiry(tc.expiresAt, tc.now, locale); got != tc.want {
				t.Errorf("%s/%s: got %q, want %q", language.name, tc.name, got, tc.want)
			}
		}
	}
}

func TestIdleProfilesRedrawAtExpiryAndCancelOnExit(t *testing.T) {
	m := profilesPreview(false)
	m.snapshot.Installed = false
	now := time.Now().Unix()
	first := &m.snapshot.ProfileDetails[1]
	first.Quota = &configuration.SubscriptionQuota{ExpiresAt: now + 2}
	m.snapshot.ProfileDetails[2].Quota = &configuration.SubscriptionQuota{ExpiresAt: now + 60}

	wake := m.syncProfileExpiry(nil, "overview")
	if wake == nil || m.profilesUI.expiryAt != first.Quota.ExpiresAt || m.syncProfileExpiry(coreStatusMsg{}, "profiles") != nil {
		t.Fatal("an idle page must schedule one wakeup for the earliest expiry")
	}
	msg := wake()
	if _, ok := msg.(profileExpiredMsg); !ok {
		t.Fatal("expiry did not wake the page")
	}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if !strings.Contains(ansi.Strip(m.screen), "Expired") || m.profilesUI.expiryAt != now+60 {
		t.Fatal("idle page missed the expiry or failed to schedule the next one")
	}
	m.page = "overview"
	if m.syncProfileExpiry(nil, "profiles") != nil || m.profilesUI.cancelExpiry != nil {
		t.Fatal("leaving Profiles retained the expiry timer")
	}
}

func TestOverviewSchedulesExpiryOnlyForAppliedSubscription(t *testing.T) {
	m := profilesPreview(false)
	m.page = "overview"
	now := time.Now().Unix()
	m.snapshot.ProfileDetails[1].Quota = &configuration.SubscriptionQuota{ExpiresAt: now + 60}
	m.snapshot.ProfileDetails[2].Quota = &configuration.SubscriptionQuota{ExpiresAt: now + 30}

	if cmd := m.syncProfileExpiry(nil, "profiles"); cmd == nil || m.profilesUI.expiryAt != now+60 {
		t.Fatalf("overview scheduled expiry %d, want applied subscription at %d", m.profilesUI.expiryAt, now+60)
	}
	m.snapshot.Applied = m.snapshot.ProfileDetails[2].ID
	if cmd := m.syncProfileExpiry(resultMsg{}, "overview"); cmd == nil || m.profilesUI.expiryAt != now+30 {
		t.Fatalf("newly selected subscription expiry was not scheduled: %d", m.profilesUI.expiryAt)
	}
	m.snapshot.Applied = ""
	if cmd := m.syncProfileExpiry(resultMsg{}, "overview"); cmd != nil || m.profilesUI.cancelExpiry != nil {
		t.Fatal("empty subscription card retained an expiry timer")
	}
}

func TestExpiredProfileCardKeepsActualDateInDetails(t *testing.T) {
	expiry := time.Date(2001, 3, 4, 12, 0, 0, 0, time.Local)
	for _, language := range []struct{ name, expired, label string }{
		{i18n.English, "Expired", "Expiry date"},
		{i18n.Chinese, "已过期", "到期日期"},
	} {
		m := profilesPreview(false)
		m.locale = i18n.New(language.name)
		info := &m.snapshot.ProfileDetails[1]
		info.Quota = &configuration.SubscriptionQuota{ExpiresAt: expiry.Unix()}
		card := ansi.Strip(strings.Join(m.profileCard(0, 56).lines, "\n"))
		if !strings.Contains(card, language.expired) || strings.Contains(card, "2001-03-04") {
			t.Errorf("%s card lost expired status: %s", language.name, card)
		}
		detail := ansi.Strip(strings.Join(m.profileSettingsMetadata(*info, 72).lines, "\n"))
		if !strings.Contains(detail, language.label) || !strings.Contains(detail, "2001-03-04") {
			t.Errorf("%s details lost actual expiry date: %s", language.name, detail)
		}
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
func TestProfileCardsLocalizeSourceType(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := profilesPreview(false)
		m.locale = i18n.New(language)
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
