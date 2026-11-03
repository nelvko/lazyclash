package cli

import (
	"bytes"
	"compress/gzip"
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
)

func TestInstallCLIUsesVersionTextAndReleaseFilesWithoutGitHubAPI(t *testing.T) {
	const version = "v1.20.0"
	const official = "https://github.com/MetaCubeX/mihomo/releases/"
	assetPath := "/" + official + "download/" + version + "/mihomo-linux-arm64-" + version + ".gz"
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
			t.Errorf("unexpected request outside version text and pinned asset: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, svc := installBackend(t)
	a.Resolve = (mihomo.Resolver{Arch: "arm64"}).Resolve
	a.DownloadRelease = mihomo.DownloadRelease
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
		if strings.Contains(diagnostics.String(), "SHA256") || strings.Contains(diagnostics.String(), "metadata") {
			t.Fatalf("diagnostics claim upstream digest or metadata validation: %s", diagnostics.String())
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
	mu.Unlock()
	if downloadedDuringPreflight != 0 {
		t.Fatal("dry run downloaded the complete core")
	}
	result := run("--core-version", version)
	if !strings.Contains(result, `"InstalledVersion": "`+version+`"`) || svc.installs != 1 || svc.actions != 0 {
		t.Fatalf("remote install did not publish a stopped core: %s installs=%d actions=%d", result, svc.installs, svc.actions)
	}
	mu.Lock()
	defer mu.Unlock()
	if fullDownloads != 1 || len(requests) != 4 || requests[0] != "/"+official+"latest/download/version.txt" {
		t.Fatalf("unexpected request chain: downloads=%d paths=%v", fullDownloads, requests)
	}
	for _, path := range requests[1:] {
		if path != assetPath {
			t.Fatalf("request used a mutable or API address: %s", path)
		}
	}
}
