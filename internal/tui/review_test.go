package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestCachedReleaseRemainsAvailableForIndependentFileTest(t *testing.T) {
	for _, changedSelection := range []bool{false, true} {
		t.Run(fmt.Sprint(changedSelection), func(t *testing.T) {
			calls, files := 0, 0
			b := &fakeBackend{resolve: func(context.Context, string, downloadnet.Config) (mihomo.Release, error) {
				calls++
				if calls == 1 {
					return testRelease(), nil
				}
				return mihomo.Release{}, errors.New("metadata unavailable")
			}, testFile: func(_ context.Context, r mihomo.Release, _ downloadnet.Config) error {
				files++
				if r.Asset != testRelease().Asset {
					t.Fatal("wrong cached file")
				}
				return nil
			}}
			m := ready(b)
			m.openWizard(false)
			m.wizard.step = stepVersion
			m, cmd := update(m, actionCmd(t, m.resolveVersion())())
			if m.wizard.cachedRelease.Asset == "" {
				t.Fatal("successful metadata did not preserve a cache")
			}
			if changedSelection {
				m.wizard.specific = true
				m.wizard.version.SetValue("v1.20.0")
			}
			m, cmd = update(m, actionCmd(t, m.resolveVersion())())
			if m.wizard.release.Asset != "" || m.wizard.err == "" {
				t.Fatal("lookup did not fail as expected")
			}
			m.openNetwork(true)
			m, cmd = activateWizard(m, "test")
			commands := commandBatch(t, cmd)
			if changedSelection {
				if len(commands) != 2 || m.wizard.fileTest.Status != "Not applicable" {
					t.Fatal("a different selection reused the old asset")
				}
				m.cancel()
				return
			}
			if len(commands) != 3 || m.wizard.fileTest.Status != "Testing" {
				t.Fatal("same-selection metadata failure hid independent file test")
			}
			m, _ = update(m, commands[0]())
			m, _ = update(m, commands[2]())
			if files != 1 || m.wizard.versionTest.Status != "Failed" || m.wizard.fileTest.Status != "Passed" || m.busy {
				t.Fatal("cached file result was coupled to metadata failure")
			}
		})
	}
}

func TestChangedLatestReleaseReprobesFileAndIgnoresOldAssetResult(t *testing.T) {
	for _, oldFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(oldFirst), func(t *testing.T) {
			old := testRelease()
			current := old
			current.Version = "v1.20.0"
			current.Asset = strings.ReplaceAll(old.Asset, old.Version, current.Version)
			current.URL = strings.ReplaceAll(old.URL, old.Version, current.Version)
			current.SHA256 = strings.Repeat("b", 64)
			var probes []string
			b := &fakeBackend{resolve: func(context.Context, string, downloadnet.Config) (mihomo.Release, error) { return current, nil }, testFile: func(_ context.Context, r mihomo.Release, _ downloadnet.Config) error {
				probes = append(probes, r.Asset)
				if r.Asset == old.Asset {
					return errors.New("old file unavailable")
				}
				return nil
			}}
			m := ready(b)
			m.wizard.release = old
			m.openNetwork(false)
			m, cmd := activateWizard(m, "test")
			commands := commandBatch(t, cmd)
			m, reprobe := update(m, commands[0]())
			if reprobe == nil || m.wizard.testRelease.Asset != current.Asset || m.wizard.fileTest.Status != "Testing" {
				t.Fatal("new metadata did not replace the file under test")
			}
			if oldFirst {
				m, _ = update(m, commands[2]())
				if m.wizard.fileTest.Status != "Testing" || !m.busy {
					t.Fatal("old result completed the new file test")
				}
				m, _ = update(m, reprobe())
			} else {
				m, _ = update(m, reprobe())
				if m.wizard.fileTest.Status != "Passed" || !m.busy {
					t.Fatal("new result should still await old request cleanup")
				}
				m, _ = update(m, commands[2]())
			}
			if m.busy || m.wizard.fileTest.Status != "Passed" || !strings.Contains(m.wizard.fileTest.Detail, current.Asset) || strings.Contains(m.wizard.fileTest.Detail, "old file unavailable") || len(probes) != 2 {
				t.Fatal("late old-asset status overwrote the actual latest file")
			}
		})
	}
}

func TestNetworkSaveLocksDraftAgainstKeyboardPasteAndMouse(t *testing.T) {
	m := ready(&fakeBackend{})
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openNetwork(false)
	m.wizard.draft = downloadnet.Config{Source: "prefix", Prefix: "https://accelerator.example.invalid/", ProxyMode: "none"}
	m.wizard.prefix.SetValue(m.wizard.draft.Prefix)
	m.wizard.remember = true
	m.wizard.focus = "source"
	m.wizard.reveal = true
	m.layout()
	before := m.wizard.draft
	prefix := m.wizard.prefix.Value()
	ctx := m.newJob("save-network")
	id := m.id
	m.wizard.focus = "prefix"
	m, _ = update(m, key("q123"))
	m, _ = update(m, tea.PasteMsg{Content: "https://different.example.invalid/"})
	for _, control := range []string{"source", "proxy-mode", "remember"} {
		m.wizard.focus = control
		m, _ = update(m, special(tea.KeyRight))
		m, _ = update(m, special(tea.KeyEnter))
	}
	m.wizard.focus = "source"
	m.wizard.reveal = true
	m.layout()
	m, _ = click(m, findTarget(t, m, "source"))
	if m.wizard.draft != before || m.wizard.prefix.Value() != prefix || !m.wizard.remember || m.id != id || !m.busy || ctx.Err() != nil {
		t.Fatal("input modified or canceled an in-flight preference save")
	}
	m.cancel()
}

func TestWrappedPageBodyTargetsMatchTheirPaintedRows(t *testing.T) {
	for _, width := range []int{18, 25, 40} {
		m := ready(&fakeBackend{snapshot: app.Snapshot{APIError: strings.Repeat("API unavailable 中文 ", 20), Groups: []mihomo.Group{{Name: "alpha", Type: "Selector", Now: "DIRECT", All: []string{"DIRECT"}}}}})
		m.page = "nodes"
		body, hits := m.pageBody(width)
		lines := strings.Split(body, "\n")
		found := false
		for _, r := range hits {
			if r.ID == "group.0" {
				found = true
				if r.Y >= len(lines) || !strings.Contains(ansi.Strip(lines[r.Y]), "alpha") {
					t.Fatalf("group target row wrong at %d columns: %+v", width, r)
				}
			}
		}
		if !found {
			t.Fatal("missing group target")
		}
		m.page = "profiles"
		m.snapshot.Profiles = []string{"work"}
		m.snapshot.UnvalidatedProfiles = []string{"work"}
		body, hits = m.pageBody(width)
		lines = strings.Split(body, "\n")
		for _, r := range hits {
			if r.ID == "profile.1" && !strings.Contains(ansi.Strip(lines[r.Y]), "work") {
				t.Fatalf("wrapped import explanation shifted profile target: %+v", r)
			}
		}
	}
	m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true, Profiles: []string{"work"}, UnvalidatedProfiles: []string{"work"}}})
	m.page = "profiles"
	body, _ := m.pageBody(100)
	if !strings.Contains(ansi.Strip(body), "work [engine validation pending]") {
		t.Fatal("preimported profile lost its validation status after installation")
	}
}

func TestResizeKeepsFocusedWizardBodyControlVisible(t *testing.T) {
	m := ready(&fakeBackend{})
	m.openNetwork(false)
	m.wizard.focus = "remember"
	m.wizard.reveal = true
	m, _ = update(m, tea.WindowSizeMsg{Width: 160, Height: 45})
	for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 20}, {120, 18}, {100, 28}, {120, 32}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if m.wizard.focus != "remember" {
			t.Fatal("resize changed focus")
		}
		target := findTarget(t, m, "remember")
		line := strings.Split(m.View().Content, "\n")[target.Y]
		if !strings.Contains(ansi.Strip(ansi.Cut(line, target.X, target.X+target.W)), "Remember") {
			t.Fatalf("focused target not actually visible at %v", size)
		}
	}
}

func TestInstallResultCarriesFinalProgressBeforeErrorClassification(t *testing.T) {
	b := &fakeBackend{install: func(_ context.Context, _ app.InstallPlan, _ downloadnet.Config, report func(mihomo.Progress)) (app.InstallResult, error) {
		for i := 0; i < 80; i++ {
			report(mihomo.Progress{Stage: "download", Downloaded: int64(i)})
		}
		report(mihomo.Progress{Stage: "checksum", Detail: "final verification"})
		return app.InstallResult{}, errors.New("checksum mismatch")
	}}
	m := confirmModel(t, b)
	m, cmd := activateWizard(m, "install")
	id := m.id
	// Deliberately run only the result command: queued progress events can be
	// delivered later or dropped by the bounded progress channel.
	result := actionCmd(t, cmd)()
	m, _ = update(m, result)
	if m.wizard.failedStage != "Verifying checksum" || m.wizard.progress.Stage != "checksum" || m.wizard.step != stepResult {
		t.Fatal("result classified failure using stale deploy/download progress")
	}
	m, _ = update(m, nextProgressMsg{progressMsg: progressMsg{id: id, progress: mihomo.Progress{Stage: "download"}}})
	if m.wizard.progress.Stage != "checksum" || m.wizard.failedStage != "Verifying checksum" {
		t.Fatal("late progress overwrote final result stage")
	}
}

func TestV2ViewFlagsPasteMouseAndKeyboardReleaseSemantics(t *testing.T) {
	m := ready(&fakeBackend{})
	view := m.View()
	if !view.AltScreen || view.MouseMode != tea.MouseModeAllMotion || view.DisableBracketedPasteMode || view.Content != m.screen {
		t.Fatal("v2 View did not request terminal capabilities")
	}
	m, _ = update(m, tea.PasteMsg{Content: "q"})
	if m.quitting || m.page != "overview" {
		t.Fatal("dashboard paste acted as a shortcut")
	}
	m, _ = update(m, tea.KeyReleaseMsg{Code: 'i', Text: "i"})
	if m.wizard.open {
		t.Fatal("key release activated a shortcut")
	}
	m, _ = update(m, key("i"))
	m.wizard.step = stepVersion
	m.wizard.specific = true
	m.wizard.focus = "version-input"
	m.layout()
	m, _ = update(m, tea.PasteMsg{Content: "q1234"})
	if m.wizard.version.Value() != "q1234" || m.quitting || m.page != "overview" || m.wizard.step != stepVersion {
		t.Fatal("bracketed paste escaped wizard input")
	}
	m.wizard.open = false
	m.layout()
	m, _ = update(m, key("a"))
	m, _ = update(m, tea.PasteMsg{Content: "q1234"})
	if m.input.Value() != "q1234" || m.quitting || m.page != "overview" {
		t.Fatal("bracketed paste escaped import input")
	}
	m.form = ""
	m, _ = update(m, tea.WindowSizeMsg{Width: 160, Height: 45})
	target := findTarget(t, m, "nav.settings")
	m, _ = update(m, tea.MouseMotionMsg{X: target.X, Y: target.Y, Button: tea.MouseNone})
	if m.hover != "nav.settings" || m.page != "overview" {
		t.Fatal("motion should hover without activating")
	}
	m, _ = update(m, tea.MouseReleaseMsg{X: target.X, Y: target.Y, Button: tea.MouseLeft})
	if m.page != "overview" {
		t.Fatal("mouse release activated navigation")
	}
	m, _ = click(m, target)
	if m.page != "settings" {
		t.Fatal("v2 click failed to navigate")
	}
	m.focus = "network"
	m, _ = update(m, key(" "))
	if !m.wizard.networkOpen {
		t.Fatal("v2 space key failed to activate focused control")
	}
}

func TestNewWizardClearsPreviousFailureAndNetworkPreviewIsActual(t *testing.T) {
	m := ready(&fakeBackend{})
	m.wizard.err = "old failure"
	m.wizard.failedKind = "install"
	m.wizard.failedStage = "Downloading"
	m.openWizard(false)
	if m.wizard.err != "" || m.wizard.failedKind != "" || m.wizard.failedStage != "" {
		t.Fatal("new flow retained previous retry state")
	}
	m.wizard.release = testRelease()
	m.openNetwork(false)
	m.wizard.draft = downloadnet.Config{Source: "prefix", Prefix: "https://accelerator.example.invalid/", ProxyMode: "none"}
	doc, _, _ := m.wizardDocument(300)
	preview := ansi.Strip(strings.Join(doc.lines, "\n"))
	actual := "https://accelerator.example.invalid/" + testRelease().URL
	if !strings.Contains(preview, actual) {
		t.Fatal("preview did not show exact nested official release URL")
	}
	m.wizard.release = mihomo.Release{}
	doc, _, _ = m.wizardDocument(300)
	preview = ansi.Strip(strings.Join(doc.lines, "\n"))
	if strings.Contains(preview, "https://github.com/") || !strings.Contains(preview, "Resolve a version") {
		t.Fatal("unresolved preview fabricated an asset address")
	}
}

func TestOverviewReflectsSelectedGroupAndPrefersPROXY(t *testing.T) {
	snapshot := app.Snapshot{Installed: true, Groups: []mihomo.Group{{Name: "GLOBAL", Type: "Selector", Now: "global-node"}, {Name: "PROXY", Type: "Selector", Now: "proxy-node"}, {Name: "Work", Type: "Selector", Now: "work-node"}}}
	snapshot.Service.Active = true
	for _, tc := range []struct{ selected, want string }{{"", "proxy-node"}, {"Work", "work-node"}, {"GLOBAL", "global-node"}, {"missing", "proxy-node"}} {
		cards := overviewCards(dashboardOptions{Snapshot: snapshot, SelectedGroup: tc.selected})
		value := ansi.Strip(strings.Join(cards[1], "\n"))
		if !strings.Contains(value, tc.want) {
			t.Fatalf("selected %q: %s", tc.selected, value)
		}
	}
	m := ready(&fakeBackend{snapshot: snapshot})
	m.wizard.expanded = "Work"
	m, _ = update(m, tea.WindowSizeMsg{Width: 160, Height: 45})
	if !strings.Contains(ansi.Strip(m.View().Content), "work-node") {
		t.Fatal("model did not pass session group selection to Overview")
	}
}
