package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func openProxyAuthFixture(t *testing.T, b *fakeBackend, width, height int) Model {
	t.Helper()
	m := focusedPortsEntry(b, width, height)
	m.focus = "settings.proxyAuth"
	// Enter through the focused setting, including the single-column viewport.
	m.revealContentFocus()
	m.layout()
	m, cmd := update(m, special(tea.KeyEnter))
	m, _ = update(m, actionCmd(t, cmd)())
	if !m.ports.open || m.ports.kind != "auth" || m.ports.step != "edit" {
		t.Fatal("settings did not open the account editor")
	}
	return m
}

func TestProxyAuthLayoutKeepsFieldsAndRemoveWithTheirAccount(t *testing.T) {
	b := &fakeBackend{snapshot: settingsTestSnapshot()}
	b.getProxyAuth = func(context.Context) (app.ProxyAuthSettings, error) {
		return app.ProxyAuthSettings{Usernames: []string{"desktop", "tablet"}}, nil
	}
	m := openProxyAuthFixture(t, b, 160, 45)
	for _, width := range []int{72, 30} {
		doc, _, _ := m.portsDocument(width)
		previousBottom := 0
		for i := range 2 {
			hits := Model{targets: doc.targets}
			name := findTarget(t, hits, authRowID("username", i))
			password := findTarget(t, hits, authRowID("password", i))
			remove := findTarget(t, hits, authRowID("remove", i))
			if name.H != 3 || password.H != 3 || name.Y <= previousBottom {
				t.Fatalf("width %d: account %d frames overlap or lack borders", width, i)
			}
			if width == 72 {
				if name.Y != password.Y || password.X <= name.X+name.W || remove.X <= password.X+password.W || remove.Y != password.Y+1 {
					t.Fatalf("wide form lost its side-by-side fields and remove action: %v %v %v", name, password, remove)
				}
			} else if name.X != password.X || password.Y <= name.Y+name.H || remove.Y != name.Y-1 {
				t.Fatalf("narrow form lost its stacked fields or account header action: %v %v %v", name, password, remove)
			}
			previousBottom = password.Y + password.H - 1
		}
		assertRegions(t, doc.targets, width, len(doc.lines))
	}
}

func TestProxyAuthModalReviewsAccountChangesAndPreservesPasswords(t *testing.T) {
	const newPassword = "example:secret with spaces"
	saved := app.ProxyAuthSettings{Usernames: []string{"alice", "bob", "charlie"}}
	b := &fakeBackend{snapshot: settingsTestSnapshot()}
	b.getProxyAuth = func(context.Context) (app.ProxyAuthSettings, error) { return saved, nil }
	var users []app.ProxyAuthUser
	b.prepareProxyAuth = func(_ context.Context, original app.ProxyAuthSettings, proposed []app.ProxyAuthUser) (app.RuntimeSettingsPlan, error) {
		if !slices.Equal(original.Usernames, saved.Usernames) {
			t.Fatal("preflight lost the original account list")
		}
		users = slices.Clone(proposed)
		return app.RuntimeSettingsPlan{Changed: true, Active: true}, nil
	}
	applies := 0
	b.applySettings = func(context.Context, app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
		applies++
		return app.RuntimeSettingsResult{Changed: true, Restarted: true}, nil
	}
	m := openProxyAuthFixture(t, b, 120, 38)
	m.ports.field(authRowID("username", 0)).SetValue("alice-work")
	m.ports.field(authRowID("password", 1)).SetValue(newPassword)
	m, _ = click(m, findTarget(t, m, authRowID("remove", 2)))
	m, _ = click(m, findTarget(t, m, proxyAuthAdd))
	m, _ = update(m, tea.PasteMsg{Content: "dana"})
	m, _ = update(m, special(tea.KeyTab))
	m, _ = update(m, tea.PasteMsg{Content: newPassword})
	if m.ports.focus != authRowID("password", 2) || strings.Contains(m.screen, newPassword) || !strings.Contains(m.screen, "•••") {
		t.Fatal("new account input focus or password masking failed")
	}
	for attempt := 0; attempt < 2; attempt++ {
		var cmd tea.Cmd
		m, cmd = click(m, findTarget(t, m, "ports.review"))
		m, _ = update(m, actionCmd(t, cmd)())
		want := []app.ProxyAuthUser{
			{OriginalUsername: "alice", Username: "alice-work"}, {OriginalUsername: "bob", Username: "bob", Password: newPassword}, {Username: "dana", Password: newPassword},
		}
		if !slices.Equal(users, want) || applies != 0 || m.ports.step != "review" || m.ports.focus != "ports.apply" {
			t.Fatal("review did not keep the multi-user proposal separate from apply")
		}
		doc, _, _ := m.portsDocument(74)
		review := strings.Join(strings.Fields(ansi.Strip(strings.Join(doc.lines, " "))), " ")
		for _, label := range []string{"access.authRenamed", "access.authPasswordChanged", "access.authAdded", "access.authRemoved", "access.authManagedHint"} {
			if !strings.Contains(review, m.tr(label)) {
				t.Fatalf("review omitted %s", label)
			}
		}
		if strings.Contains(review, newPassword) || strings.Contains(m.screen, newPassword) {
			t.Fatal("review exposed the password")
		}
		if attempt == 0 {
			m, _ = click(m, findTarget(t, m, "ports.back"))
		}
	}
	m, cmd := click(m, findTarget(t, m, "ports.apply"))
	m, _ = update(m, actionCmd(t, cmd)())
	if applies != 1 || !m.ports.applied || len(m.ports.access.auth.rows) != 0 || strings.Contains(m.screen, newPassword) {
		t.Fatal("apply failed to clear the credential draft")
	}
	m, _ = click(m, findTarget(t, m, "ports.done"))
	if m.ports.open || m.focus != "settings.proxyAuth" {
		t.Fatal("did not return to the authentication row")
	}
}

func TestProxyAuthNarrowModalRevealsInvalidAccountAndDiscardsDraft(t *testing.T) {
	b := &fakeBackend{snapshot: settingsTestSnapshot()}
	want := []string{"one", "two", "three", "four", "five", "six"}
	b.getProxyAuth = func(context.Context) (app.ProxyAuthSettings, error) {
		return app.ProxyAuthSettings{Usernames: want}, nil
	}
	b.prepareProxyAuth = func(context.Context, app.ProxyAuthSettings, []app.ProxyAuthUser) (app.RuntimeSettingsPlan, error) {
		t.Fatal("invalid draft reached the backend")
		return app.RuntimeSettingsPlan{}, nil
	}
	m := openProxyAuthFixture(t, b, 40, 24)
	last := authRowID("username", len(want)-1)
	m.ports.field(last).SetValue(want[0])
	m, _ = click(m, findTarget(t, m, "ports.review"))
	if m.ports.focus != last || !hasAction(m.targets, last) || m.ports.err != m.tr("access.authDuplicate") || m.busy {
		t.Fatal("duplicate account did not reveal and focus its field on the narrow viewport")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.ports.open || len(m.ports.access.auth.rows) != 0 || m.focus != "settings.proxyAuth" {
		t.Fatal("cancelling retained an account draft or lost the setting focus")
	}
}

func TestProxyAuthReviewCanAdoptInheritedOrEmptyList(t *testing.T) {
	for _, names := range [][]string{nil, {"existing"}} {
		b := &fakeBackend{snapshot: settingsTestSnapshot()}
		b.getProxyAuth = func(context.Context) (app.ProxyAuthSettings, error) {
			return app.ProxyAuthSettings{Usernames: names}, nil
		}
		b.prepareProxyAuth = func(_ context.Context, _ app.ProxyAuthSettings, proposed []app.ProxyAuthUser) (app.RuntimeSettingsPlan, error) {
			if len(proposed) != len(names) || len(proposed) > 0 && (proposed[0].OriginalUsername != names[0] || proposed[0].Password != "") {
				t.Fatal("adopting a profile changed its account list")
			}
			return app.RuntimeSettingsPlan{Changed: true}, nil
		}
		m := openProxyAuthFixture(t, b, 120, 38)
		m, cmd := click(m, findTarget(t, m, "ports.review"))
		m, _ = update(m, actionCmd(t, cmd)())
		doc, _, _ := m.portsDocument(74)
		review := strings.Join(strings.Fields(ansi.Strip(strings.Join(doc.lines, " "))), " ")
		want := m.tr("access.authDisabled")
		if len(names) > 0 {
			want = names[0]
		}
		if m.ports.step != "review" || !strings.Contains(review, want) || !strings.Contains(review, m.tr("access.authManagedHint")) {
			t.Fatal("adopting the current accounts did not show what would be saved")
		}
	}
}
