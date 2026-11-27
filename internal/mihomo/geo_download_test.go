package mihomo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/testfixture"
)

func TestDownloadDefaultGeoVerifiesBeforePublishing(t *testing.T) {
	for _, fixture := range []struct {
		kind, asset, local string
		data               []byte
	}{{"mmdb", "geoip.metadb", "geoip.metadb", testfixture.CountryMMDB()}, {"geosite", "geosite.dat", "GeoSite.dat", testfixture.GeoSite()}} {
		t.Run(fixture.kind, func(t *testing.T) {
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.URL.Path)
				if strings.HasSuffix(r.URL.Path, ".sha256sum") {
					fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(fixture.data), fixture.asset)
				} else {
					w.Write(fixture.data)
				}
			}))
			defer server.Close()
			dest := filepath.Join(t.TempDir(), fixture.local)
			cfg := downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: server.URL}
			var last Progress
			if err := DownloadDefaultGeo(t.Context(), fixture.kind, dest, cfg, func(p Progress) {
				if p.Stage != "geodata" || p.Detail != fixture.local {
					t.Fatalf("unexpected progress: %+v", p)
				}
				last = p
			}); err != nil {
				t.Fatal(err)
			}
			if last.Downloaded != int64(len(fixture.data)) || last.Total != last.Downloaded {
				t.Fatalf("incorrect progress: %+v", last)
			}
			path := "/" + geoRelease + fixture.asset
			if !slices.Equal(requests, []string{path + ".sha256sum", path}) {
				t.Fatalf("wrong accelerator routing: %q", requests)
			}
			data, err := os.ReadFile(dest)
			if err != nil || !bytes.Equal(data, fixture.data) {
				t.Fatalf("published bytes: %v", err)
			}
			if err := ValidateDefaultGeo(t.Context(), fixture.kind, dest); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDownloadDefaultGeoFailureKeepsDestination(t *testing.T) {
	valid := testfixture.GeoSite()
	for _, tc := range []struct {
		name   string
		sum    string
		data   []byte
		cancel bool
	}{
		{"wrong asset name", fmt.Sprintf("%x  geoip.dat\n", sha256.Sum256(valid)), valid, false},
		{"hash mismatch", fmt.Sprintf("%x  geosite.dat\n", sha256.Sum256([]byte("other"))), valid, false},
		{"invalid protobuf", fmt.Sprintf("%x  geosite.dat\n", sha256.Sum256([]byte("INVALID"))), []byte("INVALID"), false},
		{"canceled transfer", fmt.Sprintf("%x  geosite.dat\n", sha256.Sum256(valid)), valid, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".sha256sum") {
					fmt.Fprint(w, tc.sum)
				} else if tc.cancel {
					w.Write(tc.data[:1])
					w.(http.Flusher).Flush()
					cancel()
					<-r.Context().Done()
				} else {
					w.Write(tc.data)
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			dest := filepath.Join(dir, "GeoSite.dat")
			if err := os.WriteFile(dest, []byte("previous"), 0600); err != nil {
				t.Fatal(err)
			}
			err := DownloadDefaultGeo(ctx, "geosite", dest, downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: server.URL}, nil)
			if err == nil || tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("expected failure, got %v", err)
			}
			left, err := os.ReadDir(dir)
			if err != nil || len(left) != 1 {
				t.Fatalf("staging files leaked: %v %v", left, err)
			}
			raw, err := os.ReadFile(dest)
			if err != nil || string(raw) != "previous" {
				t.Fatal("changed the previous file")
			}
		})
	}
}

func TestValidateDefaultGeoRejectsWrongDatFamilyAndTruncation(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte("\x0a\x0e\x0a\x02CN\x12\x08\x0a\x04\x01\x02\x03\x04\x10\x20"), // GeoIPList with an IPv4 CIDR
		append(testfixture.GeoSite(), 0x0a, 0x80),
		bytes.ReplaceAll(testfixture.GeoSite(), []byte("CN"), []byte("US")),
	} {
		path := filepath.Join(t.TempDir(), "GeoSite.dat")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := ValidateDefaultGeo(t.Context(), "geosite", path); err == nil {
			t.Fatal("accepted wrong, truncated, or unusable database")
		}
	}
}
