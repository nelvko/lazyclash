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
			m.abortLookup()
			m.wizard.specific = true
			m.wizard.version.SetValue("v1.19.33")
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

func TestNetworkTestPinsSharedStableVersionAndSelectedNetwork(t *testing.T) {
	cfg := downloadnet.Config{Method: "environment"}
	var calls []string
	b := &fakeBackend{
		checkUpdate: func(ctx context.Context, channel mihomo.Channel, used downloadnet.Config) (app.CoreUpdateCheck, error) {
			calls = append(calls, "check")
			if channel != mihomo.ChannelStable || used != cfg {
				t.Fatal("network test did not reuse the stable checker and selected download method")
			}
			return app.CoreUpdateCheck{LatestVersion: testRelease().Version, Channel: channel}, ctx.Err()
		},
		resolve: func(ctx context.Context, selection string, used downloadnet.Config) (mihomo.Release, error) {
			calls = append(calls, "resolve")
			if selection != testRelease().Version || used != cfg {
				t.Fatal("network test did not pin the checked version before resolving its package")
			}
			return testRelease(), ctx.Err()
		},
		testFile: func(ctx context.Context, release mihomo.Release, used downloadnet.Config) error {
			calls = append(calls, "file")
			if release != testRelease() || used != cfg {
				t.Fatal("package test changed the resolved release or selected download method")
			}
			return ctx.Err()
		},
	}
	m := ready(b)
	m.openNetwork(false)
	m.wizard.draft = cfg
	commands := commandBatch(t, m.testNetwork())
	m, probe := update(m, commands[0]())
	if probe == nil || strings.Join(calls, ",") != "check,resolve" || m.wizard.versionTest.Status != checkPassed {
		t.Fatal("version test did not prepare the pinned package connection test")
	}
	m, _ = update(m, probe())
	if m.busy || m.wizard.fileTest.Status != checkPassed || strings.Join(calls, ",") != "check,resolve,file" {
		t.Fatal("network test did not complete both checks through the shared release flow")
	}
}

func TestChangedReleaseIdentityReprobesFileAndIgnoresOldResult(t *testing.T) {
	for _, sample := range []struct {
		name   string
		change func(*mihomo.Release)
	}{
		{"version", func(r *mihomo.Release) { r.Version = "v1.20.0" }},
		{"asset", func(r *mihomo.Release) { r.Asset = "mihomo-linux-amd64-v1-v1.19.33.gz" }},
		{"url", func(r *mihomo.Release) { r.URL += "?download=1" }},
		{"platform", func(r *mihomo.Release) { r.Platform = "linux/amd64" }},
		{"size", func(r *mihomo.Release) { r.Size++ }},
	} {
		for _, oldFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/oldFirst=%v", sample.name, oldFirst), func(t *testing.T) {
				old := testRelease()
				current := old
				sample.change(&current)
				var probes []mihomo.Release
				b := &fakeBackend{resolve: func(context.Context, string, downloadnet.Config) (mihomo.Release, error) { return current, nil }, testFile: func(_ context.Context, r mihomo.Release, _ downloadnet.Config) error {
					probes = append(probes, r)
					if r == old {
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
				if reprobe == nil || m.wizard.testRelease != current || m.wizard.fileTest.Status != "Testing" {
					t.Fatal("changed release identity did not replace the file under test")
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
					t.Fatal("late old-release status overwrote the actual file under test")
				}
			})
		}
	}
}

func TestNetworkSaveLocksDraftAgainstKeyboardPasteAndMouse(t *testing.T) {
	m := ready(&fakeBackend{})
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openNetwork(false)
	m.wizard.draft = downloadnet.Config{Method: "accelerated", Prefix: "https://accelerator.example.invalid/"}
	m.wizard.prefix.SetValue(m.wizard.draft.Prefix)
	m.wizard.remember = true
	m.wizard.focus = "download-method"
	m.wizard.reveal = true
	m.layout()
	methodTarget := findTarget(t, m, "download-method")
	before := m.wizard.draft
	prefix := m.wizard.prefix.Value()
	ctx := m.newJob("save-network")
	id := m.id
	m.wizard.focus = "prefix"
	m, _ = update(m, key("q123"))
	m, _ = update(m, tea.PasteMsg{Content: "https://different.example.invalid/"})
	for _, control := range []string{"download-method", "remember"} {
		m.wizard.focus = control
		m, _ = update(m, special(tea.KeyRight))
		m, _ = update(m, special(tea.KeyEnter))
	}
	m.wizard.focus = "download-method"
	m.wizard.reveal = true
	m.layout()
	if hasAction(m.targets, "download-method") || hasAction(m.targets, "remember") {
		t.Fatal("preference save retained a clickable draft choice")
	}
	m, _ = click(m, methodTarget)
	if m.wizard.draft != before || m.wizard.prefix.Value() != prefix || !m.wizard.remember || m.id != id || !m.busy || ctx.Err() != nil {
		t.Fatal("input modified or canceled an in-flight preference save")
	}
	m.cancel()
}

func TestWrappedPageBodyTargetsMatchTheirPaintedRows(t *testing.T) {
	for _, width := range []int{18, 25, 40} {
		m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true, Groups: []mihomo.Group{{Name: "alpha", Type: "Selector", Now: "DIRECT", All: []string{"DIRECT"}}}}})
		m.snapshot.Service.Active = true
		m.page = "proxies"
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
		m.snapshot.ProfileDetails = []app.ProfileInfo{{ID: "default", Name: "default"}, {ID: "work", Name: "work"}}
		body, hits = m.pageBody(width)
		lines = strings.Split(body, "\n")
		for _, r := range hits {
			if r.ID == "profile.1" && (r.Y+r.H > len(lines) || !strings.Contains(ansi.Strip(lines[r.Y+1]), "work")) {
				t.Fatalf("wrapped import explanation shifted profile target: %+v", r)
			}
		}
	}
	m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true, Profiles: []string{"work"}, UnvalidatedProfiles: []string{"work"},
		ProfileDetails: []app.ProfileInfo{{ID: "default", Name: "default", Validated: true}, {ID: "work", Name: "work"}}}})
	m.page = "profiles"
	body, _ := m.pageBody(100)
	if !strings.Contains(ansi.Strip(body), "work") || !strings.Contains(ansi.Strip(body), "Engine validation pending") {
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
		report(mihomo.Progress{Stage: "extract", Detail: "final verification"})
		return app.InstallResult{}, errors.New("gzip integrity check failed")
	}}
	m := confirmModel(t, b)
	m, cmd := activateWizard(m, "install")
	id := m.id
	// Deliberately run only the result command: queued progress events can be
	// delivered later or dropped by the bounded progress channel.
	result := actionCmd(t, cmd)()
	m, _ = update(m, result)
	if m.wizard.failedStage != "extract" || m.wizard.progress.Stage != "extract" || m.wizard.step != stepResult {
		t.Fatal("result classified failure using stale deploy/download progress")
	}
	m, _ = update(m, nextProgressMsg{progressMsg: progressMsg{id: id, progress: mihomo.Progress{Stage: "download"}}})
	if m.wizard.progress.Stage != "extract" || m.wizard.failedStage != "extract" {
		t.Fatal("late progress overwrote final result stage")
	}
}

func TestV2ViewFlagsPasteMouseAndKeyboardReleaseSemantics(t *testing.T) {
	m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true}})
	view := m.View()
	if !view.AltScreen || view.MouseMode != tea.MouseModeAllMotion || view.DisableBracketedPasteMode || ansi.Strip(view.Content) != ansi.Strip(m.screen) {
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
	m.openManagementWizard(false)
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
	m.openManagementWizard(false)
	if m.wizard.err != "" || m.wizard.failedKind != "" || m.wizard.failedStage != "" {
		t.Fatal("new flow retained previous retry state")
	}
	m.wizard.release = testRelease()
	m.openNetwork(false)
	m.wizard.draft = downloadnet.Config{Method: "accelerated", Prefix: "https://accelerator.example.invalid/"}
	m.wizard.previewURL = true
	doc, _, _ := m.wizardDocument(300)
	preview := ansi.Strip(strings.Join(doc.lines, "\n"))
	actual := "https://accelerator.example.invalid/" + testRelease().URL
	if !strings.Contains(preview, actual) {
		t.Fatal("preview did not show exact nested official release URL")
	}
	m.wizard.release = mihomo.Release{}
	doc, _, _ = m.wizardDocument(300)
	preview = ansi.Strip(strings.Join(doc.lines, "\n"))
	if !strings.Contains(preview, "https://github.com/MetaCubeX/mihomo/releases/latest/download/version.txt") || strings.Contains(preview, testRelease().Asset) {
		t.Fatal("unresolved preview did not use the real version query URL")
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
	m.proxies.expanded = "Work"
	m, _ = update(m, tea.WindowSizeMsg{Width: 160, Height: 45})
	if !strings.Contains(ansi.Strip(m.View().Content), "work-node") {
		t.Fatal("model did not pass session group selection to Overview")
	}
}
