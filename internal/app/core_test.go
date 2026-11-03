package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

func coreFixture(t *testing.T) (*App, *fakeService) {
	t.Helper()
	a, svc := testApp(t)
	a.Version = func(ctx context.Context, bin string) (string, error) {
		raw, err := os.ReadFile(bin)
		if err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if strings.HasPrefix(string(raw), "version:") {
			return strings.TrimPrefix(string(raw), "version:"), nil
		}
		return testMihomoVersion, nil
	}
	a.Resolve = func(ctx context.Context, selection string, cfg downloadnet.Config) (mihomo.Release, error) {
		return mihomo.Release{Version: "v1.20.0", Asset: "fixture.gz", Size: 128}, ctx.Err()
	}
	a.DownloadRelease = func(ctx context.Context, dest string, r mihomo.Release, cfg downloadnet.Config, progress func(mihomo.Progress)) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return os.WriteFile(dest, []byte("version:"+r.Version), 0700)
	}
	return a, svc
}

func TestCoreUpdatePreservesRunningInodeConfigurationAndService(t *testing.T) {
	a, svc := coreFixture(t)
	if err := a.Install(t.Context(), "", nil); err != nil {
		t.Fatal(err)
	}
	svc.active = true
	oldManifest, _, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	bin, cfgPath, _ := a.paths()
	beforeConfig := read(t, cfgPath)
	oldProcess, err := os.Open(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer oldProcess.Close()
	plan, err := a.PrepareInstall(t.Context(), mihomo.Release{Version: "v1.20.0", Size: 128}, "", false)
	if err != nil || plan.BlockedReason != "" || plan.Operation != "update" || plan.RunningVersion != testMihomoVersion {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	result, err := a.InstallCore(t.Context(), plan, downloadnet.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.RestartRequired || result.RunningVersion != testMihomoVersion || result.InstalledVersion != "v1.20.0" {
		t.Fatalf("result=%+v", result)
	}
	if len(svc.actions) != 0 || !svc.active || svc.installs != 1 {
		t.Fatalf("update changed service: %+v", svc)
	}
	oldBytes, err := io.ReadAll(oldProcess)
	if err != nil || string(oldBytes) != "fixture engine" {
		t.Fatalf("running inode was overwritten: %q %v", oldBytes, err)
	}
	if string(read(t, bin)) != "version:v1.20.0" || !bytes.Equal(beforeConfig, read(t, cfgPath)) {
		t.Fatal("binary path or configuration was not preserved correctly")
	}
	updated, _, err := a.load()
	if err != nil || updated.ID != oldManifest.ID || updated.Secret != oldManifest.Secret || updated.Applied != oldManifest.Applied || updated.Version != "v1.20.0" {
		t.Fatalf("metadata transaction failed: %+v %v", updated, err)
	}
	assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
	backups, _ := filepath.Glob(filepath.Join(a.Home, "installation", "core-backup-*"))
	if len(backups) != 0 {
		t.Fatalf("backup files leaked: %v", backups)
	}
}

func TestCoreUpdateWithUnavailableRunningAPIStillRequiresRestart(t *testing.T) {
	a, svc := coreFixture(t)
	if err := a.Install(t.Context(), "", nil); err != nil {
		t.Fatal(err)
	}
	svc.active = true
	a.HTTP = &http.Client{Transport: fixtureTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("API unavailable")
	})}
	p, err := a.PrepareInstall(t.Context(), mihomo.Release{Version: "v1.20.0"}, "", false)
	if err != nil || !p.ServiceActive || p.RunningVersion != "" || len(p.Warnings) == 0 {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
	r, err := a.InstallCore(t.Context(), p, downloadnet.Config{}, nil)
	if err != nil || !r.RestartRequired || len(svc.actions) != 0 || !svc.active {
		t.Fatalf("result=%+v error=%v actions=%v", r, err, svc.actions)
	}
}

func TestInstallationPlanIsBoundToItsReviewedNamespace(t *testing.T) {
	a, _ := coreFixture(t)
	b, _ := coreFixture(t)
	p, err := a.PrepareInstall(t.Context(), mihomo.Release{Version: "v1.20.0"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.InstallCore(t.Context(), p, downloadnet.Config{}, nil); err == nil {
		t.Fatal("plan for another home was accepted")
	}
	assertMissing(t, filepath.Join(b.Home, "installation"))
}

func TestCoreSameVersionIsNoOpUnlessReinstallWasRequested(t *testing.T) {
	a, svc := coreFixture(t)
	if err := a.Install(t.Context(), "", nil); err != nil {
		t.Fatal(err)
	}
	downloads := 0
	download := a.DownloadRelease
	a.DownloadRelease = func(ctx context.Context, dest string, r mihomo.Release, cfg downloadnet.Config, p func(mihomo.Progress)) error {
		downloads++
		return download(ctx, dest, r, cfg, p)
	}
	bin, _, _ := a.paths()
	before, _ := os.Stat(bin)
	for _, reinstall := range []bool{false, true} {
		plan, err := a.PrepareInstall(t.Context(), mihomo.Release{Version: testMihomoVersion}, "", reinstall)
		if err != nil {
			t.Fatal(err)
		}
		want := "use existing"
		if reinstall {
			want = "reinstall"
		}
		if plan.Operation != want {
			t.Fatalf("operation=%s", plan.Operation)
		}
		if _, err := a.InstallCore(t.Context(), plan, downloadnet.Config{}, nil); err != nil {
			t.Fatal(err)
		}
		after, _ := os.Stat(bin)
		if !reinstall && (!os.SameFile(before, after) || downloads != 0) {
			t.Fatal("same version performed an implicit reinstall")
		}
	}
	if downloads != 1 || len(svc.actions) != 0 {
		t.Fatalf("downloads=%d service actions=%v", downloads, svc.actions)
	}
}

func TestCoreRejectsStaleAndEditedPlansAndChangedLocalBinary(t *testing.T) {
	a, _ := coreFixture(t)
	if err := a.Install(t.Context(), "", nil); err != nil {
		t.Fatal(err)
	}
	plan, err := a.PrepareInstall(t.Context(), mihomo.Release{Version: "v1.20.0"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	edited := plan
	edited.Release.Version = "v9.9.9"
	if _, err := a.InstallCore(t.Context(), edited, downloadnet.Config{}, nil); err == nil {
		t.Fatal("edited plan accepted")
	}
	if err := a.Apply(t.Context(), "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.InstallCore(t.Context(), plan, downloadnet.Config{}, nil); err == nil || !strings.Contains(err.Error(), "since preflight") {
		t.Fatalf("stale plan accepted: %v", err)
	}
	local := filepath.Join(t.TempDir(), "mihomo")
	write(t, local, []byte("version:v1.20.0"))
	plan, err = a.PrepareInstall(t.Context(), mihomo.Release{}, local, false)
	if err != nil {
		t.Fatal(err)
	}
	write(t, local, []byte("different executable with the same reported version"))
	// Keep version constant to ensure the digest, rather than version, guards it.
	a.Version = func(context.Context, string) (string, error) { return "v1.20.0", nil }
	if _, err := a.InstallCore(t.Context(), plan, downloadnet.Config{}, nil); err == nil {
		t.Fatal("changed local executable accepted")
	}
}

func TestCoreCancellationAndValidationFailurePreserveInstallation(t *testing.T) {
	for _, fail := range []string{"cancel-download", "invalid-config", "wrong-version"} {
		t.Run(fail, func(t *testing.T) {
			a, svc := coreFixture(t)
			if err := a.Install(t.Context(), "", nil); err != nil {
				t.Fatal(err)
			}
			bin, config, _ := a.paths()
			beforeBin, beforeConfig := read(t, bin), read(t, config)
			plan, err := a.PrepareInstall(t.Context(), mihomo.Release{Version: "v1.20.0"}, "", false)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch fail {
			case "cancel-download":
				a.DownloadRelease = func(ctx context.Context, dest string, r mihomo.Release, cfg downloadnet.Config, p func(mihomo.Progress)) error {
					write(t, dest, []byte("partial"))
					cancel()
					return ctx.Err()
				}
			case "invalid-config":
				a.Validate = func(context.Context, string, string, string) error {
					return errors.New("new core rejected configuration")
				}
			case "wrong-version":
				a.DownloadRelease = func(ctx context.Context, dest string, r mihomo.Release, cfg downloadnet.Config, p func(mihomo.Progress)) error {
					return os.WriteFile(dest, []byte("version:v0.0.1"), 0700)
				}
			}
			if _, err := a.InstallCore(ctx, plan, downloadnet.Config{}, nil); err == nil {
				t.Fatal("failed operation accepted")
			}
			if !bytes.Equal(beforeBin, read(t, bin)) || !bytes.Equal(beforeConfig, read(t, config)) || len(svc.actions) != 0 {
				t.Fatal("failed update changed installation or service")
			}
			staging, _ := filepath.Glob(filepath.Join(a.Home, ".core-*"))
			if len(staging) != 0 {
				t.Fatalf("staging leaked: %v", staging)
			}
		})
	}
}

func stageCoreRecovery(t *testing.T, a *App, binarySwitched, pointerSwitched, committed bool) coreJournal {
	t.Helper()
	m, old, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(a.Home, "installation")
	bin, cfgPath, _ := a.paths()
	cfg := read(t, cfgPath)
	oldSHA := m.BinarySHA
	newBin := filepath.Join(t.TempDir(), "new-core")
	write(t, newBin, []byte("version:v1.20.0"))
	m.Version = "v1.20.0"
	m.BinarySHA, _ = fileDigest(newBin)
	next, err := a.writeGeneration(base, m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	j := coreJournal{Old: old, New: next, Backup: "core-backup-" + randomID(), OldBinarySHA: oldSHA, NewBinarySHA: m.BinarySHA, OldManifestSHA: digest(read(t, filepath.Join(base, old, "manifest.json"))), NewManifestSHA: digest(read(t, filepath.Join(base, next, "manifest.json"))), ConfigSHA: digest(cfg), Service: a.Name, ID: m.ID, Committed: committed}
	if err := os.Link(bin, filepath.Join(base, j.Backup)); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(j)
	if err := store.Replace(filepath.Join(a.Home, "core-pending.json"), raw, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if binarySwitched {
		if err := os.Rename(newBin, bin); err != nil {
			t.Fatal(err)
		}
	}
	if pointerSwitched {
		if err := store.Switch(filepath.Join(base, "current"), next, old); err != nil {
			t.Fatal(err)
		}
	}
	return j
}

func TestCoreInterruptedPublicationRecoversEveryBoundaryWithoutServiceAction(t *testing.T) {
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
			a, svc := coreFixture(t)
			if err := a.Install(t.Context(), "", nil); err != nil {
				t.Fatal(err)
			}
			svc.active = true
			stageCoreRecovery(t, a, tc.binary, tc.pointer, tc.committed)
			snapshot, err := a.Snapshot(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			want := testMihomoVersion
			if tc.committed {
				want = "v1.20.0"
			}
			if snapshot.Version != want || !svc.active || len(svc.actions) != 0 {
				t.Fatalf("recovery=%+v actions=%v", snapshot, svc.actions)
			}
			assertMissing(t, filepath.Join(a.Home, "core-pending.json"))
		})
	}
}

func TestCoreRecoveryProtectsUserChanges(t *testing.T) {
	a, svc := coreFixture(t)
	if err := a.Install(t.Context(), "", nil); err != nil {
		t.Fatal(err)
	}
	j := stageCoreRecovery(t, a, true, true, false)
	path := filepath.Join(a.Home, "installation", j.New, "config.yaml")
	write(t, path, []byte("user-change-with-private-token"))
	if _, err := a.Snapshot(t.Context()); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("unsafe recovery response: %v", err)
	}
	if string(read(t, path)) != "user-change-with-private-token" || len(svc.actions) != 0 {
		t.Fatal("recovery destroyed user changes")
	}
	if _, err := os.Stat(filepath.Join(a.Home, "core-pending.json")); err != nil {
		t.Fatal("recovery journal lost")
	}
}

func TestPreflightDiskPermissionsAndUnsupportedPlatform(t *testing.T) {
	a, _ := coreFixture(t)
	a.DiskSpace = func(string) (uint64, error) { return 1, nil }
	p, err := a.PrepareInstall(t.Context(), mihomo.Release{Version: "v1.20.0"}, "", false)
	if err != nil || !strings.Contains(p.BlockedReason, "disk space") || !p.DiskKnown {
		t.Fatalf("low disk plan=%+v err=%v", p, err)
	}
	a.DiskSpace = func(string) (uint64, error) { return 0, errors.New("unavailable") }
	p, err = a.PrepareInstall(t.Context(), mihomo.Release{Version: "v1.20.0"}, "", false)
	if err != nil || p.DiskKnown || len(p.Warnings) == 0 {
		t.Fatalf("unknown disk plan=%+v err=%v", p, err)
	}
	a.Service = nil
	a.PlatformError = errors.New("service management requires Linux with systemd")
	p, err = a.PrepareInstall(t.Context(), mihomo.Release{Version: "v1.20.0"}, "", false)
	if err != nil || !strings.Contains(p.BlockedReason, "Linux") {
		t.Fatalf("unsupported platform plan=%+v err=%v", p, err)
	}
	if os.Geteuid() == 0 {
		return // root bypasses directory write permission checks
	}
	a, _ = coreFixture(t)
	if _, err := a.LoadPreferences(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(a.Home, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(a.Home, 0700)
	p, err = a.PrepareInstall(t.Context(), mihomo.Release{Version: "v1.20.0"}, "", false)
	if err != nil || !strings.Contains(p.BlockedReason, "not writable") {
		t.Fatalf("permission preflight=%+v err=%v", p, err)
	}
}

func TestPreflightReservesArchiveSpaceWhenHTTPSizeIsUnknown(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "first-install", true: "update"}[installed], func(t *testing.T) {
			a, _ := coreFixture(t)
			if installed {
				if err := a.Install(t.Context(), "", nil); err != nil {
					t.Fatal(err)
				}
			}
			available := uint64(280 << 20)
			if installed {
				available = 160 << 20
			}
			a.DiskSpace = func(string) (uint64, error) { return available, nil }
			release := mihomo.Release{Version: "v1.20.0"}
			plan, err := a.PrepareInstall(t.Context(), release, "", false)
			if err != nil || !strings.Contains(plan.BlockedReason, "disk space") {
				t.Fatalf("unknown archive size did not reserve transfer space: plan=%+v err=%v", plan, err)
			}
			release.Size = 8 << 20
			plan, err = a.PrepareInstall(t.Context(), release, "", false)
			if err != nil || plan.BlockedReason != "" {
				t.Fatalf("known small archive should fit: plan=%+v err=%v", plan, err)
			}
		})
	}
}
