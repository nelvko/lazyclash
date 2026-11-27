package cli

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/testfixture"
)

func TestInstallCLIUsesVersionTextAndReleaseFilesWithoutGitHubAPI(t *testing.T) {
	const version = "v1.20.0"
	const official = "https://github.com/MetaCubeX/mihomo/releases/"
	assetPath := "/" + official + "download/" + version + "/mihomo-linux-arm64-" + version + ".gz"
	geoFiles := map[string][]byte{
		"geoip.metadb": testfixture.CountryMMDB(),
		"geosite.dat":  testfixture.GeoSite(),
	}
	geoAssets := make(map[string][]byte)
	for filename, data := range geoFiles {
		path := "/https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/" + filename
		geoAssets[path] = data
		geoAssets[path+".sha256sum"] = []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(data), filename))
	}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	_, _ = fmt.Fprintf(gz, "#!/bin/sh\necho 'Mihomo Meta %s fixture'\n", version)
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []string
	fullDownloads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/" + official + "latest/download/version.txt":
			_, _ = fmt.Fprintln(w, version)
		case assetPath:
			w.Header().Set("Content-Type", "application/gzip")
			if r.Header.Get("Range") != "" {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", archive.Len()-1, archive.Len()))
				w.WriteHeader(http.StatusPartialContent)
			} else {
				mu.Lock()
				fullDownloads++
				mu.Unlock()
			}
			_, _ = w.Write(archive.Bytes())
		default:
			if data, ok := geoAssets[r.URL.Path]; ok {
				_, _ = w.Write(data)
				return
			}
			t.Errorf("unexpected release or GeoData request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, svc := installBackend(t)
	a.Resolve = (mihomo.Resolver{Arch: "arm64"}).Resolve
	a.DownloadRelease = mihomo.DownloadRelease
	a.DownloadGeo = mihomo.DownloadDefaultGeo
	a.Version = mihomo.BinaryVersion
	run := func(args ...string) string {
		t.Helper()
		cmd := newInstallCommand(func() *app.App { return a })
		var out, diagnostics bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&diagnostics)
		cmd.SetArgs(append([]string{"--download-method", "accelerated", "--acceleration-prefix", server.URL + "/"}, args...))
		if err := cmd.ExecuteContext(t.Context()); err != nil {
			t.Fatalf("install %v failed: %v\n%s", args, err, diagnostics.String())
		}
		for line := range strings.SplitSeq(diagnostics.String(), "\n") {
			if !strings.HasPrefix(line, "geo") && (strings.Contains(line, "SHA256") || strings.Contains(line, "metadata")) {
				t.Fatalf("core download claims upstream digest or metadata validation: %s", line)
			}
		}
		return out.String()
	}
	run("--dry-run")
	if svc.installs != 0 {
		t.Fatal("dry run installed a service")
	}
	if _, err := os.Lstat(filepath.Join(a.Home, "installation")); !os.IsNotExist(err) {
		t.Fatal("dry run created an installation")
	}
	mu.Lock()
	downloadedDuringPreflight := fullDownloads
	preflight := append([]string(nil), requests...)
	mu.Unlock()
	if downloadedDuringPreflight != 0 || len(preflight) != 2 || preflight[0] != "/"+official+"latest/download/version.txt" || preflight[1] != assetPath {
		t.Fatalf("unexpected dry run traffic: %v", preflight)
	}
	result := run("--core-version", version)
	if !strings.Contains(result, `"InstalledVersion": "`+version+`"`) || svc.installs != 1 || svc.starts != 0 {
		t.Fatalf("remote install did not publish a stopped core: %s installs=%d starts=%d", result, svc.installs, svc.starts)
	}
	mu.Lock()
	defer mu.Unlock()
	if fullDownloads != 1 || len(requests) != 4+len(geoAssets) {
		t.Fatalf("unexpected request chain: downloads=%d paths=%v", fullDownloads, requests)
	}
	if requests[2] != assetPath || requests[3] != assetPath {
		t.Fatalf("expected pinned asset probe and download: %v", requests)
	}
	counts := make(map[string]int)
	for _, path := range requests[4:] {
		counts[path]++
	}
	for path := range geoAssets {
		if counts[path] != 1 {
			t.Fatalf("expected one request for %s; got %d", path, counts[path])
		}
	}
	for filename, want := range geoFiles {
		if filename == "geosite.dat" {
			filename = "GeoSite.dat"
		}
		got, err := os.ReadFile(filepath.Join(a.Home, "installation", "runtime", filename))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("GeoData download %s was not published: %v", filename, err)
		}
	}
}
