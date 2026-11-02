package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

type onboardingBackend struct {
	*fakeBackend
	guideSaves int
}

func (b *onboardingBackend) SetGuideDismissed(context.Context, bool) error {
	b.guideSaves++
	return nil
}

func onboardingReady(t *testing.T, backend Backend, language string) Model {
	t.Helper()
	m := New(t.Context(), backend, language)
	m, cmd := update(m, m.snapshotCmd(m.id, m.opCtx, true)())
	if m.wizard.inline && m.job == "check-update" {
		m, _ = update(m, inlineOperationMsg(t, cmd))
	}
	return m
}

func onboardingDefaultSnapshot() app.Snapshot {
	return app.Snapshot{
		Installed: true, ConfigReady: true, Version: "v1.19.33",
		Applied: "default", Profiles: []string{"default"},
		Service:      service.Status{State: "inactive"},
		Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
		Mode:         app.ModeStatus{Desired: "rule"},
	}
}

func TestOnboardingInitialMessagesFocusInstallWithoutStartingWork(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 20}} {
			for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
				t.Run(fmt.Sprintf("%s/%dx%d/%v", language, size[0], size[1], order), func(t *testing.T) {
					calls := 0
					b := &fakeBackend{
						resolve: func(context.Context, string, downloadnet.Config) (mihomo.Release, error) {
							calls++
							return testRelease(), nil
						},
						install: func(context.Context, app.InstallPlan, downloadnet.Config, func(mihomo.Progress)) (app.InstallResult, error) {
							calls++
							return app.InstallResult{}, nil
						},
						control: func(context.Context, string) error { calls++; return nil },
					}
					m := New(t.Context(), b, language)
					messages := []tea.Msg{
						tea.WindowSizeMsg{Width: size[0], Height: size[1]},
						tea.ColorProfileMsg{Profile: colorprofile.TrueColor},
						m.snapshotCmd(m.id, m.opCtx, true)(),
					}
					var check tea.Cmd
					for _, index := range order {
						var cmd tea.Cmd
						m, cmd = update(m, messages[index])
						if index == 2 {
							check = cmd
						}
					}
					if m.focus != "setup.install" || !m.busy || m.wizard.open || !m.wizard.inline || calls != 0 {
						t.Fatalf("startup did not focus the read-only inline setup: focus=%s busy=%v wizard=%v calls=%d", m.focus, m.busy, m.wizard.open, calls)
					}
					assertCanvas(t, m.screen, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
					for _, id := range []string{"setup.install", "setup.later"} {
						hit := findTarget(t, m, id)
						painted := ansi.Strip(ansi.Cut(strings.Split(m.screen, "\n")[hit.Y], hit.X, hit.X+hit.W))
						label := m.tr("wizard.download_install")
						if id == "setup.later" {
							label = m.tr("dashboard.guide.later")
						}
						if !strings.Contains(painted, label) {
							t.Fatalf("%s target misses translated label %q: %q", id, label, painted)
						}
					}
					beforeID := m.id
					m, _ = update(m, special(tea.KeyEnter))
					if m.wizard.open || m.wizard.step != stepVersion || !m.busy || m.id != beforeID || calls != 0 {
						t.Fatalf("pending version check accepted a duplicate install: step=%s busy=%v calls=%d", m.wizard.step, m.busy, calls)
					}
					m, _ = update(m, inlineOperationMsg(t, check))
					if m.busy || m.wizard.updateCheck.LatestVersion != "v1.19.33" || calls != 0 {
						t.Fatal("checking the recommended version resolved, installed or started a core")
					}
				})
			}
		}
	}
}

func TestOnboardingUnknownInstallationDoesNotSuggestFreshSetup(t *testing.T) {
	b := &fakeBackend{snapshotErr: errors.New("installation state is unreadable")}
	m := onboardingReady(t, b, "en")
	if m.snapshotReady || overviewSetupVisible(m.navigationOptions()) || hasAction(m.targets, "install") {
		t.Fatal("failed state read was presented as a confirmed fresh installation")
	}
	if !strings.Contains(m.failure, "installation state is unreadable") {
		t.Fatal("initial state read failure was hidden")
	}
}

func TestOnboardingLateInitialSnapshotPreservesUserNavigation(t *testing.T) {
	m := New(t.Context(), &fakeBackend{}, "en")
	initial := m.snapshotCmd(m.id, m.opCtx, true)()
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	m, _ = update(m, key("7"))
	before := m.focus
	m, _ = update(m, initial)
	if m.page != "settings" || m.focus != before || m.wizard.open {
		t.Fatalf("late initial state took focus from the chosen page: page=%s focus=%s want=%s", m.page, m.focus, before)
	}
	before = m.focus
	m, _ = update(m, resultMsg{id: m.id - 1, hasSnapshot: true, snapshot: onboardingDefaultSnapshot()})
	if m.page != "settings" || m.focus != before || m.snapshot.Installed {
		t.Fatal("stale state result changed the chosen page or installation state")
	}
}

func TestOnboardingSetUpLaterOnlySkipsTheCurrentSession(t *testing.T) {
	b := &onboardingBackend{fakeBackend: &fakeBackend{}}
	m := onboardingReady(t, b, "en")
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	beforeID := m.id
	m, _ = click(m, findTarget(t, m, "setup.later"))
	if b.guideSaves != 0 || b.prefs.GuideDismissed || m.snapshot.GuideDismissed || m.busy || m.id != beforeID {
		t.Fatal("set up later persisted a guide preference or started a backend operation")
	}
	if overviewSetupVisible(m.navigationOptions()) || hasAction(m.targets, "setup.later") {
		t.Fatal("set up later did not leave the preparation view")
	}
	m, _ = update(m, key("3"))
	if m.page != "profiles" || m.wizard.open {
		t.Fatal("leaving setup prevented ordinary page navigation")
	}
	m, _ = update(m, key("1"))
	if m.page != "overview" || overviewSetupVisible(m.navigationOptions()) {
		t.Fatal("returning to Overview reopened setup during the same session")
	}
	m, _ = update(m, key("i"))
	if m.wizard.open || !m.wizard.inline || m.wizard.step != stepVersion || b.guideSaves != 0 {
		t.Fatal("set up later removed the explicit installation entry point")
	}
	next := onboardingReady(t, b, "en")
	if next.focus != "setup.install" || !overviewSetupVisible(next.navigationOptions()) {
		t.Fatal("a session-only skip suppressed setup on the next launch")
	}
}

func TestOnboardingDefaultConfigurationNeedsNoSubscriptionOrGuideCompletion(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			calls := 0
			b := &fakeBackend{snapshot: onboardingDefaultSnapshot()}
			b.control = func(_ context.Context, action string) error {
				calls++
				if action != "start" {
					t.Fatalf("unexpected default-configuration action %q", action)
				}
				b.snapshot.Service = service.Status{Active: true, State: "active"}
				b.snapshot.RunningVersion = b.snapshot.Version
				b.snapshot.Groups = []mihomo.Group{{Name: "PROXY", Type: "Selector", Now: "DIRECT", All: []string{"DIRECT", "REJECT"}}}
				return nil
			}
			m := onboardingReady(t, b, language)
			defer m.stopRuntime()
			defer m.stopCoreStatus()
			m.width, m.height = 120, 32
			m.layout()
			body, actions := overviewContent(m.navigationOptions(), dashboardBodyWidth(m.dashboardOptions()))
			plain := ansi.Strip(body)
			if label := m.tr("dashboard.profile.builtinReady"); !strings.Contains(plain, label) {
				t.Fatalf("default configuration is not presented as ready: missing %q\n%s", label, plain)
			}
			if name := snapshotProfileName(m.snapshot, m.snapshot.Applied, m.locale); name != m.tr("profiles.builtin_name") {
				t.Fatalf("the default configuration uses an internal identifier instead of its normal display name: %q", name)
			}
			if overviewSetupVisible(m.navigationOptions()) || hasAction(actions, "install") || hasAction(actions, "guide.later") || !hasAction(actions, "core.start") || calls != 0 || m.busy {
				t.Fatal("a subscription or completed guide was required before explicitly starting the built-in configuration")
			}
			m.focus = "core.start"
			m.layout()
			m, cmd := update(m, special(tea.KeyEnter))
			if !m.busy || calls != 0 || m.snapshot.Service.Active {
				t.Fatal("explicit Start did not preserve the observed stopped state while waiting for the backend")
			}
			m, _ = update(m, actionCmd(t, cmd)())
			if !m.snapshot.Service.Active || m.busy || calls != 1 || overviewSetupVisible(m.navigationOptions()) {
				t.Fatal("default configuration did not reach ordinary running state after explicit Start")
			}
			body, _ = overviewContent(m.navigationOptions(), dashboardBodyWidth(m.dashboardOptions()))
			plain = ansi.Strip(body)
			for _, label := range []string{"PROXY", "DIRECT"} {
				if !strings.Contains(plain, label) {
					t.Fatalf("normal built-in running strategy missing %q", label)
				}
			}
			if name := snapshotProfileName(m.snapshot, m.snapshot.Applied, m.locale); name != m.tr("profiles.builtin_name") {
				t.Fatal("starting the core replaced the built-in configuration's normal display name")
			}
		})
	}
}

func TestOnboardingInstallationResultKeepsDefaultReadyAndStopped(t *testing.T) {
	calls := 0
	b := &fakeBackend{control: func(context.Context, string) error { calls++; return nil }}
	b.install = func(_ context.Context, plan app.InstallPlan, _ downloadnet.Config, _ func(mihomo.Progress)) (app.InstallResult, error) {
		b.snapshot = onboardingDefaultSnapshot()
		return app.InstallResult{InstalledVersion: plan.Release.Version, Operation: "install"}, nil
	}
	m := confirmModel(t, b)
	m, cmd := activateWizard(m, "install")
	m, _ = update(m, actionCmd(t, cmd)())
	if !m.snapshot.ConfigReady || m.snapshot.Applied != "default" || m.snapshot.Service.Active || calls != 0 {
		t.Fatal("installation did not retain the ready, stopped built-in configuration")
	}
	m, _ = activateWizard(m, "done")
	if m.wizard.open || m.page != "overview" || overviewSetupVisible(m.navigationOptions()) || !hasAction(m.contentTargets(), "core.start") || calls != 0 {
		t.Fatal("leaving the installation result required another onboarding step or started the core implicitly")
	}
}

func TestOnboardingPresentationHasOnePreparationStepAcrossLanguagesAndSizes(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		locale := i18n.New(language)
		for _, size := range [][2]int{{160, 45}, {120, 32}, {100, 20}, {80, 24}, {60, 20}, {30, 12}, {20, 7}, {10, 4}, {1, 1}} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, size[0], size[1]), func(t *testing.T) {
				o := dashboardOptions{Locale: locale, Width: size[0], Height: size[1], Page: "overview", Focus: "install"}
				body, actions := overviewContent(o, dashboardBodyWidth(o))
				plain := ansi.Strip(body)
				if strings.Contains(plain, "1 / 3") || strings.Contains(plain, locale.T("dashboard.guide.steps", 1)) || hasAction(actions, "import") || hasAction(actions, "dismiss") {
					t.Fatal("initial setup retained a fixed import/start checklist or a persistent-dismiss action")
				}
				if cards := overviewCardRegions(body, o); len(cards) > 1 {
					t.Fatal("unavailable dashboard cards distract from the initial installation panel")
				}
				for _, label := range []string{locale.T("dashboard.control.routingMode"), locale.T("dashboard.traffic.title")} {
					if strings.Contains(plain, label) {
						t.Fatalf("unavailable dashboard card distracts from initial installation: %q", label)
					}
				}
				screen, hits, _ := renderDashboard(o)
				assertCanvas(t, screen, size[0], size[1])
				assertRegions(t, hits, size[0], size[1])
			})
		}
	}
}
