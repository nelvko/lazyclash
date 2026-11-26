package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func proxySearchScrolledReferenceModel(t *testing.T, width int) Model {
	t.Helper()
	m := proxyHoverModel(t, 120)
	m.width, m.height = width, 24
	m.snapshot.Groups[1].All = append([]string(nil), m.snapshot.Groups[0].All[:60]...)
	m.snapshot.Groups[1].Now = m.snapshot.Groups[1].All[25]
	m.focus = "node.0.0"
	m.browseProxyReference("AUTO")
	m.focus = "node.1.25"
	m.revealContentFocus()
	m.layout()
	if m.scroll <= 0 || len(m.proxies.history) == 0 || m.proxies.activeGroup != "AUTO" {
		t.Fatalf("fixture did not enter a scrolled referenced group: scroll=%d active=%s history=%+v", m.scroll, m.proxies.activeGroup, m.proxies.history)
	}
	return m
}

func TestProxiesSearchEscapeRestoresOriginalBrowsingSession(t *testing.T) {
	for _, width := range []int{80, 160} {
		for _, entry := range []string{"slash", "click"} {
			for _, exit := range []string{"editor", "results"} {
				t.Run(fmt.Sprintf("%d/%s/%s", width, entry, exit), func(t *testing.T) {
					m := proxySearchScrolledReferenceModel(t, width)
					location := m.proxyLocation()
					activeGroup := m.proxies.activeGroup
					history := append([]proxyLocation(nil), m.proxies.history...)
					for i := range history {
						history[i].expanded = cloneProxyExpansion(history[i].expanded)
					}
					if entry == "click" {
						m, _ = click(m, findTarget(t, m, "proxies.search-hint"))
					} else {
						m, _ = update(m, key("/"))
					}
					if !m.proxies.editing || m.focus != "proxies.search" {
						t.Fatal("explicit search entry did not open its editor")
					}
					m, _ = update(m, tea.PasteMsg{Content: "节点 059"})
					if exit == "results" {
						m, _ = update(m, special(tea.KeyEnter))
						if m.proxies.editing || m.proxySearchQuery() == "" {
							t.Fatal("Enter did not leave the filtered search results active")
						}
					}
					m, _ = update(m, special(tea.KeyEscape))
					if m.proxies.editing || m.proxies.searchReturn != nil || m.proxySearchQuery() != "" || hasAction(m.targets, "proxies.search") {
						t.Fatalf("Escape did not end the complete search session: editing=%t query=%q", m.proxies.editing, m.proxySearchQuery())
					}
					if !reflect.DeepEqual(m.proxyLocation(), location) || m.proxies.activeGroup != activeGroup ||
						!reflect.DeepEqual(m.proxies.history, history) {
						t.Fatalf("Escape changed the original browsing location: got=%+v active=%s history=%+v want=%+v active=%s history=%+v", m.proxyLocation(), m.proxies.activeGroup, m.proxies.history, location, activeGroup, history)
					}
				})
			}
		}
	}
}

func TestProxiesSearchEnterFocusesFirstMatchingResult(t *testing.T) {
	for _, test := range []struct {
		name, query, focus string
		width              int
	}{
		{"wide node match", "US 01", "node.0.5", 160},
		{"wide group match", "PROXY", "group.0", 160},
		{"narrow directory", "HK 02", "group.0", 80},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, backend := proxiesModel(t, test.width, 24)
			setProxyTestGroup(&m, "")
			m.layout()
			m, _ = update(m, key("/"))
			m, _ = update(m, tea.PasteMsg{Content: test.query})
			m, _ = update(m, special(tea.KeyEnter))
			if m.proxies.editing || m.focus != test.focus || backend.selections != 0 || m.busy {
				t.Fatalf("Enter did not focus the first matching result without selecting: focus=%s want=%s editing=%t selections=%d", m.focus, test.focus, m.proxies.editing, backend.selections)
			}
		})
	}
}

func TestProxiesSearchFromNarrowMembersIncludesOtherCollapsedGroups(t *testing.T) {
	m, backend := proxiesModel(t, 80, 24)
	if m.proxies.activeGroup != "PROXY" || m.proxyGroupExpanded("Japan") {
		t.Fatal("fixture did not start inside another group's members")
	}
	m, _ = update(m, key("/"))
	m, _ = update(m, tea.PasteMsg{Content: "JP 02"})
	if !hasAction(m.contentTargets(), "group.5") || hasAction(m.contentTargets(), "group.0") || backend.selections != 0 {
		t.Fatalf("narrow search stayed confined to the current group: visible=%v targets=%+v", m.visibleProxyGroups(), m.contentTargets())
	}
}

func TestProxiesSearchShortcutWorksWhileClosedSidebarOwnsFocus(t *testing.T) {
	m := proxySearchScrolledReferenceModel(t, 160)
	m.returnToNavigation()
	m.layout()
	scroll := m.scroll
	browsingFocus := m.pageViews["proxies"].focus
	if m.focus != "nav.proxies" || m.sidebarOpen || scroll <= 0 {
		t.Fatal("fixture did not retain navigation ownership over a scrolled proxy view")
	}
	m, _ = update(m, key("/"))
	if !m.proxies.editing || m.focus != "proxies.search" {
		t.Fatal("slash did not open proxy search from the visible sidebar")
	}
	m, _ = update(m, tea.PasteMsg{Content: "unmatched search"})
	m, _ = update(m, special(tea.KeyEscape))
	if m.focus != "nav.proxies" || m.scroll != scroll || m.proxies.editing {
		t.Fatalf("Escape lost sidebar ownership or its original scroll: focus=%s scroll=%d want=%d", m.focus, m.scroll, scroll)
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.focus != browsingFocus {
		t.Fatalf("entering the list after search lost its original focus: got=%s want=%s", m.focus, browsingFocus)
	}
}

func TestProxiesSearchShortcutDoesNotEscapeAnOpenNavigationDrawer(t *testing.T) {
	m, _ := proxiesModel(t, 80, 24)
	m.sidebarOpen, m.focus = true, "menu"
	m.layout()
	m, _ = update(m, key("/"))
	if m.proxies.editing || !m.sidebarOpen || m.focus != "menu" || hasAction(m.targets, "proxies.search-hint") {
		t.Fatal("slash opened proxy search through the navigation drawer")
	}
}

func TestProxiesSearchClickMatchesPaintedFooterHint(t *testing.T) {
	for _, lang := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{20, 32, 40, 80, 160} {
			t.Run(fmt.Sprintf("%s/%d", lang, width), func(t *testing.T) {
				m, _ := proxiesModel(t, width, 24)
				m.locale = i18n.New(lang)
				label := m.tr("navigation.proxies.searchKey")
				navigation := "menu"
				if width >= sidebarMinWidth {
					navigation = "nav.proxies"
				}
				for _, focus := range []string{navigation, "group.0"} {
					m.focus = focus
					m.layout()
					line := strings.Split(m.screen, "\n")[m.height-1]
					painted := strings.Contains(ansi.Strip(line), label)
					if hasAction(m.focusTargets(), "proxies.search-hint") || hasAction(m.targets, "proxies.search-hint") != painted || width >= 80 && !painted {
						t.Fatalf("focus %s: footer search target differs from painted hint or owns Tab: %q", focus, ansi.Strip(line))
					}
					if !painted {
						continue
					}
					hit := findTarget(t, m, "proxies.search-hint")
					if hit.Y != m.height-1 || hit.H != 1 || ansi.Strip(ansi.Cut(line, hit.X, hit.X+hit.W)) != label {
						t.Fatalf("focus %s: click region does not match %q: %+v in %q", focus, label, hit, ansi.Strip(line))
					}
					editing, _ := click(m, hit)
					if !editing.proxies.editing || editing.focus != "proxies.search" || !hasAction(editing.targets, "proxies.search") || hasAction(editing.targets, hit.ID) {
						t.Fatal("clicking the footer did not replace its shortcut with a focused search")
					}
					editing, _ = update(editing, special(tea.KeyEscape))
					if editing.focus != focus || hasAction(editing.targets, "proxies.search") {
						t.Fatal("Escape did not close search and restore the owner of the footer hint")
					}
				}
			})
		}
	}
}

func TestProxiesSearchShortcutWorksWithHiddenHints(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	hint := findTarget(t, m, "proxies.search-hint")
	m, _ = update(m, key("?"))
	if !m.keyHints.hidden || hasAction(m.targets, hint.ID) {
		t.Fatal("hiding the footer retained its mouse shortcut")
	}
	m, _ = update(m, key("/"))
	if !m.proxies.editing || !hasAction(m.targets, "proxies.search") {
		t.Fatal("hiding hints disabled the slash shortcut")
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.proxies.editing || !hasAction(m.targets, "proxies.search") {
		t.Fatal("tabbing away from an empty editor unexpectedly closed the search session")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if !m.keyHints.hidden || hasAction(m.targets, "proxies.search") || hasAction(m.targets, hint.ID) {
		t.Fatal("ending search restored the hidden footer or left an idle field")
	}
}

func TestProxiesSearchFieldStaysPinnedAndFitsWindowHeight(t *testing.T) {
	for _, size := range []struct{ width, height, fieldRows int }{{40, 16, 1}, {80, 24, 1}, {160, 24, 1}, {80, 25, 3}, {160, 40, 3}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			m := proxySearchScrolledReferenceModel(t, size.width)
			m, _ = update(m, tea.WindowSizeMsg{Width: size.width, Height: size.height})
			m, _ = update(m, key("/"))
			before := findTarget(t, m, "proxies.search")
			if before.H != size.fieldRows || hasAction(m.contentTargets(), before.ID) {
				t.Fatalf("expected a fixed %d-row search field: %+v", size.fieldRows, before)
			}
			if size.fieldRows == 3 {
				lines := strings.Split(m.screen, "\n")
				top := ansi.Strip(ansi.Cut(lines[before.Y], before.X, before.X+before.W))
				bottom := ansi.Strip(ansi.Cut(lines[before.Y+2], before.X, before.X+before.W))
				if top != "╭"+strings.Repeat("─", before.W-2)+"╮" || bottom != "╰"+strings.Repeat("─", before.W-2)+"╯" {
					t.Fatalf("search field lost its full outline: top=%q bottom=%q", top, bottom)
				}
			}
			m, _ = update(m, tea.PasteMsg{Content: "节点"})
			m.scroll = 10
			m.layout()
			after := findTarget(t, m, "proxies.search")
			if before != after || hasAction(m.contentTargets(), after.ID) {
				t.Fatalf("editing or scrolling moved search: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestProxiesSearchShortcutRevealsFieldInShortScrollingBody(t *testing.T) {
	m := proxySearchScrolledReferenceModel(t, 40)
	m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 7})
	m.revealContentFocus()
	m.layout()
	if m.scroll == 0 || hasAction(m.contentTargets(), "proxies.search") {
		t.Fatal("short-window fixture should be browsing scrolled members without a search field")
	}
	location := m.proxyLocation()
	m, _ = update(m, key("/"))
	if !hasAction(m.targets, "proxies.search") || !hasAction(m.contentTargets(), "proxies.search") || m.View().Cursor == nil {
		t.Fatal("slash did not reveal the editable field")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if !reflect.DeepEqual(m.proxyLocation(), location) || hasAction(m.targets, "proxies.search") {
		t.Fatal("Escape did not restore the scrolled member list")
	}
}
