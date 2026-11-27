package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/testfixture"
)

func TestOnlineInstallPreparesGeoAfterCommitAndRetainsPartialSuccess(t *testing.T) {
	a, svc := coreFixture(t)
	cfg := downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: "https://mirror.example"}
	var calls atomic.Int32
	started := make(chan struct{})
	a.DownloadGeo = func(ctx context.Context, kind, target string, actual downloadnet.Config, report func(mihomo.Progress)) error {
		if actual != cfg || svc.installs != 1 {
			t.Errorf("wrong route or uncommitted installation: route=%+v, installs=%d", actual, svc.installs)
		}
		if _, err := os.Stat(filepath.Join(a.Home, "install-pending.json")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("core install journal present during GEO download: %v", err)
		}
		if calls.Add(1) == 2 {
			close(started)
		}
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			return errors.New("downloads did not run in parallel")
		}
		report(mihomo.Progress{Stage: "geodata", Detail: kind})
		if kind == "mmdb" {
			return errors.New("private fixture transport failure https://private.example/?token=secret")
		}
		return os.WriteFile(target, testfixture.GeoSite(), 0600)
	}
	plan, err := a.PrepareInstall(t.Context(), mihomo.Release{Version: "v1.20.0", Size: 128}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	var stages []string
	installed, err := a.InstallCore(t.Context(), plan, cfg, func(p mihomo.Progress) {
		stages = append(stages, p.Stage)
	})
	result := installed.GeoData
	if err != nil || result == nil || result.Cancelled || result.Warning == "" || strings.Contains(result.Warning, "private") ||
		!slices.Equal(result.Failed, []string{"geoip.metadb"}) || !slices.Equal(result.Prepared, []string{"GeoSite.dat"}) {
		t.Fatalf("installation=%+v, GEO=%+v, error=%v", installed, result, err)
	}
	if len(stages) == 0 || slices.Index(stages, "done") != len(stages)-1 || !slices.Contains(stages, "geodata") {
		t.Fatalf("installation progress: %v", stages)
	}
	bin, _, live := a.paths()
	if string(read(t, bin)) != "version:v1.20.0" || !bytes.Equal(read(t, filepath.Join(live, "GeoSite.dat")), testfixture.GeoSite()) {
		t.Fatal("failed country download did not retain the core and the completed GeoSite file")
	}
	assertMissing(t, filepath.Join(live, "geoip.metadb"))
	calls.Store(0)
	a.DownloadGeo = func(ctx context.Context, kind, target string, _ downloadnet.Config, _ func(mihomo.Progress)) error {
		calls.Add(1)
		if kind != "mmdb" {
			return errors.New("already-installed GeoSite requested")
		}
		return os.WriteFile(target, testfixture.CountryMMDB(), 0600)
	}
	retry, err := a.PrepareDefaultGeo(t.Context(), cfg, nil)
	if err != nil || !slices.Equal(retry.Present, []string{"GeoSite.dat"}) || !slices.Equal(retry.Prepared, []string{"geoip.metadb"}) {
		t.Fatalf("retry result=%+v err=%v", retry, err)
	}
	again, err := a.PrepareDefaultGeo(t.Context(), cfg, nil)
	if err != nil || !slices.Equal(again.Present, []string{"geoip.metadb", "GeoSite.dat"}) || calls.Load() != 1 || svc.active || len(svc.actions) != 0 {
		t.Fatalf("repeat result=%+v, calls=%d, service=%+v, err=%v", again, calls.Load(), svc, err)
	}
}

func TestPrepareDefaultGeoCancellationKeepsCompletedAsset(t *testing.T) {
	a, svc := installedApp(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	completed := make(chan struct{})
	a.DownloadGeo = func(ctx context.Context, kind, target string, _ downloadnet.Config, _ func(mihomo.Progress)) error {
		if kind == "mmdb" {
			err := os.WriteFile(target, testfixture.CountryMMDB(), 0600)
			close(completed)
			return err
		}
		<-completed
		cancel()
		return ctx.Err()
	}
	prepared, err := a.PrepareDefaultGeo(ctx, downloadnet.Config{}, nil)
	if !errors.Is(err, context.Canceled) || !prepared.Cancelled || prepared.Warning != "" || !slices.Equal(prepared.Prepared, []string{"geoip.metadb"}) {
		t.Fatalf("outcome=%+v, err=%v", prepared, err)
	}
	_, _, live := a.paths()
	if !bytes.Equal(read(t, filepath.Join(live, "geoip.metadb")), testfixture.CountryMMDB()) || svc.active || len(svc.actions) != 0 {
		t.Fatal("completed file or stopped service intent lost")
	}
	assertMissing(t, filepath.Join(live, "GeoSite.dat"))
	assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
}

func TestPrepareDefaultGeoRepairsAliasAndRespectsCustomSource(t *testing.T) {
	a, svc := installedApp(t)
	p := filepath.Join(t.TempDir(), "custom.yaml")
	write(t, p, []byte("mode: rule\ngeox-url:\n  geosite: https://example.invalid/own.dat\nrules: ['MATCH,DIRECT']\n"))
	if err := a.Add(t.Context(), "custom", p); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "custom"); err != nil {
		t.Fatal(err)
	}
	_, _, live := a.paths()
	write(t, filepath.Join(live, "Country.mmdb"), []byte("bad preferred alias"))
	write(t, filepath.Join(live, "geoip.metadb"), testfixture.CountryMMDB())
	write(t, filepath.Join(live, "GeoSite.dat"), []byte("custom site data"))
	var calls atomic.Int32
	a.DownloadGeo = func(context.Context, string, string, downloadnet.Config, func(mihomo.Progress)) error {
		calls.Add(1)
		return errors.New("unexpected download")
	}
	result, err := a.PrepareDefaultGeo(t.Context(), downloadnet.Config{}, nil)
	if err != nil || !slices.Equal(result.Prepared, []string{"geoip.metadb"}) || !slices.Equal(result.Skipped, []string{"GeoSite.dat"}) || calls.Load() != 0 {
		t.Fatalf("alias repair = %+v, calls = %d, err = %v", result, calls.Load(), err)
	}
	assertMissing(t, filepath.Join(live, "Country.mmdb"))
	if string(read(t, filepath.Join(live, "GeoSite.dat"))) != "custom site data" {
		t.Fatal("custom GeoSite replaced")
	}
	svc.active = true
	result, err = a.PrepareDefaultGeo(t.Context(), downloadnet.Config{}, nil)
	if err == nil || !strings.Contains(result.Warning, "stop") || calls.Load() != 0 || len(svc.actions) != 0 || !svc.active {
		t.Fatalf("active service preparation=%+v, actions=%v, error=%v", result, svc.actions, err)
	}
}

func TestPrepareDefaultGeoRechecksServiceAfterDownloads(t *testing.T) {
	a, svc := installedApp(t)
	a.DownloadGeo = func(ctx context.Context, kind, target string, _ downloadnet.Config, _ func(mihomo.Progress)) error {
		if kind == "mmdb" {
			svc.active = true
			return os.WriteFile(target, testfixture.CountryMMDB(), 0600)
		}
		return os.WriteFile(target, testfixture.GeoSite(), 0600)
	}
	result, err := a.PrepareDefaultGeo(t.Context(), downloadnet.Config{}, nil)
	if err == nil || len(result.Prepared) != 0 || len(result.Failed) != 2 || !svc.active || len(svc.actions) != 0 {
		t.Fatalf("service started during preparation: result=%+v, service=%+v, err=%v", result, svc, err)
	}
	_, _, live := a.paths()
	assertMissing(t, filepath.Join(live, "geoip.metadb"))
	assertMissing(t, filepath.Join(live, "GeoSite.dat"))
	assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
}

func TestCoreLocalInstallAndLaterReplacementsSkipGeoSeeding(t *testing.T) {
	a, _ := coreFixture(t)
	local := filepath.Join(t.TempDir(), "binary")
	write(t, local, []byte("version:v1.20.0"))
	var calls atomic.Int32
	a.DownloadGeo = func(context.Context, string, string, downloadnet.Config, func(mihomo.Progress)) error {
		calls.Add(1)
		return errors.New("unexpected download")
	}
	for _, step := range []struct {
		version, local string
		reinstall      bool
	}{
		{"v1.20.0", local, false},
		{"v1.20.0", "", false},
		{"v1.20.0", "", true},
		{"v1.21.0", "", false},
	} {
		plan, err := a.PrepareInstall(t.Context(), mihomo.Release{Version: step.version, Size: 128}, step.local, step.reinstall)
		if err != nil {
			t.Fatal(err)
		}
		result, err := a.InstallCore(t.Context(), plan, downloadnet.Config{}, nil)
		if err != nil || result.GeoData != nil || calls.Load() != 0 {
			t.Fatalf("operation %s: GEO=%+v, calls=%d, err=%v", plan.Operation, result.GeoData, calls.Load(), err)
		}
	}
}
