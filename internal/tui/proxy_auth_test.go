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
	b.prepareProxyAuth = func(_ context.Context, original app.ProxyAuthSettings, proposed []app.ProxyAuthUser, _ string) (app.RuntimeSettingsPlan, error) {
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
		if !slices.Equal(users, want) || applies != 0 || m.ports.step != "review" || m.ports.focus != "ports.cancel" {
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
	b.prepareProxyAuth = func(context.Context, app.ProxyAuthSettings, []app.ProxyAuthUser, string) (app.RuntimeSettingsPlan, error) {
		t.Fatal("invalid draft reached the backend")
		return app.RuntimeSettingsPlan{}, nil
	}
	m := openProxyAuthFixture(t, b, 40, 24)
	last := authRowID("username", len(want)-1)
	m.ports.field(last).SetValue(want[0])
	m.layout()
	m, _ = click(m, findTarget(t, m, "ports.review"))
	if m.ports.focus != last || !hasAction(m.targets, last) || m.ports.err != m.tr("access.authDuplicate") || m.busy {
		t.Fatal("duplicate account did not reveal and focus its field on the narrow viewport")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.ports.open || len(m.ports.access.auth.rows) != 0 || m.focus != "settings.proxyAuth" {
		t.Fatal("cancelling retained an account draft or lost the setting focus")
	}
}

func TestProxyAuthBypassEditsIndependentlyAtBothWidths(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {40, 24}} {
		saved := app.ProxyAuthSettings{Usernames: []string{"desktop"}, SkipAuthPrefixes: "10.0.0.0/8"}
		b := &fakeBackend{snapshot: settingsTestSnapshot()}
		b.getProxyAuth = func(context.Context) (app.ProxyAuthSettings, error) { return saved, nil }
		const proposed = "192.168.1.5/32\nfd00::/64"
		calls := 0
		b.prepareProxyAuth = func(_ context.Context, _ app.ProxyAuthSettings, users []app.ProxyAuthUser, prefixes string) (app.RuntimeSettingsPlan, error) {
			calls++
			if prefixes != proposed || !slices.Equal(users, []app.ProxyAuthUser{{OriginalUsername: "desktop", Username: "desktop"}}) {
				t.Fatal("prefix-only edit changed credentials or lost normalized prefixes")
			}
			return app.RuntimeSettingsPlan{Changed: true}, nil
		}
		m := openProxyAuthFixture(t, b, size[0], size[1])
		if hasAction(m.targets, "ports.review") || slices.Contains(m.ports.controls, proxyAuthPrefixes) {
			t.Fatal("unchanged form was reviewable or the collapsed field received focus")
		}
		m.ports.focus, m.ports.reveal = proxyAuthBypass, true
		m.layout()
		m, _ = update(m, special(tea.KeyEnter))
		m, _ = update(m, special(tea.KeyTab))
		if m.ports.focus != proxyAuthPrefixes || !hasAction(m.targets, proxyAuthPrefixes) {
			t.Fatal("expanded exception list is unreachable")
		}
		area := m.ports.textarea(proxyAuthPrefixes)
		area.SetValue("")
		m, _ = update(m, tea.PasteMsg{Content: "not a prefix"})
		if hasAction(m.targets, "ports.review") || m.preparePorts() != nil {
			t.Fatal("invalid exception reached review")
		}
		area.SetValue("")
		m, _ = update(m, tea.PasteMsg{Content: "fd00::12/64\n192.168.1.5\n192.168.1.5/32"})
		m, cmd := click(m, findTarget(t, m, "ports.review"))
		m, _ = update(m, actionCmd(t, cmd)())
		doc, _, _ := m.portsDocument(74)
		review := ansi.Strip(strings.Join(doc.lines, " "))
		if calls != 1 || m.ports.step != "review" || !strings.Contains(review, saved.SkipAuthPrefixes) || !strings.Contains(review, "192.168.1.5/32") || !strings.Contains(review, "fd00::/64") || strings.Contains(review, "desktop") {
			t.Fatal("review did not limit its changes to added and removed exceptions")
		}
		m, _ = update(m, special(tea.KeyEscape))
		if m.ports.open || m.ports.access.areas != nil {
			t.Fatal("cancelling retained the exception draft")
		}
	}
}

func TestProxyAddressClipboardSelectsOnlyOnExplicitRequest(t *testing.T) {
	for _, names := range [][]string{{"desktop"}, {"desktop", "tablet"}} {
		for _, size := range [][2]int{{160, 45}, {40, 24}} {
			b := &fakeBackend{snapshot: settingsTestSnapshot()}
			b.snapshot.RuntimeSettings.ProxyUserCount = len(names)
			b.snapshot.RuntimeSettings.MixedPort = 7897
			b.getProxyAuth = func(context.Context) (app.ProxyAuthSettings, error) {
				return app.ProxyAuthSettings{Usernames: names}, nil
			}
			const credential = "private-copy-fixture"
			chosen := names[len(names)-1]
			address := "http://" + chosen + ":" + credential + "@<LAN IP>:7897"
			reads := 0
			b.getProxyAddress = func(_ context.Context, original app.ProxyAuthSettings, user string) (string, error) {
				reads++
				if !slices.Equal(original.Usernames, names) || user != chosen {
					t.Fatal("unexpected clipboard account")
				}
				return address, nil
			}
			m := focusedPortsEntry(b, size[0], size[1])
			m.focus = "settings.copyProxyAddress"
			m.revealContentFocus()
			m.layout()
			if reads != 0 || strings.Contains(m.screen, credential) {
				t.Fatal("render accessed a credential")
			}
			copyButton := findTarget(t, m, "settings.copyProxyAddress")
			m, _ = update(m, tea.MouseMotionMsg{X: copyButton.X, Y: copyButton.Y})
			m, _ = update(m, m.tooltip.generation)
			if m.tooltip.open != copyButton.ID || m.tooltip.bounds.W == 0 || copyButton.tip.text != m.tr("settings.copyProxyAddress") || reads != 0 {
				t.Fatal("hover must explain that credentials will be copied before accessing them")
			}
			m, _ = update(m, special(tea.KeyEscape))
			m, _ = update(m, special(tea.KeyF1))
			if m.tooltip.open != copyButton.ID || m.keyHints.generation != 0 || reads != 0 {
				t.Fatal("focused copy control lost its contextual help")
			}
			m, _ = update(m, special(tea.KeyEscape))
			cmd := m.copyProxyAddress()
			if cmd == nil {
				t.Fatal("copy action unavailable")
			}
			reply := cmd().(proxyCopyMsg)
			write := m.acceptProxyCopy(reply)
			if len(names) > 1 {
				m.layout()
				if reads != 0 || write != nil || !m.ports.open || !strings.Contains(ansi.Strip(m.screen), "tablet") || !hasAction(m.targets, proxyCopyAccountID(1)) {
					t.Fatal("multiple accounts must be offered before reading a credential")
				}
				m, _ = update(m, special(tea.KeyTab))
				cmd = m.portsActivate(m.ports.focus)
				if cmd == nil {
					t.Fatal("selecting an account did not request a copy")
				}
				reply = cmd().(proxyCopyMsg)
				write = m.acceptProxyCopy(reply)
			}
			m.layout()
			if m.ports.open || reads != 1 || write == nil || write() != tea.SetClipboard(localizedProxyAddress(address, m.locale))() || m.message != m.tr("access.copyTemplateRequested") || strings.Contains(m.screen, credential) {
				t.Fatal("copy did not isolate the selected credential or report the wildcard host")
			}
			cmd = m.copyProxyAddress()
			stale := cmd().(proxyCopyMsg)
			m.abortLookup()
			if m.acceptProxyCopy(stale) != nil || m.ports.open {
				t.Fatal("stale copy produced a write or reopened the picker")
			}
		}
	}
}

func TestEmptyProxyAuthFormAllowsEditingExceptionsAlone(t *testing.T) {
	b := &fakeBackend{snapshot: settingsTestSnapshot()}
	m := openProxyAuthFixture(t, b, 80, 24)
	if m.ports.focus != authRowID("username", 0) || hasAction(m.targets, "ports.review") {
		t.Fatal("new users should be able to type immediately without creating an empty account")
	}
	b.prepareProxyAuth = func(_ context.Context, _ app.ProxyAuthSettings, users []app.ProxyAuthUser, prefixes string) (app.RuntimeSettingsPlan, error) {
		if len(users) != 0 || prefixes != "127.0.0.1/32" {
			t.Fatal("editing exceptions unexpectedly created an account")
		}
		return app.RuntimeSettingsPlan{Changed: true}, nil
	}
	m.ports.textarea(proxyAuthPrefixes).SetValue("127.0.0.1")
	m.layout()
	m, cmd := click(m, findTarget(t, m, "ports.review"))
	m, _ = update(m, actionCmd(t, cmd)())
	if m.ports.step != "review" || strings.Contains(ansi.Strip(m.screen), m.tr("access.authAdded")) {
		t.Fatal("an untouched blank row appeared in the review")
	}
}
