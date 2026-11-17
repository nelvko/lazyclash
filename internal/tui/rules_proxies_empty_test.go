package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestRulesEmptyStateDistinguishesSourceAndSearch(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{80, 24}, {60, 18}} {
			for _, state := range []string{"empty", "no matches", "stopped", "failed", "invalid search"} {
				t.Run(fmt.Sprintf("%s/%v/%s", language, size, state), func(t *testing.T) {
					m := rulesModel(t, &fakeBackend{})
					m.locale = i18n.New(language)
					m.width, m.height = size[0], size[1]
					want, action := "rules.empty", ""
					switch state {
					case "empty":
						m.rules.snapshot.Rules = nil
						m.rules.search.SetValue("unmatched query")
					case "no matches":
						m.rules.search.SetValue("unmatched query")
						want, action = "rules.no_matches", "rules.empty.clear"
					case "stopped":
						m.snapshot.Service.Active, m.snapshot.Service.State = false, "inactive"
						m.rules.snapshot = app.RuleSnapshot{State: "stopped"}
						m.rules.search.SetValue("unmatched query")
						want, action = "runtime.status.stopped", "core.start"
					case "failed":
						m.rules.snapshot = app.RuleSnapshot{State: "unavailable"}
						m.rules.err = errors.New("rule read diagnostic")
						m.rules.search.SetValue("unmatched query")
						want, action = "empty.rules.failed", "rules.retry"
					case "invalid search":
						m.rules.search.SetValue("[")
						m.rules.regex = true
						want, action = "rules.invalid_regex", "rules.empty.clear"
					}
					g := dashboardGeometry(m.dashboardOptions())
					body := m.rulesBodyWindow(g.width, 0, -1)
					plain := ansi.Strip(strings.Join(body.lines, "\n"))
					if !strings.Contains(plain, m.tr(want)) {
						t.Fatalf("wrong empty-state category: %s", plain)
					}
					if action != "" && !hasAction(body.targets, action) {
						t.Fatalf("missing recovery action %s: %+v", action, body.targets)
					}
					assertRulesProxyEmptyCentered(t, body.lines, body.targets, g.width, g.height)
					m.layout()
					assertCanvas(t, m.screen, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
				})
			}
		}
	}
}

func TestProxiesEmptyStateDistinguishesDirectorySearchAndFailure(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{80, 24}, {60, 18}} {
			for _, state := range []string{"empty", "no matches", "stopped", "failed", "direct", "empty group"} {
				t.Run(fmt.Sprintf("%s/%v/%s", language, size, state), func(t *testing.T) {
					m, backend := proxiesModel(t, size[0], size[1])
					m.locale = i18n.New(language)
					m.proxies.activeGroup, m.proxies.history = "", nil
					m.proxies.expanded = map[string]bool{}
					want, action := "proxies.no_groups", ""
					switch state {
					case "empty":
						m.snapshot.Groups = nil
						m.proxies.search.SetValue("unmatched query")
					case "no matches":
						m.proxies.search.SetValue("unmatched query")
						want, action = "proxies.search_no_match", "proxies.empty.clear"
					case "stopped":
						m.snapshot.Service.Active, m.snapshot.Service.State = false, "inactive"
						m.proxies.search.SetValue("unmatched query")
						want, action = "runtime.status.stopped", "core.start"
					case "failed":
						m.snapshot.APIError = "proxy read diagnostic"
						m.proxies.search.SetValue("unmatched query")
						want = "empty.proxies.failed"
					case "direct":
						m.snapshot.Mode.Actual, m.snapshot.Mode.Desired = "direct", "direct"
						want = "proxies.direct.description"
					case "empty group":
						m.snapshot.Groups = []mihomo.Group{{Name: "Empty", Type: "Selector"}}
						m.proxies.activeGroup = "Empty"
						want = "proxies.empty_group"
					}
					g := dashboardGeometry(m.dashboardOptions())
					body, hits := m.proxiesBody(g.width)
					plain := ansi.Strip(body)
					if !strings.Contains(plain, m.tr(want)) {
						t.Fatalf("wrong empty-state category: %s", plain)
					}
					if action != "" && !hasAction(hits, action) {
						t.Fatalf("missing recovery action %s: %+v", action, hits)
					}
					if state != "empty group" {
						assertRulesProxyEmptyCentered(t, strings.Split(body, "\n"), hits, g.width, g.height)
					}
					if state == "no matches" && m.proxies.search.Value() != "unmatched query" {
						t.Fatal("counting the unfiltered directory mutated the live search")
					}
					if backend.selections != 0 {
						t.Fatal("rendering an empty state selected a proxy")
					}
					m.layout()
					assertCanvas(t, m.screen, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
				})
			}
		}
	}
}

func TestRulesAndProxyEmptySearchRecoveryKeepsPage(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	m.rules.search.SetValue("unmatched query")
	m.focus, m.scroll = "rules.empty.clear", 1000
	m.layout()
	assertEmptySearchTabStop(t, m, "rules.empty.clear")
	if m.contextHelp() != m.tr("empty.rules.clear_hint") {
		t.Fatal("rule empty-state recovery did not advertise its Enter action")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.page != "rules" || m.scroll != 0 || m.rules.search.Value() != "" || len(m.filteredRules()) != len(sampleRules()) || !m.rules.editing {
		t.Fatal("rule empty-state recovery did not restore the rule search")
	}
	proxy, backend := proxiesModel(t, 80, 24)
	proxy.proxies.search.SetValue("unmatched query")
	proxy.focus, proxy.scroll = "proxies.empty.clear", 1000
	proxy.layout()
	assertEmptySearchTabStop(t, proxy, "proxies.empty.clear")
	if !strings.HasPrefix(proxy.contextHelp(), "Enter") {
		t.Fatal("proxy empty-state recovery did not advertise its Enter action")
	}
	proxy, _ = update(proxy, special(tea.KeyEnter))
	if proxy.page != "proxies" || proxy.scroll != 0 || proxy.proxies.search.Value() != "" || len(proxy.visibleProxyGroups()) == 0 || !proxy.proxies.editing || backend.selections != 0 {
		t.Fatal("proxy empty-state recovery did not restore search without selecting")
	}
}

func assertEmptySearchTabStop(t *testing.T, m Model, id string) {
	t.Helper()
	count := 0
	for _, target := range m.focusTargets() {
		if target.ID == id {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("empty-state action %s has %d Tab stops", id, count)
	}
	if !hasAction(m.targets, id) || m.scroll != 0 {
		t.Fatalf("empty-state action %s is hidden by stale scrolling", id)
	}
}

func assertRulesProxyEmptyCentered(t *testing.T, lines []string, hits []hitRegion, width, height int) {
	t.Helper()
	first, last := -1, -1
	for y, line := range lines {
		plain := ansi.Strip(line)
		if strings.TrimSpace(plain) == "" {
			continue
		}
		if first < 0 {
			first = y
		}
		last = y
		left := len(plain) - len(strings.TrimLeft(plain, " "))
		occupied := ansi.StringWidth(strings.TrimSpace(plain))
		for _, hit := range hits {
			if hit.Y == y {
				// Buttons reserve their focus gutter even when it is blank.
				left, occupied = hit.X, hit.W
				break
			}
		}
		right := width - left - occupied
		if left-right < -1 || left-right > 1 {
			t.Fatalf("empty row is not horizontally centered: %q", plain)
		}
	}
	if first < 0 || len(lines) != height || first-(height-last-1) < -1 || first-(height-last-1) > 1 {
		t.Fatalf("empty state is not vertically centered in %d rows: first=%d last=%d actual=%d", height, first, last, len(lines))
	}
}
