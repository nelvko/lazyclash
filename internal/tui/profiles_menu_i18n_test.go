package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestProfileFormCancellationUsesInformationalNotificationInBothLanguages(t *testing.T) {
	for _, sample := range []struct{ language, message string }{{i18n.English, "Input canceled"}, {i18n.Chinese, "已取消输入"}} {
		m, _ := profileClickModel(t, true, true, false, false)
		m.locale = i18n.New(sample.language)
		m.openProfileEdit("profile-rename", "p-stable-profile-id")
		m.failure = "old validation failure"
		m, _ = update(m, special(tea.KeyEscape))
		if m.form != "" || m.failure != "" || m.messageTransient || !m.messageInformational || !strings.Contains(ansi.Strip(m.screen), sample.message) {
			t.Fatalf("%s: cancellation lost notification semantics", sample.language)
		}
	}
}

func TestProfileMenuLanguagesKeepActionsAndDisabledState(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m, _ := profileClickModel(t, true, true, false, false)
		m.locale = i18n.New(language)
		items := m.profileMenuItems("p-stable-profile-id")
		want := []string{"apply", "refresh", "rename", "description", "address", "overrides", "details", "delete"}
		if len(items) != len(want) {
			t.Fatalf("%s: actions=%+v", language, items)
		}
		for index, id := range want {
			if items[index].ID != id || items[index].DisabledReason != "" {
				t.Fatalf("%s: action changed: %+v", language, items[index])
			}
		}
		if items[0].Label != m.tr("profile_menu.use") || items[6].Label != m.tr("profile_menu.details") {
			t.Fatal("menu labels did not follow current language")
		}
		m.snapshot.Applied = "p-stable-profile-id"
		items = m.profileMenuItems("p-stable-profile-id")
		if items[0].DisabledReason != m.tr("profile_menu.already_selected") || items[len(items)-1].DisabledReason != m.tr("profile_menu.switch_first") {
			t.Fatalf("%s: disabled explanations not localized", language)
		}
		m.snapshot.ProfileDetails[1].Pending = true
		items = m.profileMenuItems("p-stable-profile-id")
		if items[0].Label != m.tr("profile_menu.apply_changes") || items[0].DisabledReason != "" {
			t.Fatalf("%s: pending application lost availability", language)
		}
	}
}

func TestProfileMenuLanguageUpdatesPreserveIdentityAndCanvas(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {80, 24}, {30, 10}, {8, 4}, {1, 1}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m, _ := profileClickModel(t, true, true, false, false)
			m.menu.show(m.profileMenuItems("p-stable-profile-id"), size[0]-1, size[1]-1)
			m.menu.selected = profileMenuItemIndex(t, m, "rename")
			m.locale = i18n.New(i18n.Chinese)
			m.menu.setItems(m.profileMenuItems("p-stable-profile-id"))
			if m.menu.items[m.menu.selected].ID != "rename" {
				t.Fatal("language changed the selected action identity")
			}
			view := m.menu.render("", size[0], size[1])
			assertCanvas(t, view, size[0], size[1])
			if size[0] >= 30 && !strings.Contains(ansi.Strip(view), "重命名") {
				t.Fatalf("Chinese menu action missing: %s", view)
			}
			if m.menu.items[m.menu.selected].Label != "重命名…" {
				t.Fatal("menu still holds an English label")
			}
		})
	}
}
