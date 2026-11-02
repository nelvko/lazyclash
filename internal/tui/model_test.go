package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

type fakeBackend struct {
	checkUpdate          func(context.Context, mihomo.Channel, downloadnet.Config) (app.CoreUpdateCheck, error)
	upgradeCore          func(context.Context, app.CoreUpdateCheck, downloadnet.Config, func(mihomo.Progress)) (app.CoreUpgradeResult, error)
	snapshot             app.Snapshot
	snapshotErr          error
	prefs                app.Preferences
	prefsErr             error
	saveLanguage         func(context.Context, string) error
	resolve              func(context.Context, string, downloadnet.Config) (mihomo.Release, error)
	testFile             func(context.Context, mihomo.Release, downloadnet.Config) error
	prepare              func(context.Context, mihomo.Release, string, bool) (app.InstallPlan, error)
	install              func(context.Context, app.InstallPlan, downloadnet.Config, func(mihomo.Progress)) (app.InstallResult, error)
	save                 func(context.Context, downloadnet.Config) error
	add                  func(context.Context, string, string) error
	addSubscription      func(context.Context, string, string) error
	createProfile        func(context.Context, app.ProfileDraft) error
	renameProfile        func(context.Context, string, string) error
	updateDescription    func(context.Context, string, string) error
	updateProfileAddress func(context.Context, string, string) error
	refresh              func(context.Context, string) error
	refreshAllProfiles   func(context.Context) error
	deleteProfile        func(context.Context, string) error
	setOverrides         func(context.Context, string, []byte) error
	control              func(context.Context, string) error
	autostart            func(context.Context, bool) error
	mode                 func(context.Context, string) error
	runtime              func(context.Context) (app.RuntimeSnapshot, error)
	rules                func(context.Context) (app.RuleSnapshot, error)
	streamLogs           func(context.Context, func(mihomo.LogEntry)) error
	closeConnection      func(context.Context, string) error
	getSettings          func(context.Context) (app.RuntimeSettings, error)
	prepareSettings      func(context.Context, app.RuntimeSettings) (app.RuntimeSettingsPlan, error)
	applySettings        func(context.Context, app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error)
}

func (f *fakeBackend) GetRuntimeSettings(ctx context.Context) (app.RuntimeSettings, error) {
	if f.getSettings != nil {
		return f.getSettings(ctx)
	}
	return app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}, ctx.Err()
}
func (f *fakeBackend) PrepareRuntimeSettings(ctx context.Context, settings app.RuntimeSettings) (app.RuntimeSettingsPlan, error) {
	if f.prepareSettings != nil {
		return f.prepareSettings(ctx, settings)
	}
	current := app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}
	return app.RuntimeSettingsPlan{Current: current, Proposed: settings, ControllerHost: "127.0.0.1", Active: f.snapshot.Service.Active, Changed: current != settings, AppliedProfile: f.snapshot.Applied}, ctx.Err()
}
func (f *fakeBackend) ApplyRuntimeSettings(ctx context.Context, plan app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
	if f.applySettings != nil {
		return f.applySettings(ctx, plan)
	}
	return app.RuntimeSettingsResult{Settings: plan.Proposed, Controller: fmt.Sprintf("%s:%d", plan.ControllerHost, plan.Proposed.ControllerPort), Changed: plan.Changed, Restarted: plan.Active && plan.Changed}, ctx.Err()
}

func (f *fakeBackend) StreamLogs(ctx context.Context, emit func(mihomo.LogEntry)) error {
	if f.streamLogs != nil {
		return f.streamLogs(ctx, emit)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeBackend) Snapshot(context.Context) (app.Snapshot, error) {
	return f.snapshot, f.snapshotErr
}
func (f *fakeBackend) LoadPreferences(context.Context) (app.Preferences, error) {
	return f.prefs, f.prefsErr
}
func (f *fakeBackend) SaveKeyHintsPreference(_ context.Context, hidden bool) error {
	f.prefs.HideKeyHints = hidden
	return nil
}
func (f *fakeBackend) SaveLanguagePreference(ctx context.Context, language string) error {
	if f.saveLanguage != nil {
		return f.saveLanguage(ctx, language)
	}
	f.prefs.Language = language
	return ctx.Err()
}
func (f *fakeBackend) SaveNetworkPreferences(ctx context.Context, cfg downloadnet.Config) error {
	if f.save != nil {
		return f.save(ctx, cfg)
	}
	return nil
}
func (f *fakeBackend) SetGuideDismissed(context.Context, bool) error { return nil }
func (f *fakeBackend) SetAutostart(ctx context.Context, enabled bool) error {
	if f.autostart != nil {
		return f.autostart(ctx, enabled)
	}
	return ctx.Err()
}
func (f *fakeBackend) SetMode(ctx context.Context, mode string) error {
	if f.mode != nil {
		return f.mode(ctx, mode)
	}
	return ctx.Err()
}
func (f *fakeBackend) ResolveRelease(ctx context.Context, selection string, cfg downloadnet.Config) (mihomo.Release, error) {
	if f.resolve != nil {
		return f.resolve(ctx, selection, cfg)
	}
	return testRelease(), nil
}
func (f *fakeBackend) TestReleaseFile(ctx context.Context, r mihomo.Release, cfg downloadnet.Config) error {
	if f.testFile != nil {
		return f.testFile(ctx, r, cfg)
	}
	return nil
}
func (f *fakeBackend) PrepareInstall(ctx context.Context, r mihomo.Release, local string, reinstall bool) (app.InstallPlan, error) {
	if f.prepare != nil {
		return f.prepare(ctx, r, local, reinstall)
	}
	return app.InstallPlan{Release: r, Local: local, ActualPath: "/tmp/lazyclash/installation/mihomo", Operation: "install", DiskKnown: true, FreeBytes: 1 << 30}, nil
}
func (f *fakeBackend) InstallCore(ctx context.Context, p app.InstallPlan, cfg downloadnet.Config, progress func(mihomo.Progress)) (app.InstallResult, error) {
	if f.install != nil {
		return f.install(ctx, p, cfg, progress)
	}
	return app.InstallResult{InstalledVersion: p.Release.Version, ActualPath: p.ActualPath, Operation: p.Operation}, nil
}
func (f *fakeBackend) Add(ctx context.Context, name, source string) error {
	if f.add != nil {
		return f.add(ctx, name, source)
	}
	return nil
}
func (f *fakeBackend) AddSubscription(ctx context.Context, name, source string) error {
	if f.addSubscription != nil {
		return f.addSubscription(ctx, name, source)
	}
	return ctx.Err()
}

func (f *fakeBackend) CreateProfile(ctx context.Context, draft app.ProfileDraft) error {
	if f.createProfile != nil {
		return f.createProfile(ctx, draft)
	}
	return ctx.Err()
}

func (f *fakeBackend) RenameProfile(ctx context.Context, id, name string) error {
	if f.renameProfile != nil {
		return f.renameProfile(ctx, id, name)
	}
	return ctx.Err()
}

func (f *fakeBackend) UpdateProfileDescription(ctx context.Context, id, description string) error {
	if f.updateDescription != nil {
		return f.updateDescription(ctx, id, description)
	}
	return ctx.Err()
}

func (f *fakeBackend) UpdateProfileAddress(ctx context.Context, id, source string) error {
	if f.updateProfileAddress != nil {
		return f.updateProfileAddress(ctx, id, source)
	}
	return ctx.Err()
}

func (f *fakeBackend) Refresh(ctx context.Context, name string) error {
	if f.refresh != nil {
		return f.refresh(ctx, name)
	}
	return ctx.Err()
}
func (f *fakeBackend) RefreshAllProfiles(ctx context.Context) error {
	if f.refreshAllProfiles != nil {
		return f.refreshAllProfiles(ctx)
	}
	return ctx.Err()
}

func (f *fakeBackend) DeleteProfile(ctx context.Context, name string) error {
	if f.deleteProfile != nil {
		return f.deleteProfile(ctx, name)
	}
	return ctx.Err()
}
func (f *fakeBackend) SetProfileOverrides(ctx context.Context, name string, raw []byte) error {
	if f.setOverrides != nil {
		return f.setOverrides(ctx, name, raw)
	}
	return ctx.Err()
}
func (f *fakeBackend) Apply(context.Context, string) error { return nil }
func (f *fakeBackend) Control(ctx context.Context, action string) error {
	if f.control != nil {
		return f.control(ctx, action)
	}
	return nil
}
func (f *fakeBackend) Select(context.Context, string, string) error { return nil }

func (f *fakeBackend) Runtime(ctx context.Context) (app.RuntimeSnapshot, error) {
	if f.runtime != nil {
		return f.runtime(ctx)
	}
	return app.RuntimeSnapshot{State: "stopped"}, ctx.Err()
}
func (f *fakeBackend) Rules(ctx context.Context) (app.RuleSnapshot, error) {
	if f.rules != nil {
		return f.rules(ctx)
	}
	return app.RuleSnapshot{State: "stopped"}, ctx.Err()
}
func (f *fakeBackend) CloseConnection(ctx context.Context, id string) error {
	if f.closeConnection != nil {
		return f.closeConnection(ctx, id)
	}
	return ctx.Err()
}

func testRelease() mihomo.Release {
	return mihomo.Release{Version: "v1.19.33", Asset: "mihomo-linux-arm64-v1.19.33.gz", URL: "https://github.com/MetaCubeX/mihomo/releases/download/v1.19.33/mihomo-linux-arm64-v1.19.33.gz", SHA256: strings.Repeat("a", 64), ChecksumSource: "official API", Platform: "linux/arm64", Size: 1024}
}
func update(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}
func key(value string) tea.KeyPressMsg {
	r := []rune(value)
	code := rune(0)
	if len(r) > 0 {
		code = r[0]
	}
	return tea.KeyPressMsg{Code: code, Text: value}
}
func special(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }
func ctrlC() tea.KeyPressMsg            { return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl} }
func ready(b *fakeBackend) Model {
	m := New(context.Background(), b, "en")
	m, _ = update(m, m.snapshotCmd(m.id, m.opCtx, true)())
	return m
}
func profileFixtures(ids ...string) []app.ProfileInfo {
	profiles := make([]app.ProfileInfo, 0, len(ids))
	for _, id := range ids {
		profiles = append(profiles, app.ProfileInfo{ID: id, Name: id, Core: "mihomo", Validated: true})
	}
	return profiles
}
func commandBatch(t *testing.T, cmd tea.Cmd) tea.BatchMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected asynchronous command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatal("expected operation batch")
	}
	return batch
}
func actionCmd(t *testing.T, cmd tea.Cmd) tea.Cmd { t.Helper(); return commandBatch(t, cmd)[0] }
func activateWizard(m Model, id string) (Model, tea.Cmd) {
	m.wizard.focus = id
	return update(m, special(tea.KeyEnter))
}

// confirmModel isolates the already-prepared installation transaction. The
// ordinary first-run UI is exercised separately and no longer visits Confirm.
func confirmModel(t *testing.T, b *fakeBackend) Model {
	t.Helper()
	m := ready(b)
	m.wizard.open, m.wizard.step, m.wizard.focus = true, stepConfirm, "install"
	plan, err := b.PrepareInstall(t.Context(), testRelease(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	m.wizard.plan, m.wizard.release = plan, plan.Release
	m.layout()
	return m
}
func findTarget(t *testing.T, m Model, id string) hitRegion {
	t.Helper()
	for _, r := range m.targets {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("target %q absent; got %+v", id, m.targets)
	return hitRegion{}
}
func click(m Model, r hitRegion) (Model, tea.Cmd) {
	return update(m, tea.MouseClickMsg{X: r.X + r.W/2, Y: r.Y + r.H/2, Button: tea.MouseLeft})
}
func expectQuit(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected quit after cleanup")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected QuitMsg")
	}
}

func TestInitialOverviewAndExplicitInstallFlow(t *testing.T) {
	m := ready(&fakeBackend{})
	if m.page != "overview" || m.wizard.open || m.wizard.networkOpen {
		t.Fatal("startup must leave the dashboard browsable")
	}
	for _, label := range []string{"Get started", "Install the Mihomo core", "Install Mihomo"} {
		if !strings.Contains(ansi.Strip(m.View().Content), label) {
			t.Fatalf("missing %s", label)
		}
	}
	m, cmd := update(m, key("i"))
	if !m.wizard.open || m.wizard.step != stepVersion || !m.busy {
		t.Fatal("install shortcut did not open the single installation page and read-only version check")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if m.busy || m.wizard.updateCheck.LatestVersion == "" || m.wizard.release.Asset != "" {
		t.Fatal("read-only version check downloaded or resolved a release asset")
	}
	m, cmd = activateWizard(m, "install")
	for _, stage := range []string{"resolve", "prepare", "install"} {
		if !m.busy || m.job != stage || m.wizard.step == stepConfirm {
			t.Fatalf("explicit install did not continue automatically through %s: step=%s job=%s", stage, m.wizard.step, m.job)
		}
		m, cmd = update(m, actionCmd(t, cmd)())
	}
	if m.wizard.step != stepResult || m.wizard.result.InstalledVersion != "v1.19.33" || m.busy {
		t.Fatal("verified result not shown")
	}
	m, _ = activateWizard(m, "done")
	if m.wizard.open || m.page != "overview" {
		t.Fatal("Done did not return to Overview")
	}
}

func TestResizePreservesWizardInputNetworkFocusAndJob(t *testing.T) {
	m := ready(&fakeBackend{})
	m, _ = update(m, key("i"))
	m.wizard.step = stepVersion
	m.wizard.specific = true
	m.wizard.focus = "version-input"
	m.wizard.version.SetValue("v1.19.33")
	m.wizard.local.SetValue("/tmp/中文/mihomo")
	m.wizard.cfg = downloadnet.Config{Source: "prefix", Prefix: "https://accelerator.example.invalid/", ProxyMode: "custom", ProxyURL: "http://127.0.0.1:7890"}
	m.wizard.draft = m.wizard.cfg
	m.wizard.remember = true
	ctx := m.newJob("resolve")
	id := m.id
	for _, size := range [][2]int{{120, 32}, {100, 28}, {80, 24}, {60, 20}, {120, 18}, {160, 45}, {60, 20}, {120, 32}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if m.id != id || ctx.Err() != nil || !m.busy || m.job != "resolve" || m.wizard.focus != "version-input" {
			t.Fatalf("resize %v changed active operation or focus", size)
		}
		if m.wizard.version.Value() != "v1.19.33" || m.wizard.local.Value() != "/tmp/中文/mihomo" || m.wizard.cfg != m.wizard.draft || !m.wizard.remember {
			t.Fatal("resize discarded choices")
		}
		assertCanvas(t, m.View().Content, size[0], size[1])
		assertRegions(t, m.targets, size[0], size[1])
	}
	m.cancel()
}

func TestResponsiveWizardModalAndFullPage(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {100, 28}, {80, 24}, {60, 20}, {120, 18}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := ready(&fakeBackend{})
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m, _ = update(m, key("i"))
			plain := ansi.Strip(m.View().Content)
			if !strings.Contains(plain, "Install Mihomo") {
				t.Fatal("wizard title missing")
			}
			modal := size[0] >= 110 && size[1] >= 30
			if strings.HasPrefix(plain, "┌") == modal {
				t.Fatalf("wrong modal/full-page layout at %v:\n%s", size, plain)
			}
			assertCanvas(t, m.View().Content, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
			for _, id := range []string{"back", "install"} {
				r := findTarget(t, m, id)
				if r.Y >= size[1]-1 {
					t.Fatalf("footer target not inside frame: %+v", r)
				}
			}
		})
	}
}

func TestInstallCancellationWaitsForCleanupAndRejectsDuplicates(t *testing.T) {
	for _, cancelWith := range []string{"keyboard", "parent"} {
		t.Run(cancelWith, func(t *testing.T) {
			started, canceled, allowCleanup := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer close(allowCleanup)
			b := &fakeBackend{install: func(ctx context.Context, _ app.InstallPlan, _ downloadnet.Config, p func(mihomo.Progress)) (app.InstallResult, error) {
				close(started)
				p(mihomo.Progress{Stage: "download", Downloaded: 10, Total: 100})
				<-ctx.Done()
				close(canceled)
				<-allowCleanup
				return app.InstallResult{}, ctx.Err()
			}}
			m := confirmModel(t, b)
			m, cmd := activateWizard(m, "install")
			run := actionCmd(t, cmd)
			result := make(chan tea.Msg, 1)
			go func() { result <- run() }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("install not started")
			}
			id := m.id
			m, cmd = activateWizard(m, "install")
			if cmd != nil || m.id != id {
				t.Fatal("duplicate install accepted")
			}
			if cancelWith == "parent" {
				m, cmd = update(m, parentCanceledMsg{})
			} else {
				m, cmd = update(m, ctrlC())
			}
			if cmd != nil || !m.busy || !m.quitting || !m.wizard.cancelling {
				t.Fatal("quit bypassed cleanup")
			}
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("context cancellation not propagated")
			}
			select {
			case <-result:
				t.Fatal("result returned before cleanup was allowed")
			default:
			}
			// Permit cleanup without closing the channel twice in the deferred guard.
			allowCleanup <- struct{}{}
			var done tea.Msg
			select {
			case done = <-result:
			case <-time.After(time.Second):
				t.Fatal("cleanup did not finish")
			}
			m, cmd = update(m, done)
			if m.busy {
				t.Fatal("operation remained busy")
			}
			expectQuit(t, cmd)
		})
	}
}

func TestEscapeCancelsInstallWithoutClosingResult(t *testing.T) {
	m := confirmModel(t, &fakeBackend{})
	m, _ = activateWizard(m, "install")
	m, cmd := update(m, special(tea.KeyEsc))
	if cmd != nil || !m.busy || m.quitting || !m.wizard.open || !m.wizard.cancelling {
		t.Fatal("Esc should request cancel while retaining progress")
	}
	m, cmd = update(m, wizardMsg{id: m.id, kind: "install", err: context.Canceled})
	if cmd != nil || m.busy || !m.wizard.open || m.wizard.step != stepResult || !strings.Contains(m.wizard.err, "canceled") {
		t.Fatal("canceled install result missing")
	}
}

func TestLateMessagesCannotReplaceCurrentOperation(t *testing.T) {
	m := ready(&fakeBackend{})
	m, _ = update(m, key("i"))
	m.wizard.step = stepVersion
	m.newJob("resolve")
	old := m.id
	m.abortLookup()
	m.openNetwork(false)
	m.wizard.versionTest = checkState{Status: "Not tested"}
	id := m.id
	stale := []tea.Msg{resultMsg{id: old, snapshot: app.Snapshot{Installed: true}, hasSnapshot: true}, wizardMsg{id: old, kind: "prepare", plan: app.InstallPlan{Release: testRelease()}}, networkMsg{id: old, kind: "version", release: testRelease()}, nextProgressMsg{progressMsg: progressMsg{id: old, progress: mihomo.Progress{Stage: "done"}}}}
	for _, msg := range stale {
		m, _ = update(m, msg)
		if m.id != id || m.snapshot.Installed || m.wizard.step != stepVersion || m.wizard.versionTest.Status != "Not tested" || m.wizard.progress.Stage != "" {
			t.Fatalf("stale %T changed current state", msg)
		}
	}
}

func TestNetworkDraftBackAndExplicitRemember(t *testing.T) {
	for _, remember := range []bool{false, true} {
		t.Run(fmt.Sprint(remember), func(t *testing.T) {
			saves := 0
			b := &fakeBackend{save: func(context.Context, downloadnet.Config) error { saves++; return nil }}
			m := ready(b)
			original := m.wizard.cfg
			m, _ = update(m, key("n"))
			m.wizard.draft = downloadnet.Config{Source: "prefix", Prefix: "https://accelerator.example.invalid/", ProxyMode: "none"}
			m, _ = activateWizard(m, "back")
			if m.wizard.cfg != original || saves != 0 || m.wizard.networkOpen {
				t.Fatal("Back applied network draft")
			}
			m, _ = update(m, key("n"))
			if m.wizard.remember {
				t.Fatal("remember must require an explicit choice")
			}
			m.wizard.draft = downloadnet.Config{Source: "prefix", Prefix: "https://accelerator.example.invalid/", ProxyMode: "none"}
			if remember {
				m, _ = activateWizard(m, "remember")
			}
			if saves != 0 {
				t.Fatal("remember toggle wrote preferences before Apply")
			}
			var cmd tea.Cmd
			m, cmd = activateWizard(m, "apply-network")
			if remember {
				if !m.busy || saves != 0 {
					t.Fatal("save should be asynchronous")
				}
				m, _ = update(m, actionCmd(t, cmd)())
			} else if m.busy {
				t.Fatal("session-only apply unexpectedly started a job")
			}
			if m.wizard.networkOpen || m.wizard.cfg.Source != "prefix" || saves != map[bool]int{false: 0, true: 1}[remember] {
				t.Fatal("explicit Apply semantics incorrect")
			}
		})
	}
}

func TestNetworkEditInvalidatesChecksAndLateRequests(t *testing.T) {
	m := ready(&fakeBackend{})
	m, _ = update(m, key("n"))
	m.wizard.draft.Source = "prefix"
	m.wizard.draft.Prefix = "https://example.invalid/"
	m.wizard.prefix.SetValue(m.wizard.draft.Prefix)
	m.wizard.focus = "prefix"
	ctx := m.newJob("network")
	id := m.id
	m.wizard.versionTest = checkState{Status: "Passed"}
	m.wizard.fileTest = checkState{Status: "Passed"}
	m.wizard.testPending = 2
	m, _ = update(m, key("x"))
	if ctx.Err() == nil || m.busy || m.id == id || m.wizard.versionTest.Status != "Not tested" || m.wizard.fileTest.Status != "Not tested" {
		t.Fatal("changed draft retained test results or in-flight requests")
	}
	m, _ = update(m, networkMsg{id: id, kind: "file", err: nil})
	if m.wizard.fileTest.Status != "Not tested" {
		t.Fatal("old test result repopulated changed draft")
	}
}

func TestMetadataFailureKeepsNetworkReachableAndApplyRetries(t *testing.T) {
	calls := 0
	var used downloadnet.Config
	b := &fakeBackend{resolve: func(_ context.Context, _ string, cfg downloadnet.Config) (mihomo.Release, error) {
		calls++
		used = cfg
		if calls == 1 {
			return mihomo.Release{}, errors.New("metadata unavailable")
		}
		return testRelease(), nil
	}}
	m := ready(b)
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m, initial := update(m, key("i"))
	m, _ = update(m, actionCmd(t, initial)())
	m.wizard.specific = true
	m.wizard.version.SetValue("v1.19.33")
	m, cmd := activateWizard(m, "resolve")
	m, _ = update(m, actionCmd(t, cmd)())
	if m.wizard.failedKind != "resolve" || m.wizard.step != stepVersion || m.busy {
		t.Fatal("metadata failure lost recoverable Version state")
	}
	m, cmd = click(m, findTarget(t, m, "network"))
	if cmd != nil || !m.wizard.networkOpen {
		t.Fatal("network settings inaccessible after metadata failure")
	}
	m.wizard.draft = downloadnet.Config{Source: "official", ProxyMode: "environment"}
	m, cmd = activateWizard(m, "apply-network")
	if !m.busy || m.wizard.networkOpen || m.job != "resolve" {
		t.Fatal("Apply & retry did not start metadata retry")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if calls != 2 || used.ProxyMode != "environment" || m.wizard.release.Version == "" || m.wizard.err != "" {
		t.Fatal("retry did not use selected connection")
	}
}

func TestVersionAndFileTestsRemainIndependent(t *testing.T) {
	for _, versionFails := range []bool{false, true} {
		t.Run(fmt.Sprint(versionFails), func(t *testing.T) {
			b := &fakeBackend{resolve: func(context.Context, string, downloadnet.Config) (mihomo.Release, error) {
				if versionFails {
					return mihomo.Release{}, errors.New("API blocked")
				}
				return testRelease(), nil
			}, testFile: func(context.Context, mihomo.Release, downloadnet.Config) error {
				if versionFails {
					return nil
				}
				return errors.New("mirror unavailable")
			}}
			m := ready(b)
			m.wizard.release = testRelease()
			m, _ = update(m, key("n"))
			m, cmd := activateWizard(m, "test")
			commands := commandBatch(t, cmd)
			if len(commands) != 3 || m.wizard.testPending != 2 {
				t.Fatal("version/file tests were not launched independently")
			}
			m, _ = update(m, commands[0]())
			expected := "Passed"
			if versionFails {
				expected = "Failed"
			}
			if m.wizard.versionTest.Status != expected || m.wizard.fileTest.Status != "Testing" || !m.busy {
				t.Fatal("version result incorrectly completed file test")
			}
			m, _ = update(m, commands[2]())
			fileExpected := "Failed"
			if versionFails {
				fileExpected = "Passed"
			}
			if m.wizard.versionTest.Status != expected || m.wizard.fileTest.Status != fileExpected || m.busy {
				t.Fatal("file result replaced version status")
			}
		})
	}
	// Without an asset URL, a metadata failure must say Not applicable rather than
	// claim the independent file request succeeded.
	m := ready(&fakeBackend{resolve: func(context.Context, string, downloadnet.Config) (mihomo.Release, error) {
		return mihomo.Release{}, errors.New("API blocked")
	}})
	m, _ = update(m, key("n"))
	m, cmd := activateWizard(m, "test")
	m, _ = update(m, actionCmd(t, cmd)())
	if m.wizard.fileTest.Status != "Not applicable" || m.busy {
		t.Fatal("missing file URL was reported as tested")
	}
}

func TestNetworkCancellationQuitsOnlyAfterBothResults(t *testing.T) {
	for _, parent := range []bool{false, true} {
		for _, order := range [][]string{{"version", "file"}, {"file", "version"}} {
			t.Run(fmt.Sprintf("parent=%t/%s-first", parent, order[0]), func(t *testing.T) {
				m := ready(&fakeBackend{})
				m.wizard.release = testRelease()
				m, _ = update(m, key("n"))
				m, _ = activateWizard(m, "test")
				id := m.id
				var cmd tea.Cmd
				if parent {
					m, cmd = update(m, parentCanceledMsg{})
				} else {
					m, cmd = update(m, ctrlC())
				}
				if cmd != nil || !m.quitting || !m.busy {
					t.Fatal("network quit did not await both requests")
				}
				m, cmd = update(m, networkMsg{id: id, kind: order[0], err: context.Canceled})
				if cmd != nil || !m.busy {
					t.Fatal("quit before second request completed")
				}
				m, cmd = update(m, networkMsg{id: id, kind: order[1], err: context.Canceled})
				if m.busy {
					t.Fatal("network remained busy after both results")
				}
				expectQuit(t, cmd)
			})
		}
	}
}

func TestFailedInstallSurvivesNetworkBackAndDraftEdits(t *testing.T) {
	for _, edit := range []bool{false, true} {
		t.Run(fmt.Sprint(edit), func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.openWizard(false)
			m.wizard.step = stepResult
			m.wizard.err = "checksum mismatch"
			m.wizard.failedKind = "install"
			m.wizard.failedStage = "Verifying checksum"
			m.layout()
			m, _ = activateWizard(m, "network")
			if edit {
				m.wizard.focus = "source"
				m, _ = update(m, special(tea.KeyRight))
			}
			m, _ = activateWizard(m, "back")
			if m.wizard.err == "" || m.wizard.failedKind != "install" || strings.Contains(ansi.Strip(m.View().Content), "Mihomo installed and verified") {
				t.Fatal("network Back converted failed installation into success")
			}
		})
	}
}

func TestTextInputOwnsQAndDigitsAndSourceEditingIsVisible(t *testing.T) {
	m := ready(&fakeBackend{})
	m, _ = update(m, key("i"))
	m.wizard.step = stepVersion
	m.wizard.specific = true
	m.wizard.focus = "version-input"
	m.layout()
	for _, letter := range "q1234" {
		m, _ = update(m, key(string(letter)))
	}
	if m.wizard.version.Value() != "q1234" || m.page != "overview" || !m.wizard.open || m.quitting {
		t.Fatal("global shortcuts intercepted wizard text")
	}
	m.wizard.open = false
	m.layout()
	m, _ = update(m, key("a"))
	for _, letter := range "q1234" {
		m, _ = update(m, key(string(letter)))
	}
	if m.input.Value() != "q1234" || m.page != "overview" || m.quitting {
		t.Fatal("global shortcuts intercepted import text")
	}
	m.input.SetValue("https://secret.example/private-token?q=super-secret")
	m.layout()
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "https://secret.example/private-token?q=super-secret") {
		t.Fatal("subscription source is not visible for editing")
	}
	m, _ = update(m, special(tea.KeyEsc))
	if m.form != "" || m.input.Value() != "" {
		t.Fatal("cancel did not clear source input")
	}
	m, _ = update(m, key("n"))
	m.wizard.draft.ProxyMode = "custom"
	m.wizard.proxy.SetValue("http://user:password-secret@127.0.0.1:7890")
	m.wizard.draft.ProxyURL = m.wizard.proxy.Value()
	m.wizard.focus = "proxy-url"
	m.layout()
	if strings.Contains(ansi.Strip(m.View().Content), "password-secret") {
		t.Fatal("proxy input leaked credentials")
	}
}

func TestModalMouseDoesNotReachDashboardAndActualButtonsWork(t *testing.T) {
	m := ready(&fakeBackend{})
	m, _ = update(m, tea.WindowSizeMsg{Width: 160, Height: 45})
	behind := findTarget(t, m, "nav.settings")
	m, initial := update(m, key("i"))
	m, _ = update(m, actionCmd(t, initial)())
	for _, r := range m.targets {
		if strings.HasPrefix(r.ID, "nav.") {
			t.Fatal("modal kept dashboard targets")
		}
	}
	m, _ = click(m, behind)
	if m.page != "overview" || !m.wizard.open {
		t.Fatal("mouse click penetrated modal")
	}
	target := findTarget(t, m, "install")
	line := strings.Split(m.View().Content, "\n")[target.Y]
	if !strings.Contains(ansi.Strip(ansi.Cut(line, target.X, target.X+target.W)), "Download & install") {
		t.Fatal("button target misses painted label")
	}
	m, cmd := click(m, target)
	if !m.busy || m.job != "resolve" || m.wizard.step != stepVersion {
		t.Fatal("actual install click did not trigger pinned asset lookup")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	m.abortLookup()
	m.wizard.step = stepConfirm
	m.wizard.plan.Warnings = make([]string, 30)
	for i := range m.wizard.plan.Warnings {
		m.wizard.plan.Warnings[i] = "long diagnostic line 中文"
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 60, Height: 20})
	m.wizard.scroll = 0
	m.wizard.reveal = false
	m.layout()
	m, _ = update(m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if m.wizard.scroll == 0 {
		t.Fatal("wheel did not scroll wizard")
	}
	m, _ = update(m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if m.wizard.scroll != 0 {
		t.Fatal("wheel up did not return to previous position")
	}
}

func TestWizardLongContentAndProgressRemainWithinTerminal(t *testing.T) {
	long := strings.Repeat("中文路径/", 40) + strings.Repeat("x", 200)
	for _, size := range [][2]int{{160, 45}, {120, 32}, {100, 28}, {80, 24}, {60, 20}, {120, 18}, {20, 7}, {3, 2}} {
		for _, step := range []wizardStep{stepCore, stepVersion, stepConfirm, stepInstalling, stepResult} {
			t.Run(fmt.Sprintf("%v/%s", size, step), func(t *testing.T) {
				m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true, Version: long, Service: service.Status{Active: true}}})
				m.openWizard(false)
				m.wizard.step = step
				m.wizard.release = testRelease()
				m.wizard.plan = app.InstallPlan{Release: testRelease(), ActualPath: long, Warnings: []string{long}}
				m.wizard.result = app.InstallResult{InstalledVersion: long, ActualPath: long}
				m.wizard.progress = mihomo.Progress{Stage: "download", Downloaded: 256, Total: 0, Detail: long}
				m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				assertCanvas(t, m.View().Content, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
			})
		}
	}
	m := ready(&fakeBackend{})
	m.openWizard(false)
	m.wizard.step = stepInstalling
	m.wizard.progress = mihomo.Progress{Stage: "download", Downloaded: 512, Total: 0}
	m.layout()
	if !strings.Contains(ansi.Strip(m.View().Content), "indeterminate") {
		t.Fatal("unknown total not clearly indicated")
	}
	m.wizard.progress.Total = 1024
	m.layout()
	if !strings.Contains(ansi.Strip(m.View().Content), "50.0%") {
		t.Fatal("known progress percent missing")
	}
}

func TestFailedRefreshPreservesLastSnapshotAndProfilesDeduplicate(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Version: "known", Profiles: []string{"default", "work", "work"}}, snapshotErr: errors.New("lock timeout")}
	m := New(context.Background(), b, "en")
	m, _ = update(m, resultMsg{id: m.id, snapshot: b.snapshot, hasSnapshot: true})
	m, cmd := update(m, key("r"))
	m, _ = update(m, actionCmd(t, cmd)())
	if !m.snapshot.Installed || m.snapshot.Version != "known" || !strings.Contains(m.failure, "refresh current state") {
		t.Fatal("failed refresh erased valid dashboard state")
	}
	if got := strings.Join(m.profiles(), ","); got != "default,work" {
		t.Fatal(got)
	}
}

func TestRememberFailureKeepsDraftAndOriginalInstallFailure(t *testing.T) {
	b := &fakeBackend{save: func(context.Context, downloadnet.Config) error {
		return errors.New("credentials must remain session-only")
	}}
	m := ready(b)
	m.openWizard(false)
	m.wizard.step = stepResult
	m.wizard.err = "checksum mismatch"
	m.wizard.failedKind = "install"
	m.layout()
	m, _ = activateWizard(m, "network")
	original := m.wizard.cfg
	m.wizard.draft = downloadnet.Config{Source: "official", ProxyMode: "custom", ProxyURL: "http://user:pass@127.0.0.1:7890"}
	m, _ = activateWizard(m, "remember")
	m, cmd := activateWizard(m, "apply-network")
	m, _ = update(m, actionCmd(t, cmd)())
	if !m.wizard.networkOpen || m.wizard.cfg != original || !strings.Contains(m.wizard.err, "session-only") {
		t.Fatal("failed preference save applied settings or lost editable draft")
	}
	m, _ = activateWizard(m, "back")
	if m.wizard.err != "checksum mismatch" || m.wizard.failedKind != "install" {
		t.Fatal("preference failure overwrote original installation failure")
	}
}

func TestNetworkVersionSuccessStartsFileTestForResolvedAsset(t *testing.T) {
	files := 0
	b := &fakeBackend{testFile: func(_ context.Context, r mihomo.Release, _ downloadnet.Config) error {
		files++
		if r.URL != testRelease().URL {
			t.Fatal("file test did not use the resolved release")
		}
		return nil
	}}
	m := ready(b)
	m, _ = update(m, key("n"))
	m, cmd := activateWizard(m, "test")
	if m.wizard.fileTest.Status != "Not applicable" {
		t.Fatal("file request started without an asset")
	}
	m, cmd = update(m, actionCmd(t, cmd)())
	if !m.busy || m.wizard.versionTest.Status != "Passed" || m.wizard.fileTest.Status != "Testing" || cmd == nil {
		t.Fatal("metadata success did not schedule asset test")
	}
	m, _ = update(m, cmd())
	if m.busy || m.wizard.fileTest.Status != "Passed" || files != 1 {
		t.Fatal("scheduled file test did not complete independently")
	}
}

func (f *fakeBackend) CheckUpdate(ctx context.Context, channel mihomo.Channel, cfg downloadnet.Config) (app.CoreUpdateCheck, error) {
	if f.checkUpdate != nil {
		return f.checkUpdate(ctx, channel, cfg)
	}
	check := app.CoreUpdateCheck{LatestVersion: "v1.19.33", Channel: mihomo.ChannelStable, Status: mihomo.UpdateNotInstalled, CheckedAt: time.Now()}
	if f.snapshot.Installed {
		check.InstalledVersion, check.Status = f.snapshot.Version, mihomo.UpdateAvailable
	}
	return check, ctx.Err()
}
func (f *fakeBackend) UpgradeCore(ctx context.Context, check app.CoreUpdateCheck, cfg downloadnet.Config, progress func(mihomo.Progress)) (app.CoreUpgradeResult, error) {
	if f.upgradeCore != nil {
		return f.upgradeCore(ctx, check, cfg, progress)
	}
	return app.CoreUpgradeResult{InstalledVersion: check.LatestVersion, PreviousVersion: check.InstalledVersion}, ctx.Err()
}
