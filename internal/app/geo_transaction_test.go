package app

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nelvko/lazyclash/internal/store"
)

func TestGeoSwitchPublishesValidatedBytesAndRollsBackFailedRestart(t *testing.T) {
	for _, failRestart := range []bool{false, true} {
		name := "publish"
		if failRestart {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = true
			m, old, err := a.load()
			if err != nil {
				t.Fatal(err)
			}
			_, config, runtime := a.paths()
			oldCfg := read(t, config)
			newCfg := append([]byte("# new GEO source\n"), oldCfg...)
			write(t, filepath.Join(runtime, "Country.mmdb"), []byte("old country"))
			write(t, filepath.Join(runtime, "geoip.db"), []byte("old alias"))
			write(t, filepath.Join(runtime, "GeoSite.dat"), []byte("unchanged"))
			write(t, filepath.Join(runtime, "cache.db"), []byte("selection cache"))
			src := maps.Clone(m.GeoSources)
			src["mmdb"] = digest([]byte("new source"))
			c := &geoCandidate{
				Data: t.TempDir(), Sources: src,
				Before: map[string]string{"Country.mmdb": digest([]byte("old country")), "geoip.db": digest([]byte("old alias")), "GeoSite.dat": digest([]byte("unchanged"))},
				After:  map[string]string{"geoip.metadb": digest([]byte("validated country")), "GeoSite.dat": digest([]byte("unchanged"))},
			}
			write(t, filepath.Join(c.Data, "geoip.metadb"), []byte("validated country"))
			var sawCandidate, sawBarrier bool
			svc.actionHook = func(_ context.Context, action string) error {
				if action == "stop" {
					if _, err := os.Open(config); !errors.Is(err, syscall.ENOTDIR) {
						t.Fatalf("service can start during a multi-file GEO transition: %v", err)
					}
					sawBarrier = true
				}
				if action == "restart" && string(read(t, config)) == string(newCfg) {
					sawCandidate = true
					if got := string(read(t, filepath.Join(runtime, "geoip.metadb"))); got != "validated country" {
						t.Fatalf("candidate config published with wrong GEO data: %s", got)
					}
					assertMissing(t, filepath.Join(runtime, "Country.mmdb"))
					assertMissing(t, filepath.Join(runtime, "geoip.db"))
					if failRestart {
						return errors.New("test service failed to start")
					}
				}
				return nil
			}
			status, _ := svc.Inspect(t.Context())
			err = a.publishPreparedGeo(t.Context(), old, m, newCfg, status, c)
			if (err != nil) != failRestart || !sawCandidate || !sawBarrier {
				t.Fatalf("switch (%v, barrier=%v, candidate=%v)", err, sawBarrier, sawCandidate)
			}
			if !svc.active {
				t.Fatal("original running intent was lost")
			}
			actual, target, err := a.load()
			if err != nil {
				t.Fatal(err)
			}
			if failRestart {
				if target != old || !maps.Equal(actual.GeoSources, m.GeoSources) ||
					string(read(t, filepath.Join(runtime, "Country.mmdb"))) != "old country" ||
					string(read(t, filepath.Join(runtime, "geoip.db"))) != "old alias" {
					t.Fatal("did not restore old config and aliases as a set")
				}
				assertMissing(t, filepath.Join(runtime, "geoip.metadb"))
			} else if target == old || !maps.Equal(actual.GeoSources, src) {
				t.Fatal("new source provenance was not published")
			}
			if string(read(t, filepath.Join(runtime, "GeoSite.dat"))) != "unchanged" ||
				string(read(t, filepath.Join(runtime, "cache.db"))) != "selection cache" {
				t.Fatal("unrelated runtime data was modified")
			}
			assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
		})
	}
}

func TestGeoRecoveryFromPartialPublicationWhileStopped(t *testing.T) {
	a, svc := installedApp(t)
	m, old, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	_, cfgPath, runtime := a.paths()
	base := filepath.Dir(runtime)
	previous := read(t, cfgPath)
	next, err := a.writeGeneration(base, m, append([]byte("# next\n"), previous...))
	if err != nil {
		t.Fatal(err)
	}
	oldHash, err := a.generationGuard(old)
	if err != nil {
		t.Fatal(err)
	}
	newHash, err := a.generationGuard(next)
	if err != nil {
		t.Fatal(err)
	}
	undoBytes := []byte("verified original")
	p := pending{Old: old, New: next, OldGuard: oldHash, NewGuard: newHash, Geo: &geoTransition{
		Kinds: map[string]bool{"mmdb": true, "geosite": true}, Before: map[string]string{"Country.mmdb": digest(undoBytes)}, Ready: true,
	}}
	undo := a.geoUndoDirectory(p)
	if err := os.Mkdir(undo, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(undo, "Country.mmdb"), undoBytes)
	raw, _ := json.Marshal(p)
	write(t, filepath.Join(a.Home, "apply-pending.json"), raw)
	if err := store.Switch(filepath.Join(base, "current"), geoBarrier(old), old); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(runtime, "geoip.metadb"), []byte("new file"))
	write(t, filepath.Join(runtime, "geosite.dat"), []byte("new file"))
	if err := a.recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if svc.active || string(read(t, cfgPath)) != string(previous) ||
		string(read(t, filepath.Join(runtime, "Country.mmdb"))) != string(undoBytes) {
		t.Fatal("interrupted switch did not restore original data and stopped state")
	}
	assertMissing(t, filepath.Join(runtime, "geoip.metadb"))
	assertMissing(t, filepath.Join(runtime, "geosite.dat"))
	assertMissing(t, undo)
	assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
}

func TestGeoCopyRejectsChangedAndLinkedDatabase(t *testing.T) {
	src, stage, dst := t.TempDir(), t.TempDir(), t.TempDir()
	file := filepath.Join(src, "GeoSite.dat")
	hash := digest([]byte("old"))
	write(t, file, []byte("new"))
	if err := copyGeoForSwitch(t.Context(), src, stage, dst, "GeoSite.dat", hash, -1, -1); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed candidate accepted: %v", err)
	}
	if err := os.Link(file, filepath.Join(src, "another")); err != nil {
		t.Fatal(err)
	}
	if err := copyGeoForSwitch(t.Context(), src, stage, dst, "GeoSite.dat", digest([]byte("new")), -1, -1); err == nil {
		t.Fatal("should not open multiply linked files as root")
	}
	assertMissing(t, filepath.Join(dst, "GeoSite.dat"))
}

func TestGeoTakeoverKeepsOnlyAssetsWithUnknownProvenance(t *testing.T) {
	a, svc := installedApp(t)
	m, old, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	_, cfgPath, runtime := a.paths()
	cfg := read(t, cfgPath)
	sources := maps.Clone(m.GeoSources)
	delete(m.GeoSources, "geosite")
	write(t, filepath.Join(runtime, "geosite.dat"), []byte("manual site"))
	write(t, filepath.Join(runtime, "GeoIP.dat"), []byte("managed IP"))
	// Simulate an older installation with no record for its manual GeoSite,
	// alongside another managed resource being replaced by the same switch.
	sources["geoip"] = digest([]byte("alternate IP source"))
	c := &geoCandidate{Data: t.TempDir(), Sources: sources, Before: map[string]string{
		"geosite.dat": digest([]byte("manual site")), "GeoIP.dat": digest([]byte("managed IP")),
	}, After: map[string]string{}}
	status, _ := svc.Inspect(t.Context())
	if err := a.publishPreparedGeo(t.Context(), old, m, cfg, status, c); err != nil {
		t.Fatal(err)
	}
	_, target, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(a.Home, "geodata-backup-"+filepath.Base(target))
	if data := string(read(t, filepath.Join(archive, "geosite.dat"))); data != "manual site" {
		t.Fatal("untracked file not retained")
	}
	assertMissing(t, filepath.Join(archive, "GeoIP.dat"))
	assertMissing(t, filepath.Join(runtime, "geosite.dat"))
	assertMissing(t, filepath.Join(runtime, "GeoIP.dat"))
}
