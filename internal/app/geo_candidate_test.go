package app

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestGeoCandidateIsolatesValidationAndChecksSourceByKind(t *testing.T) {
	a, _ := installedApp(t)
	m, _, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	bin, _, live := a.paths()
	base := []byte("geox-url: {geoip: https://a.invalid/ip, geosite: https://a.invalid/site}\n")
	m.GeoSources, err = mihomo.GeoSources(base)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(live, "GeoIP.DAT"), []byte("same"))
	write(t, filepath.Join(live, "geosite.dat"), []byte("from A"))
	write(t, filepath.Join(live, "cache.db"), []byte("private process cache"))
	if err := os.Mkdir(filepath.Join(live, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(live, "assets", "relative.yaml"), []byte("relative asset"))
	cfg := []byte("geox-url: {geoip: https://a.invalid/ip, geosite: https://b.invalid/site}\n")
	a.Validate = func(ctx context.Context, _, config, data string) error {
		if data == live || string(read(t, config)) != string(cfg) {
			t.Fatalf("wrong validation paths")
		}
		if string(read(t, filepath.Join(data, "assets", "relative.yaml"))) != "relative asset" {
			t.Fatal("lost relative asset")
		}
		if string(read(t, filepath.Join(data, "GeoIP.DAT"))) != "same" {
			t.Fatal("did not reuse same source")
		}
		assertMissing(t, filepath.Join(data, "cache.db"))
		assertMissing(t, filepath.Join(data, "geosite.dat"))
		// Model the core modifying both matching and newly acquired files.
		write(t, filepath.Join(data, "GeoIP.DAT"), []byte("validated ip"))
		write(t, filepath.Join(data, "GeoSite.dat"), []byte("from B"))
		return ctx.Err()
	}
	c, err := a.prepareGeoCandidate(t.Context(), bin, m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.After["GeoSite.dat"] != digest([]byte("from B")) || c.Before["geosite.dat"] != digest([]byte("from A")) ||
		c.After["GeoIP.DAT"] != digest([]byte("validated ip")) || c.Before["GeoIP.DAT"] != digest([]byte("same")) {
		t.Fatal("incorrect candidate content hashes")
	}
	liveAfter, err := hashGeoFiles(t.Context(), live)
	if err != nil || !maps.Equal(c.Before, liveAfter) {
		t.Fatalf("validation modified running resources: %v", err)
	}
}

func TestGeoCandidateRejectsCorruptDownloadAndUnsafeLinks(t *testing.T) {
	a, _ := installedApp(t)
	m, _, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	bin, _, live := a.paths()
	cfg := []byte("rules: [MATCH,DIRECT]\n")
	m.GeoSources, err = mihomo.GeoSources(cfg)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(live, "country.mmdb"), []byte("old corrupt file"))
	a.Validate = func(context.Context, string, string, string) error { return nil }
	c, err := a.prepareGeoCandidate(t.Context(), bin, m, cfg)
	if err != nil {
		t.Fatalf("unused corrupt MMDB blocked validation: %v", err)
	}
	if _, kept := c.After["country.mmdb"]; kept {
		t.Fatal("corrupt mmdb retained in candidate")
	}
	c.Close()
	a.Validate = func(_ context.Context, _, _, data string) error {
		assertMissing(t, filepath.Join(data, "country.mmdb"))
		write(t, filepath.Join(data, "geoip.metadb"), []byte("bad remote database"))
		return nil
	}
	if _, err := a.prepareGeoCandidate(t.Context(), bin, m, cfg); !errors.Is(err, errInvalidGeoMMDB) {
		t.Fatalf("should reject invalid MMDB despite successful -t: %v", err)
	}
	if string(read(t, filepath.Join(live, "country.mmdb"))) != "old corrupt file" {
		t.Fatal("live file was modified")
	}
	if err := os.Symlink(filepath.Join(live, "country.mmdb"), filepath.Join(live, "asset")); err != nil {
		t.Fatal(err)
	}
	a.Validate = func(context.Context, string, string, string) error { t.Fatal("followed an unsafe link"); return nil }
	if _, err := a.prepareGeoCandidate(t.Context(), bin, m, cfg); err == nil {
		t.Fatal("accepted absolute link into running directory")
	}
	if err := os.Symlink("country.mmdb", filepath.Join(live, "ASN.mmdb")); err != nil {
		t.Fatal(err)
	}
	if _, err := hashGeoFiles(t.Context(), live); err == nil {
		t.Fatal("accepted GEO symlink")
	}
}

func TestOrphanedGeoCandidateCleanup(t *testing.T) {
	a, _ := installedApp(t)
	m, _, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	bin, _, _ := a.paths()
	c, err := a.prepareGeoCandidate(t.Context(), bin, m, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(a.Home, geoStagePrefix+"foreign")
	if err := os.Mkdir(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.locked(t.Context(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, c.stage)
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("removed unrelated directory")
	}
}
