package mihomo

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/downloadnet"
)

func releaseFixture(t *testing.T, version string) (Release, []byte, []byte) {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	_, _ = gz.Write([]byte("#!/bin/sh\necho 'Mihomo Meta " + version + " fixture'\n"))
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive.Bytes())
	asset, _ := releaseAsset(version, "arm64")
	r := Release{Version: version, Asset: asset, URL: releaseFiles + version + "/" + asset, SHA256: hex.EncodeToString(sum[:]), ChecksumSource: releaseAPI + "latest#asset-digest:" + asset, Platform: "linux/arm64", Size: int64(archive.Len())}
	raw, _ := json.Marshal(map[string]any{"tag_name": r.Version, "draft": false, "prerelease": false, "assets": []map[string]any{{"name": r.Asset, "browser_download_url": r.URL, "digest": "sha256:" + r.SHA256, "size": r.Size}}})
	return r, archive.Bytes(), raw
}
func fixtureResponse(r *http.Request, status int, media string, body []byte) *http.Response {
	headers := make(http.Header)
	if media != "" {
		headers.Set("Content-Type", media)
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Header: headers, Request: r, ContentLength: int64(len(body))}
}

func TestResolveLatestAndSpecificUseSelectedMetadataRoute(t *testing.T) {
	expected, _, raw := releaseFixture(t, "v1.2.3")
	for _, cfg := range []downloadnet.Config{{}, {Method: downloadnet.MethodEnvironment}, {Method: downloadnet.MethodAccelerated, Prefix: "https://accelerator.invalid/"}} {
		var requests []string
		resolver := Resolver{Arch: "arm64", HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
			requests = append(requests, r.URL.String())
			if r.Header.Get("Accept") != "application/vnd.github+json" {
				t.Error("missing GitHub JSON Accept header")
			}
			return fixtureResponse(r, 200, "application/json", raw), nil
		})}}
		got, err := resolver.Resolve(t.Context(), "latest", cfg)
		if err != nil || got != expected {
			t.Fatalf("release=%+v err=%v expected=%+v", got, err, expected)
		}
		got, err = resolver.Resolve(t.Context(), "1.2.3", cfg)
		if err != nil || got.Version != "v1.2.3" || got.ChecksumSource != releaseAPI+"tags/v1.2.3#asset-digest:"+got.Asset {
			t.Fatalf("specific release=%+v err=%v", got, err)
		}
		latest, _ := cfg.URL(releaseAPI + "latest")
		specific, _ := cfg.URL(releaseAPI + "tags/v1.2.3")
		if len(requests) != 2 || requests[0] != latest || requests[1] != specific {
			t.Fatalf("metadata ignored selected route: %v", requests)
		}
	}
}

func TestResolveAMD64PrefersV1AndAcceptsCompatibleFallback(t *testing.T) {
	for _, test := range []struct {
		name   string
		assets []string
		want   string
	}{
		{"prefer-v1", []string{"compatible", "v1"}, "v1"},
		{"old-release", []string{"compatible"}, "compatible"},
		{"v1-only", []string{"v1"}, "v1"},
		{"never-v3-default", []string{""}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			var assets []map[string]any
			for _, variant := range test.assets {
				prefix := "mihomo-linux-amd64-"
				if variant != "" {
					prefix += variant + "-"
				}
				name := prefix + "v1.2.3.gz"
				assets = append(assets, map[string]any{"name": name, "browser_download_url": releaseFiles + "v1.2.3/" + name, "digest": "sha256:" + strings.Repeat("a", 64), "size": 123})
			}
			raw, _ := json.Marshal(map[string]any{"tag_name": "v1.2.3", "assets": assets})
			resolver := Resolver{Arch: "amd64", HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
				return fixtureResponse(r, 200, "application/json", raw), nil
			})}}
			got, err := resolver.Resolve(t.Context(), "v1.2.3", downloadnet.Config{})
			if test.want == "" {
				if err == nil {
					t.Fatal("unsafe plain amd64 fallback accepted")
				}
				return
			}
			want := "mihomo-linux-amd64-" + test.want + "-v1.2.3.gz"
			if err != nil || got.Asset != want {
				t.Fatalf("asset=%q err=%v, want %q", got.Asset, err, want)
			}
			if err := validRelease(got); err != nil {
				t.Fatalf("resolver produced invalid release: %v", err)
			}
		})
	}
}

func TestResolveAMD64SelectsAvailableBuildWithinDeviceCapabilities(t *testing.T) {
	for _, test := range []struct {
		name   string
		level  int
		assets []string
		want   string
	}{
		{"v3-device", 3, []string{"compatible", "v1", "v2", "v3"}, "v3"},
		{"v2-device", 2, []string{"compatible", "v1", "v2", "v3"}, "v2"},
		{"v1-device", 1, []string{"compatible", "v1", "v2", "v3"}, "v1"},
		{"v3-missing", 3, []string{"v1", "v2"}, "v2"},
		{"v2-missing", 2, []string{"v1", "v3"}, "v1"},
		{"old-v3-default", 3, []string{"", "compatible"}, ""},
		{"old-v2-compatible", 2, []string{"", "compatible"}, "compatible"},
		{"never-promote-v1", 1, []string{"v2", "v3", ""}, "missing"},
		{"never-promote-v2", 2, []string{"v3", ""}, "missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var assets []map[string]any
			for _, variant := range test.assets {
				name := "mihomo-linux-amd64-"
				if variant != "" {
					name += variant + "-"
				}
				name += "v1.2.3.gz"
				assets = append(assets, map[string]any{"name": name, "browser_download_url": releaseFiles + "v1.2.3/" + name, "digest": "sha256:" + strings.Repeat("a", 64), "size": 123})
			}
			raw, _ := json.Marshal(map[string]any{"tag_name": "v1.2.3", "assets": assets})
			resolver := Resolver{Arch: "amd64", AMD64Level: test.level, HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
				return fixtureResponse(r, 200, "application/json", raw), nil
			})}}
			got, err := resolver.Resolve(t.Context(), "latest", downloadnet.Config{})
			if test.want == "missing" {
				if err == nil {
					t.Fatalf("selected unsupported build %s", got.Asset)
				}
				return
			}
			want := "mihomo-linux-amd64-"
			if test.want != "" {
				want += test.want + "-"
			}
			want += "v1.2.3.gz"
			if err != nil || got.Asset != want {
				t.Fatalf("asset=%q, err=%v; want %s", got.Asset, err, want)
			}
			if err := validRelease(got); err != nil {
				t.Fatalf("selected official build is invalid: %v", err)
			}
		})
	}
}

func TestReleaseRejectsUntrustedMetadataAndMissingDigest(t *testing.T) {
	expected, _, raw := releaseFixture(t, "v1.2.3")
	cases := map[string][]byte{
		"html":            []byte("<html>private-api-token</html>"),
		"missing-digest":  bytes.Replace(raw, []byte("sha256:"+expected.SHA256), []byte(""), 1),
		"malicious-asset": bytes.Replace(raw, []byte(expected.URL), []byte("https://evil.invalid/private-token"), 1),
		"prerelease":      bytes.Replace(raw, []byte(`"prerelease":false`), []byte(`"prerelease":true`), 1),
		"draft":           bytes.Replace(raw, []byte(`"draft":false`), []byte(`"draft":true`), 1),
		"no-asset":        bytes.ReplaceAll(raw, []byte(expected.Asset), []byte("mihomo-other.gz")),
		"bad-sha":         bytes.Replace(raw, []byte(expected.SHA256), []byte("not-a-hash"), 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resolver := Resolver{Arch: "arm64", HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
				return fixtureResponse(r, 200, "application/json", body), nil
			})}}
			if _, err := resolver.Resolve(t.Context(), "latest", downloadnet.Config{}); err == nil || strings.Contains(err.Error(), "private-") {
				t.Fatalf("untrusted metadata accepted or leaked: %v", err)
			}
		})
	}
	resolver := Resolver{Arch: "arm64", HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) { return fixtureResponse(r, 200, "text/html", raw), nil })}}
	if _, err := resolver.Resolve(t.Context(), "latest", downloadnet.Config{}); err == nil {
		t.Fatal("HTML content type accepted")
	}
	resolver.HTTP.Transport = downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
		return fixtureResponse(r, 403, "application/json", []byte("private-api-error")), nil
	})
	if _, err := resolver.Resolve(t.Context(), "latest", downloadnet.Config{}); err == nil || !strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "private-") {
		t.Fatalf("unsafe status error: %v", err)
	}
}

type limitedProbeBody struct {
	bytes.Reader
	read *int
}

func (b *limitedProbeBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	*b.read += n
	return n, err
}
func (b *limitedProbeBody) Close() error { return nil }

func TestFileProbeIsIndependentBoundedAndUsesSelectedRoute(t *testing.T) {
	release, _, _ := releaseFixture(t, "v1.2.3")
	body := append([]byte{0x1f, 0x8b}, bytes.Repeat([]byte("x"), 10000)...)
	consumed := 0
	resolver := Resolver{HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Range") != "bytes=0-4095" {
			t.Error("file test did not request bounded range")
		}
		if r.URL.String() != "https://accelerator.invalid/"+release.URL {
			t.Errorf("unexpected probe URL %s", r.URL.String())
		}
		response := fixtureResponse(r, 206, "application/gzip", nil)
		response.Body = &limitedProbeBody{Reader: *bytes.NewReader(body), read: &consumed}
		return response, nil
	})}}
	if err := resolver.TestFile(t.Context(), release, downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: "https://accelerator.invalid/"}); err != nil {
		t.Fatal(err)
	}
	if consumed != 4096 {
		t.Fatalf("probe read %d bytes", consumed)
	}
	for _, test := range []struct {
		code  int
		media string
		body  []byte
	}{{200, "text/html", []byte("private-mirror-token")}, {200, "application/json", []byte("{}")}, {200, "application/octet-stream", []byte("invalid archive")}, {502, "application/octet-stream", body}} {
		resolver.HTTP.Transport = downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
			return fixtureResponse(r, test.code, test.media, test.body), nil
		})
		if err := resolver.TestFile(t.Context(), release, downloadnet.Config{}); err == nil || strings.Contains(err.Error(), "private-") {
			t.Fatalf("unsafe probe error: %v", err)
		}
	}
}

func TestDownloadReleaseReportsMeasuredProgressAndRejectsTampering(t *testing.T) {
	release, archive, _ := releaseFixture(t, "v1.2.3")
	for _, tamper := range []bool{false, true} {
		t.Run(map[bool]string{false: "verified", true: "tampered"}[tamper], func(t *testing.T) {
			payload := append([]byte(nil), archive...)
			if tamper {
				payload[len(payload)-1] ^= 1
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/gzip")
				_, _ = w.Write(payload)
			}))
			defer server.Close()
			dir := t.TempDir()
			dest := filepath.Join(dir, "mihomo")
			var events []Progress
			err := DownloadRelease(t.Context(), dest, release, downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: server.URL}, func(p Progress) { events = append(events, p) })
			if tamper {
				if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
					t.Fatalf("tampered source accepted: %v", err)
				}
				files, _ := os.ReadDir(dir)
				if len(files) != 0 {
					t.Fatal("tampering left partial files")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for _, p := range events {
				seen[p.Stage] = true
				if strings.Contains(p.Detail, server.URL) {
					t.Fatal("progress exposed file URL")
				}
			}
			for _, stage := range []string{"query", "download", "checksum", "extract", "verify", "done"} {
				if !seen[stage] {
					t.Fatalf("missing progress stage %s: %+v", stage, events)
				}
			}
			final := events[len(events)-1]
			if final.Downloaded != release.Size || final.Total != release.Size {
				t.Fatalf("incorrect actual transfer size %+v", final)
			}
			speed := false
			for _, p := range events {
				if p.Stage == "download" && p.Downloaded == release.Size && p.BytesPerSecond > 0 {
					speed = true
				}
			}
			if !speed {
				t.Fatal("no measured transfer rate")
			}
		})
	}
}

func TestReleaseQueryCancellationAndFileStatusAreSeparate(t *testing.T) {
	release, _, _ := releaseFixture(t, "v1.2.3")
	resolver := Resolver{HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) { return nil, context.Canceled })}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := resolver.Resolve(ctx, "latest", downloadnet.Config{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("query cancellation lost: %v", err)
	}
	resolver.HTTP.Transport = downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
		return fixtureResponse(r, 200, "application/gzip", []byte{0x1f, 0x8b}), nil
	})
	if err := resolver.TestFile(t.Context(), release, downloadnet.Config{}); err != nil {
		t.Fatal("file probe depended on a prior failed version query", err)
	}
}

func TestMetadataRedirectDoesNotSilentlyChangeSelectedSource(t *testing.T) {
	_, _, raw := releaseFixture(t, "v1.2.3")
	requests := 0
	resolver := Resolver{Arch: "arm64", HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			resp := fixtureResponse(r, 302, "application/json", nil)
			resp.Header.Set("Location", "https://untrusted.invalid/private-token")
			return resp, nil
		}
		return fixtureResponse(r, 200, "application/json", raw), nil
	})}}
	if _, err := resolver.Resolve(t.Context(), "latest", downloadnet.Config{}); err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("metadata redirect accepted or leaked: %v", err)
	}
	if requests != 1 {
		t.Fatal("followed redirect away from digest authority")
	}
}

func TestDownloadUnknownTotalStillReportsActualBytes(t *testing.T) {
	_, archive, _ := releaseFixture(t, "v1.2.3")
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.(http.Flusher).Flush()
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "mihomo")
	var final Progress
	err := transfer(t.Context(), dest, artifact{server.URL, hex.EncodeToString(sum[:])}, 0, "", server.Client(), func(p Progress) { final = p })
	if err != nil {
		t.Fatal(err)
	}
	if final.Total != 0 || final.Downloaded != int64(len(archive)) || final.Stage != "done" {
		t.Fatalf("incorrect unknown-length progress %+v", final)
	}
}

func TestAcceleratedInstallRoutesVersionMetadataAndFileThroughOnePrefix(t *testing.T) {
	release, archive, metadata := releaseFixture(t, "v1.2.3")
	requestPaths := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPaths <- r.URL.Path
		switch r.URL.Path {
		case "/proxy/" + stableVersionURL:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, release.Version+"\n")
		case "/proxy/" + releaseAPI + "tags/v1.2.3":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(metadata)
		case "/proxy/" + release.URL:
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(archive)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	// Acceleration must remain usable with an invalid inherited proxy.
	t.Setenv("HTTPS_PROXY", "http://private-user:private-pass@127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://private-user:private-pass@127.0.0.1:1")
	cfg := downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: server.URL + "/proxy///"}
	resolver := Resolver{Arch: "arm64"}
	version, err := resolver.LatestVersion(t.Context(), ChannelStable, cfg)
	if err != nil || version != release.Version {
		t.Fatalf("version=%q err=%v", version, err)
	}
	resolved, err := resolver.Resolve(t.Context(), version, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.TestFile(t.Context(), resolved, cfg); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "mihomo")
	if err := DownloadRelease(t.Context(), dest, resolved, cfg, nil); err != nil {
		t.Fatal(err)
	}
	var requests []string
	for len(requestPaths) > 0 {
		requests = append(requests, <-requestPaths)
	}
	if len(requests) != 4 || requests[0] != "/proxy/"+stableVersionURL || requests[1] != "/proxy/"+releaseAPI+"tags/v1.2.3" || requests[2] != "/proxy/"+release.URL || requests[3] != requests[2] {
		t.Fatalf("installation did not use one consistent route: %v", requests)
	}
}

func TestAcceleratorMetadataFailureHasNoDirectFallback(t *testing.T) {
	release, archive, _ := releaseFixture(t, "v1.2.3")
	var requests []string
	resolver := Resolver{Arch: "arm64", HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.String())
		if strings.HasPrefix(r.URL.String(), "https://accelerator.invalid/"+releaseAPI) {
			return fixtureResponse(r, http.StatusBadGateway, "text/plain", []byte("private-error")), nil
		}
		if r.URL.String() != "https://accelerator.invalid/"+release.URL {
			t.Errorf("attempted direct fallback: %s", r.URL.String())
		}
		return fixtureResponse(r, 200, "application/gzip", archive), nil
	})}}
	cfg := downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: "https://accelerator.invalid/"}
	if _, err := resolver.Resolve(t.Context(), "v1.2.3", cfg); err == nil || !strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "private-") {
		t.Fatalf("unsupported metadata query accepted or exposed response: %v", err)
	}
	if len(requests) != 1 {
		t.Fatal("metadata failure triggered fallback", requests)
	}
	if err := resolver.TestFile(t.Context(), release, cfg); err != nil {
		t.Fatal("file compatibility should be independent of metadata support", err)
	}
}
