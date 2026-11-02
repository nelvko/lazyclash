package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func updateFixture(t *testing.T, installed string) (*App, *fakeService) {
	t.Helper()
	a, svc := coreFixture(t)
	a.Version = func(ctx context.Context, path string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if version, ok := strings.CutPrefix(string(raw), "version:"); ok {
			return version, nil
		}
		return installed, nil
	}
	a.QueryLatest = func(ctx context.Context, channel mihomo.Channel, cfg downloadnet.Config) (string, error) {
		return "v1.20.0", ctx.Err()
	}
	if err := a.Install(t.Context(), "", nil); err != nil {
		t.Fatal(err)
	}
	// Make API version observations track the fixture executable that the fake
	// systemd service starts, without contacting a real controller.
	a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		bin, _, _ := a.paths()
		version, err := a.Version(r.Context(), bin)
		if err != nil {
			return nil, err
		}
		body := fmt.Sprintf(`{"version":%q,"meta":true}`, version)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})}
	return a, svc
}

func checkedUpgrade(t *testing.T, a *App) CoreUpdateCheck {
	t.Helper()
	check, err := a.CheckUpdate(t.Context(), "", downloadnet.Config{})
	if err != nil || check.Status != mihomo.UpdateAvailable || check.LatestVersion != "v1.20.0" {
		t.Fatalf("check=%+v error=%v", check, err)
	}
	return check
}

func updateTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := info.Mode().String()
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			value += ":" + target
		case entry.Type().IsRegular():
			hash, err := fileDigest(path)
			if err != nil {
				return err
			}
			value += ":" + hash + ":" + info.ModTime().String()
		}
		tree[rel] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestCheckUpdateComparesInstalledExecutableWithLatestVersion(t *testing.T) {
	for _, tc := range []struct {
		name, installed, latest string
		channel                 mihomo.Channel
		status                  mihomo.UpdateStatus
	}{
		{"same", "v1.20.0", "v1.20.0", mihomo.ChannelStable, mihomo.UpdateCurrent},
		{"remote-newer", "v1.19.33", "v1.20.0", mihomo.ChannelStable, mihomo.UpdateAvailable},
		{"local-newer", "v1.21.0", "v1.20.0", mihomo.ChannelStable, mihomo.UpdateAhead},
		{"prefix-normalized", "1.20.0", "v1.20.0", mihomo.ChannelStable, mihomo.UpdateCurrent},
		{"alpha-identical", "alpha-69c1631", "alpha-69c1631", mihomo.ChannelAlpha, mihomo.UpdateCurrent},
		{"alpha-different", "alpha-fffffff", "alpha-0000000", mihomo.ChannelAlpha, mihomo.UpdateIncomparable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, svc := updateFixture(t, tc.installed)
			calls := 0
			a.QueryLatest = func(ctx context.Context, channel mihomo.Channel, cfg downloadnet.Config) (string, error) {
				calls++
				if channel != tc.channel {
					t.Fatalf("queried channel=%q want=%q", channel, tc.channel)
				}
				return tc.latest, ctx.Err()
			}
			a.DownloadRelease = func(context.Context, string, mihomo.Release, downloadnet.Config, func(mihomo.Progress)) error {
				t.Fatal("checking downloaded an executable")
				return nil
			}
			before := updateTree(t, a.Home)
			check, err := a.CheckUpdate(t.Context(), "", downloadnet.Config{})
			if err != nil || check.Status != tc.status || check.Channel != tc.channel || check.InstalledVersion != tc.installed || check.LatestVersion != tc.latest || check.CheckedAt.IsZero() || calls != 1 {
				t.Fatalf("check=%+v error=%v calls=%d", check, err, calls)
			}
			if !reflect.DeepEqual(before, updateTree(t, a.Home)) || len(svc.actions) != 0 || svc.installs != 1 {
				t.Fatal("checking changed files or service state")
			}
		})
	}
}

func TestCheckUpdateAbsentHomeDoesNotCreateLockOrInstallation(t *testing.T) {
	a, svc := coreFixture(t)
	a.Home = filepath.Join(t.TempDir(), "not-created")
	a.QueryLatest = func(ctx context.Context, channel mihomo.Channel, cfg downloadnet.Config) (string, error) {
		if channel != mihomo.ChannelStable {
			t.Fatalf("default channel=%q", channel)
		}
		return "v1.20.0", ctx.Err()
	}
	check, err := a.CheckUpdate(t.Context(), "", downloadnet.Config{})
	if err != nil || check.Status != mihomo.UpdateNotInstalled || check.LatestVersion != "v1.20.0" || check.InstalledVersion != "" {
		t.Fatalf("check=%+v error=%v", check, err)
	}
	assertMissing(t, a.Home)
	if svc.installs != 0 || len(svc.actions) != 0 {
		t.Fatal("checking installed or started a service")
	}
}

func TestCheckUpdateUnreadableVersionCannotReportCurrent(t *testing.T) {
	a, svc := updateFixture(t, "v1.19.33")
	a.Version = func(context.Context, string) (string, error) { return "", errors.New("version probe failed") }
	before := updateTree(t, a.Home)
	check, err := a.CheckUpdate(t.Context(), mihomo.ChannelStable, downloadnet.Config{})
	if err != nil || check.Status != mihomo.UpdateUnavailable || check.InstalledError == "" || check.LatestVersion != "v1.20.0" {
		t.Fatalf("check=%+v error=%v", check, err)
	}
	if !reflect.DeepEqual(before, updateTree(t, a.Home)) || len(svc.actions) != 0 {
		t.Fatal("failed version probe modified installation")
	}
}

func TestCheckUpdateUnknownChannelDoesNotAssumeStable(t *testing.T) {
	a, _ := updateFixture(t, "custom-development-build")
	a.QueryLatest = func(context.Context, mihomo.Channel, downloadnet.Config) (string, error) {
		t.Fatal("unknown local channel triggered a stable query")
		return "", nil
	}
	check, err := a.CheckUpdate(t.Context(), "", downloadnet.Config{})
	if err != nil || check.Status != mihomo.UpdateIncomparable || check.Channel != mihomo.ChannelUnknown {
		t.Fatalf("check=%+v error=%v", check, err)
	}
}

func TestCheckUpdateNetworkFailureAndPendingRecoveryRemainReadOnly(t *testing.T) {
	a, svc := updateFixture(t, "v1.19.33")
	journal := filepath.Join(a.Home, "core-pending.json")
	write(t, journal, []byte("interrupted transaction requiring manual recovery"))
	networkErr := errors.New("official version query failed")
	a.QueryLatest = func(context.Context, mihomo.Channel, downloadnet.Config) (string, error) { return "", networkErr }
	before := updateTree(t, a.Home)
	check, err := a.CheckUpdate(t.Context(), "", downloadnet.Config{})
	if err == nil || check.Status == mihomo.UpdateCurrent || check.Status == mihomo.UpdateAvailable {
		t.Fatalf("network or pending state reported success: check=%+v error=%v", check, err)
	}
	if !reflect.DeepEqual(before, updateTree(t, a.Home)) || len(svc.actions) != 0 {
		t.Fatal("checking attempted transaction recovery")
	}
}

func TestUpgradeCorePinsReviewedVersionAndPreservesStoppedState(t *testing.T) {
	a, svc := updateFixture(t, "v1.19.33")
	check := checkedUpgrade(t, a)
	a.QueryLatest = func(context.Context, mihomo.Channel, downloadnet.Config) (string, error) {
		t.Fatal("upgrade queried mutable latest instead of the reviewed target")
		return "", nil
	}
	a.Resolve = func(ctx context.Context, selection string, cfg downloadnet.Config) (mihomo.Release, error) {
		if selection != check.LatestVersion {
			t.Fatalf("resolved selection=%q want=%q", selection, check.LatestVersion)
		}
		return mihomo.Release{Version: selection, Asset: "fixture.gz", Size: 128}, ctx.Err()
	}
	bin, config, _ := a.paths()
	beforeConfig := read(t, config)
	result, err := a.UpgradeCore(t.Context(), check, downloadnet.Config{}, nil)
	if err != nil || result.InstalledVersion != "v1.20.0" || result.PreviousVersion != "v1.19.33" || result.Restarted {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if !bytes.Equal(beforeConfig, read(t, config)) || string(read(t, bin)) != "version:v1.20.0" || svc.active || len(svc.actions) != 0 {
		t.Fatal("stopped upgrade changed configuration or started the service")
	}
	assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
}

func TestUpgradeCoreRunningServiceRestartsAfterAtomicPublication(t *testing.T) {
	a, svc := updateFixture(t, "v1.19.33")
	svc.active = true
	check := checkedUpgrade(t, a)
	bin, _, _ := a.paths()
	oldProcess, err := os.Open(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer oldProcess.Close()
	download := a.DownloadRelease
	a.DownloadRelease = func(ctx context.Context, dest string, release mihomo.Release, cfg downloadnet.Config, progress func(mihomo.Progress)) error {
		if !svc.active || len(svc.actions) != 0 || string(read(t, bin)) != "fixture engine" {
			t.Fatal("service stopped or binary changed before download completed")
		}
		return download(ctx, dest, release, cfg, progress)
	}
	svc.actionHook = func(ctx context.Context, action string) error {
		if action == "restart" && string(read(t, bin)) != "version:v1.20.0" {
			t.Fatal("service restarted before publishing the verified executable")
		}
		return ctx.Err()
	}
	result, err := a.UpgradeCore(t.Context(), check, downloadnet.Config{}, nil)
	if err != nil || !result.Restarted || !svc.active {
		t.Fatalf("result=%+v error=%v active=%v", result, err, svc.active)
	}
	oldBytes, err := io.ReadAll(oldProcess)
	if err != nil || string(oldBytes) != "fixture engine" {
		t.Fatalf("running inode was overwritten: %q %v", oldBytes, err)
	}
	restarts := 0
	for _, action := range svc.actions {
		if action == "restart" {
			restarts++
		}
	}
	if restarts != 1 {
		t.Fatalf("service actions=%v", svc.actions)
	}
	assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
}

func TestUpgradeCoreFailuresBeforePublicationPreserveFilesAndProcess(t *testing.T) {
	for _, failure := range []string{"download", "wrong-version", "configuration", "cancel-download", "release-version"} {
		t.Run(failure, func(t *testing.T) {
			a, svc := updateFixture(t, "v1.19.33")
			svc.active = true
			check := checkedUpgrade(t, a)
			bin, config, _ := a.paths()
			beforeBin, beforeConfig := read(t, bin), read(t, config)
			beforeManifest, beforeGeneration, err := a.load()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch failure {
			case "download", "cancel-download":
				a.DownloadRelease = func(ctx context.Context, dest string, release mihomo.Release, cfg downloadnet.Config, progress func(mihomo.Progress)) error {
					if err := os.WriteFile(dest, []byte("partial"), 0700); err != nil {
						return err
					}
					if failure == "cancel-download" {
						cancel()
						return ctx.Err()
					}
					return io.ErrUnexpectedEOF
				}
			case "wrong-version":
				a.DownloadRelease = func(ctx context.Context, dest string, release mihomo.Release, cfg downloadnet.Config, progress func(mihomo.Progress)) error {
					return os.WriteFile(dest, []byte("version:v1.0.0"), 0700)
				}
			case "configuration":
				a.Validate = func(context.Context, string, string, string) error {
					return errors.New("new core rejected existing configuration")
				}
			case "release-version":
				a.Resolve = func(context.Context, string, downloadnet.Config) (mihomo.Release, error) {
					return mihomo.Release{Version: "v1.21.0", Size: 128}, nil
				}
			}
			if _, err := a.UpgradeCore(ctx, check, downloadnet.Config{}, nil); err == nil {
				t.Fatal("failed upgrade succeeded")
			}
			afterManifest, afterGeneration, err := a.load()
			if err != nil || !reflect.DeepEqual(beforeManifest, afterManifest) || beforeGeneration != afterGeneration || !bytes.Equal(beforeBin, read(t, bin)) || !bytes.Equal(beforeConfig, read(t, config)) || !svc.active || len(svc.actions) != 0 {
				t.Fatalf("failure changed installation or process: manifest=%+v error=%v actions=%v", afterManifest, err, svc.actions)
			}
			assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
			staging, _ := filepath.Glob(filepath.Join(a.Home, ".core-stage-*"))
			if len(staging) != 0 {
				t.Fatalf("staging leaked: %v", staging)
			}
		})
	}
}

func TestUpgradeCoreRestartFailureRestoresBinaryGenerationAndRunningState(t *testing.T) {
	a, svc := updateFixture(t, "v1.19.33")
	svc.active = true
	check := checkedUpgrade(t, a)
	bin, config, _ := a.paths()
	beforeBin, beforeConfig := read(t, bin), read(t, config)
	_, oldGeneration, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	newStartFailed := errors.New("new service restart failed")
	starts := 0
	svc.actionHook = func(ctx context.Context, action string) error {
		if action != "restart" && action != "start" {
			return ctx.Err()
		}
		starts++
		if string(read(t, bin)) == "version:v1.20.0" {
			svc.active = false
			return newStartFailed
		}
		if !bytes.Equal(beforeBin, read(t, bin)) {
			t.Fatal("recovery started an unexpected binary")
		}
		return ctx.Err()
	}
	_, err = a.UpgradeCore(t.Context(), check, downloadnet.Config{}, nil)
	if err == nil || !strings.Contains(err.Error(), newStartFailed.Error()) || starts < 2 {
		t.Fatalf("error=%v starts=%d actions=%v", err, starts, svc.actions)
	}
	manifest, generation, err := a.load()
	if err != nil || manifest.Version != "v1.19.33" || generation != oldGeneration || !bytes.Equal(beforeBin, read(t, bin)) || !bytes.Equal(beforeConfig, read(t, config)) || !svc.active {
		t.Fatalf("rollback failed: manifest=%+v generation=%q error=%v active=%v", manifest, generation, err, svc.active)
	}
	assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
}

func TestUpgradeCoreCancellationAfterPublicationStillRestoresRunningState(t *testing.T) {
	a, svc := updateFixture(t, "v1.19.33")
	svc.active = true
	check := checkedUpgrade(t, a)
	bin, _, _ := a.paths()
	beforeBin := read(t, bin)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	svc.actionHook = func(actionCtx context.Context, action string) error {
		if action == "restart" && string(read(t, bin)) == "version:v1.20.0" {
			svc.active = false
			cancel()
			return actionCtx.Err()
		}
		return actionCtx.Err()
	}
	if _, err := a.UpgradeCore(ctx, check, downloadnet.Config{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation cause lost: %v", err)
	}
	if !svc.active || !bytes.Equal(beforeBin, read(t, bin)) {
		t.Fatalf("cancelled context prevented rollback: active=%v actions=%v", svc.active, svc.actions)
	}
	assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
}

type upgradeReadinessService struct {
	*fakeService
	bin string
}

func (s *upgradeReadinessService) Inspect(ctx context.Context) (service.Status, error) {
	raw, err := os.ReadFile(s.bin)
	if err != nil {
		return service.Status{}, err
	}
	if string(raw) == "version:v1.20.0" {
		return service.Status{State: "failed", Result: "exit-code", ExitCode: 1}, ctx.Err()
	}
	return s.fakeService.Inspect(ctx)
}

func TestUpgradeCoreFailedReadinessRollsBackEvenWhenRestartJobSucceeded(t *testing.T) {
	a, svc := updateFixture(t, "v1.19.33")
	svc.active = true
	bin, _, _ := a.paths()
	a.Service = &upgradeReadinessService{fakeService: svc, bin: bin}
	check := checkedUpgrade(t, a)
	beforeBin := read(t, bin)
	_, err := a.UpgradeCore(t.Context(), check, downloadnet.Config{}, nil)
	if err == nil || !strings.Contains(err.Error(), "service failed") {
		t.Fatalf("failed service readiness was accepted: %v", err)
	}
	if !svc.active || !bytes.Equal(beforeBin, read(t, bin)) {
		t.Fatal("failed readiness did not restore old running service")
	}
	assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
}

func TestUpgradeCoreWrongRunningVersionRollsBack(t *testing.T) {
	a, svc := updateFixture(t, "v1.19.33")
	svc.active = true
	check := checkedUpgrade(t, a)
	bin, _, _ := a.paths()
	beforeBin := read(t, bin)
	a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"version":"v1.19.33","meta":true}`)), Header: make(http.Header), Request: r}, nil
	})}
	if _, err := a.UpgradeCore(t.Context(), check, downloadnet.Config{}, nil); err == nil {
		t.Fatal("wrong running version was accepted as a successful upgrade")
	}
	if !svc.active || !bytes.Equal(beforeBin, read(t, bin)) {
		t.Fatal("wrong running version did not trigger rollback")
	}
	assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
}

func TestInterruptedRestartingUpgradeRecoversEachPublicationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		binary, pointer, committed bool
	}{
		{"before-binary", false, false, false},
		{"after-binary", true, false, false},
		{"after-generation", true, true, false},
		{"committed", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, svc := updateFixture(t, "v1.19.33")
			bin, _, _ := a.paths()
			beforeBin := read(t, bin)
			j := stageCoreRecovery(t, a, tc.binary, tc.pointer, tc.committed)
			j.Restart = true
			raw, err := json.Marshal(j)
			if err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(a.Home, "core-pending.json"), raw)
			svc.active = tc.committed
			if err := a.locked(t.Context(), func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			m, generation, err := a.load()
			if err != nil {
				t.Fatal(err)
			}
			if tc.committed {
				if generation != j.New || m.Version != "v1.20.0" || len(svc.actions) != 0 {
					t.Fatalf("committed upgrade was rolled back: manifest=%+v generation=%q actions=%v", m, generation, svc.actions)
				}
			} else if generation != j.Old || m.Version != "v1.19.33" || !bytes.Equal(beforeBin, read(t, bin)) || !svc.active || len(svc.actions) == 0 {
				t.Fatalf("interrupted upgrade failed to restore old running service: manifest=%+v generation=%q actions=%v", m, generation, svc.actions)
			}
			assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
		})
	}
}

func TestExplicitStopSuppressesRestartDuringInterruptedUpgradeRecovery(t *testing.T) {
	a, svc := updateFixture(t, "v1.19.33")
	bin, _, _ := a.paths()
	beforeBin := read(t, bin)
	j := stageCoreRecovery(t, a, true, true, false)
	j.Restart = true
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(a.Home, "core-pending.json"), raw)
	svc.active = true
	if err := a.Control(t.Context(), "stop"); err != nil {
		t.Fatal(err)
	}
	if svc.active || !reflect.DeepEqual(svc.actions, []string{"stop"}) || !bytes.Equal(beforeBin, read(t, bin)) {
		t.Fatalf("explicit Stop was undone by recovery: active=%v actions=%v", svc.active, svc.actions)
	}
	m, generation, err := a.load()
	if err != nil || m.Version != "v1.19.33" || generation != j.Old {
		t.Fatalf("Stop did not recover old files: manifest=%+v generation=%q error=%v", m, generation, err)
	}
	assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
}

func TestUpgradeCoreRecoveryFailurePreservesJournalAndReportsBothFailures(t *testing.T) {
	a, svc := updateFixture(t, "v1.19.33")
	svc.active = true
	check := checkedUpgrade(t, a)
	bin, _, _ := a.paths()
	beforeBin := read(t, bin)
	svc.actionHook = func(ctx context.Context, action string) error {
		if action != "restart" && action != "start" {
			return ctx.Err()
		}
		svc.active = false
		if string(read(t, bin)) == "version:v1.20.0" {
			return errors.New("new restart failed")
		}
		return errors.New("old restart also failed")
	}
	_, err := a.UpgradeCore(t.Context(), check, downloadnet.Config{}, nil)
	if err == nil || !strings.Contains(err.Error(), "new restart failed") || !strings.Contains(err.Error(), "old restart also failed") {
		t.Fatalf("missing original or recovery cause: %v", err)
	}
	if !bytes.Equal(beforeBin, read(t, bin)) {
		t.Fatal("rollback failure failed to restore the old binary")
	}
	if _, err := os.Stat(filepath.Join(a.Home, "core-pending.json")); err != nil {
		t.Fatalf("recovery journal discarded: %v", err)
	}
	backups, err := filepath.Glob(filepath.Join(a.Home, "installation", "core-backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("rollback backup not preserved: %v %v", backups, err)
	}
	// A subsequent explicit recovery can complete after systemd becomes usable.
	svc.actionHook = nil
	if err := a.locked(t.Context(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !svc.active {
		t.Fatal("pending recovery did not restore the original running state")
	}
	assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
}

func TestUpgradeCoreRejectsUnreviewedEditedAndStaleChecks(t *testing.T) {
	for _, scenario := range []string{"unreviewed", "edited-target", "edited-status", "stale-installation", "other-home"} {
		t.Run(scenario, func(t *testing.T) {
			a, svc := updateFixture(t, "v1.19.33")
			check := checkedUpgrade(t, a)
			switch scenario {
			case "unreviewed":
				check = CoreUpdateCheck{InstalledVersion: "v1.19.33", LatestVersion: "v1.20.0", Channel: mihomo.ChannelStable, Status: mihomo.UpdateAvailable}
			case "edited-target":
				check.LatestVersion = "v1.21.0"
			case "edited-status":
				check.Status = mihomo.UpdateCurrent
			case "stale-installation":
				if err := a.Apply(t.Context(), "default"); err != nil {
					t.Fatal(err)
				}
			case "other-home":
				a, svc = updateFixture(t, "v1.19.33")
			}
			a.DownloadRelease = func(context.Context, string, mihomo.Release, downloadnet.Config, func(mihomo.Progress)) error {
				t.Fatal("invalid check downloaded a core")
				return nil
			}
			before := updateTree(t, a.Home)
			if _, err := a.UpgradeCore(t.Context(), check, downloadnet.Config{}, nil); err == nil {
				t.Fatal("invalid check authorized an upgrade")
			}
			if !reflect.DeepEqual(before, updateTree(t, a.Home)) || len(svc.actions) != 0 {
				t.Fatal("invalid check changed files or service")
			}
		})
	}
}

func TestUpgradeCoreCannotDowngradeOrOrderAlphaHashes(t *testing.T) {
	for _, tc := range []struct{ installed, latest string }{
		{"v1.21.0", "v1.20.0"},
		{"v1.20.0", "v1.20.0"},
		{"alpha-fffffff", "alpha-0000000"},
	} {
		t.Run(tc.installed, func(t *testing.T) {
			a, svc := updateFixture(t, tc.installed)
			a.QueryLatest = func(context.Context, mihomo.Channel, downloadnet.Config) (string, error) { return tc.latest, nil }
			check, err := a.CheckUpdate(t.Context(), "", downloadnet.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.UpgradeCore(t.Context(), check, downloadnet.Config{}, nil); err == nil {
				t.Fatalf("unsafe upgrade accepted: %+v", check)
			}
			if len(svc.actions) != 0 {
				t.Fatalf("unsafe upgrade changed service: %v", svc.actions)
			}
		})
	}
}

func TestConcurrentUpgradeRequestsPublishOneReviewedTarget(t *testing.T) {
	a, svc := updateFixture(t, "v1.19.33")
	check := checkedUpgrade(t, a)
	var downloads atomic.Int32
	download := a.DownloadRelease
	a.DownloadRelease = func(ctx context.Context, dest string, release mihomo.Release, cfg downloadnet.Config, progress func(mihomo.Progress)) error {
		downloads.Add(1)
		return download(ctx, dest, release, cfg, progress)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			<-start
			_, err := a.UpgradeCore(t.Context(), check, downloadnet.Config{}, nil)
			results <- err
		})
	}
	close(start)
	workers.Wait()
	close(results)
	succeeded, rejected := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else {
			rejected++
		}
	}
	if succeeded != 1 || rejected != 1 || downloads.Load() != 1 {
		t.Fatalf("success=%d rejected=%d downloads=%d", succeeded, rejected, downloads.Load())
	}
	m, _, err := a.load()
	if err != nil || m.Version != "v1.20.0" || svc.active || len(svc.actions) != 0 {
		t.Fatalf("concurrent publication corrupted installation: manifest=%+v error=%v actions=%v", m, err, svc.actions)
	}
	assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
}

func TestUpgradeCoreRejectsActiveServiceAgainstSavedStopIntent(t *testing.T) {
	for _, intent := range []string{"stop-marker", "desired-stopped"} {
		t.Run(intent, func(t *testing.T) {
			a, svc := updateFixture(t, "v1.19.33")
			if intent == "stop-marker" {
				if err := a.rememberStop(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := a.Control(t.Context(), "stop"); err != nil {
					t.Fatal(err)
				}
				if err := a.clearStop(); err != nil {
					t.Fatal(err)
				}
				svc.actions = nil
			}
			// A unit started outside lazyclash must not silently erase an explicit
			// saved Stop or lead to rollback violating that Stop later.
			svc.active = true
			check := checkedUpgrade(t, a)
			a.DownloadRelease = func(context.Context, string, mihomo.Release, downloadnet.Config, func(mihomo.Progress)) error {
				t.Fatal("upgrade downloaded before resolving the saved stop intent")
				return nil
			}
			before := updateTree(t, a.Home)
			if _, err := a.UpgradeCore(t.Context(), check, downloadnet.Config{}, nil); err == nil {
				t.Fatal("upgrade accepted an active unit against saved stop intent")
			}
			if !reflect.DeepEqual(before, updateTree(t, a.Home)) || len(svc.actions) != 0 || !svc.active {
				t.Fatalf("rejected upgrade modified files or service: active=%v actions=%v", svc.active, svc.actions)
			}
		})
	}
}

type upgradeStartLimitService struct {
	*fakeService
	bin       string
	blocked   bool
	resets    int
	resetErr  error
	resetArgs [3]string
}

func (s *upgradeStartLimitService) Action(ctx context.Context, action string) error {
	if action != "restart" && action != "start" {
		return s.fakeService.Action(ctx, action)
	}
	raw, err := os.ReadFile(s.bin)
	if err != nil {
		return err
	}
	if string(raw) == "version:v1.20.0" {
		s.actions = append(s.actions, action)
		s.blocked, s.active = true, false
		return errors.New("new core hit systemd start limit")
	}
	if s.blocked {
		s.actions = append(s.actions, action)
		return errors.New("old core restart blocked by start limit")
	}
	return s.fakeService.Action(ctx, action)
}

func (s *upgradeStartLimitService) ResetFailedOwned(ctx context.Context, bin, config, data string) error {
	s.resets++
	s.resetArgs = [3]string{bin, config, data}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.resetErr != nil {
		return s.resetErr
	}
	s.blocked = false
	return nil
}

func TestUpgradeRollbackResetsOwnedStartLimitBeforeRestartingOldCore(t *testing.T) {
	a, svc := updateFixture(t, "v1.19.33")
	svc.active = true
	bin, config, data := a.paths()
	startLimited := &upgradeStartLimitService{fakeService: svc, bin: bin}
	a.Service = startLimited
	check := checkedUpgrade(t, a)
	beforeBin := read(t, bin)
	_, err := a.UpgradeCore(t.Context(), check, downloadnet.Config{}, nil)
	if err == nil || !strings.Contains(err.Error(), "new core hit systemd start limit") {
		t.Fatalf("upgrade failure not reported: %v", err)
	}
	if startLimited.resets != 1 || startLimited.resetArgs != [3]string{bin, config, data} || startLimited.blocked || !svc.active || !bytes.Equal(beforeBin, read(t, bin)) {
		t.Fatalf("rollback failed to clear the owned unit start limit: resets=%d args=%v blocked=%v active=%v actions=%v", startLimited.resets, startLimited.resetArgs, startLimited.blocked, svc.active, svc.actions)
	}
	assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
}

func TestUpgradeRollbackResetFailurePreservesRecoveryMaterials(t *testing.T) {
	a, svc := updateFixture(t, "v1.19.33")
	svc.active = true
	bin, _, _ := a.paths()
	startLimited := &upgradeStartLimitService{fakeService: svc, bin: bin, resetErr: errors.New("owned unit reset failed")}
	a.Service = startLimited
	check := checkedUpgrade(t, a)
	beforeBin := read(t, bin)
	_, err := a.UpgradeCore(t.Context(), check, downloadnet.Config{}, nil)
	if err == nil || !strings.Contains(err.Error(), "new core hit systemd start limit") || !strings.Contains(err.Error(), "owned unit reset failed") {
		t.Fatalf("missing original or reset failure: %v", err)
	}
	if !bytes.Equal(beforeBin, read(t, bin)) || !startLimited.blocked || svc.active || startLimited.resets != 1 {
		t.Fatalf("reset failure state incorrect: blocked=%v active=%v resets=%d", startLimited.blocked, svc.active, startLimited.resets)
	}
	if _, err := os.Stat(filepath.Join(a.Home, "core-pending.json")); err != nil {
		t.Fatalf("reset failure discarded recovery journal: %v", err)
	}
	backups, err := filepath.Glob(filepath.Join(a.Home, "installation", "core-backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("reset failure discarded recovery backup: %v %v", backups, err)
	}
	startLimited.resetErr = nil
	if err := a.locked(t.Context(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !svc.active || startLimited.blocked || startLimited.resets != 2 {
		t.Fatalf("retry did not recover old running state: active=%v blocked=%v resets=%d", svc.active, startLimited.blocked, startLimited.resets)
	}
	assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
}

func TestUpgradeCoreRejectsExternalServiceStateChangeDuringStaging(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(fmt.Sprintf("initial-active-%v", active), func(t *testing.T) {
			a, svc := updateFixture(t, "v1.19.33")
			svc.active = active
			check := checkedUpgrade(t, a)
			bin, config, _ := a.paths()
			beforeBin, beforeConfig := read(t, bin), read(t, config)
			_, oldGeneration, err := a.load()
			if err != nil {
				t.Fatal(err)
			}
			download := a.DownloadRelease
			a.DownloadRelease = func(ctx context.Context, dest string, release mihomo.Release, cfg downloadnet.Config, progress func(mihomo.Progress)) error {
				if err := download(ctx, dest, release, cfg, progress); err != nil {
					return err
				}
				// Simulate a direct systemctl action outside the app's namespace
				// lock while the archive was being transferred.
				svc.active = !active
				return nil
			}
			if _, err := a.UpgradeCore(t.Context(), check, downloadnet.Config{}, nil); err == nil {
				t.Fatal("upgrade continued after external service state changed")
			}
			_, generation, err := a.load()
			if err != nil || generation != oldGeneration || !bytes.Equal(beforeBin, read(t, bin)) || !bytes.Equal(beforeConfig, read(t, config)) || len(svc.actions) != 0 || svc.active != !active {
				t.Fatalf("stale runtime observation changed files or external state: generation=%q error=%v active=%v actions=%v", generation, err, svc.active, svc.actions)
			}
			assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
		})
	}
}
