package mihomo

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/downloadnet"
)

func releaseFixture(t *testing.T, version string) (Release, []byte) {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	_, _ = gz.Write([]byte("#!/bin/sh\necho 'Mihomo Meta " + version + " fixture'\n"))
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	asset, _ := releaseAsset(version, "arm64")
	r := Release{Version: version, Asset: asset, URL: releaseFiles + version + "/" + asset, Platform: "linux/arm64", Size: int64(archive.Len())}
	return r, archive.Bytes()
}
func fixtureResponse(r *http.Request, status int, media string, body []byte) *http.Response {
	headers := make(http.Header)
	if media != "" {
		headers.Set("Content-Type", media)
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Header: headers, Request: r, ContentLength: int64(len(body))}
}

func TestResolveLatestAndSpecificUseOnlySelectedVersionAndFileRoutes(t *testing.T) {
	expected, archive := releaseFixture(t, "v1.2.3")
	for _, cfg := range []downloadnet.Config{{}, {Method: downloadnet.MethodEnvironment}, {Method: downloadnet.MethodAccelerated, Prefix: "https://accelerator.invalid/"}} {
		t.Run(cfg.Method, func(t *testing.T) {
			var requests []string
			versionURL, _ := cfg.URL(stableVersionURL)
			fileURL, _ := cfg.URL(expected.URL)
			resolver := Resolver{Arch: "arm64", HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.URL.String())
				switch r.URL.String() {
				case versionURL:
					return fixtureResponse(r, 200, "text/plain", []byte("v1.2.3\n")), nil
				case fileURL:
					if r.Header.Get("Range") != "bytes=0-4095" || r.Header.Get("Accept-Encoding") != "identity" {
						t.Error("file availability did not use a bounded unencoded probe")
					}
					return fixtureResponse(r, 200, "application/gzip", archive), nil
				default:
					t.Fatalf("unexpected endpoint, including any Release API: %s", r.URL)
					return nil, errors.New("unexpected request")
				}
			})}}
			for _, choice := range []string{"latest", "", "1.2.3", "v1.2.3"} {
				got, err := resolver.Resolve(t.Context(), choice, cfg)
				if err != nil || got != expected {
					t.Fatalf("choice=%q release=%+v err=%v expected=%+v", choice, got, err, expected)
				}
			}
			want := []string{versionURL, fileURL, versionURL, fileURL, fileURL, fileURL}
			if !slices.Equal(requests, want) {
				t.Fatalf("release resolution did not fix latest to one tag: %v", requests)
			}
		})
	}
}

func TestResolveRejectsNonStableVersionsBeforeFileRequest(t *testing.T) {
	for _, choice := range []string{"v1.2", "v1.2.3-alpha", "v1.2.3/evil", "v1.2.3\n", "LATEST"} {
		resolver := Resolver{Arch: "arm64", HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
			t.Fatalf("invalid version caused a request: %s", r.URL)
			return nil, errors.New("unexpected request")
		})}}
		if _, err := resolver.Resolve(t.Context(), choice, downloadnet.Config{}); err == nil {
			t.Fatalf("accepted non-stable choice %q", choice)
		}
	}
	resolver := Resolver{Arch: "arm64", HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != stableVersionURL {
			t.Fatalf("non-stable latest caused file request: %s", r.URL)
		}
		return fixtureResponse(r, 200, "text/plain", []byte("v2.0\n")), nil
	})}}
	if _, err := resolver.Resolve(t.Context(), "latest", downloadnet.Config{}); err == nil {
		t.Fatal("accepted latest without a stable release tag")
	}
}

func TestResolveAMD64UsesSafeOrderedFallbacks(t *testing.T) {
	_, archive := releaseFixture(t, "v1.2.3")
	for _, test := range []struct {
		name      string
		level     int
		available []string
		attempts  []string
	}{
		{"explicit-arch-defaults-v1", 0, []string{"v3", "v2", "v1", "compatible"}, []string{"v1"}},
		{"v3-device", 3, []string{"v3", "v2", "v1"}, []string{"v3"}},
		{"v2-device", 2, []string{"v3", "v2", "v1"}, []string{"v2"}},
		{"old-v3-plain", 3, []string{"", "compatible"}, []string{"v3", ""}},
		{"v3-falls-to-v2", 3, []string{"v2", "v1"}, []string{"v3", "", "v2"}},
		{"v2-falls-to-v1", 2, []string{"v1"}, []string{"v2", "v1"}},
		{"v1-compatible", 1, []string{"compatible", "v3"}, []string{"v1", "compatible"}},
		{"never-promote-v1", 1, []string{"v2", "v3", ""}, []string{"v1", "compatible"}},
		{"never-promote-v2", 2, []string{"v3", ""}, []string{"v2", "v1", "compatible"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var attempts []string
			resolver := Resolver{Arch: "amd64", AMD64Level: test.level, HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
				name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(r.URL.Path), "mihomo-linux-amd64-"), "v1.2.3.gz")
				variant := strings.TrimSuffix(name, "-")
				attempts = append(attempts, variant)
				if slices.Contains(test.available, variant) {
					return fixtureResponse(r, 200, "application/gzip", archive), nil
				}
				// Both absent-resource statuses permit the next safe candidate.
				status := http.StatusNotFound
				if len(attempts)%2 == 0 {
					status = http.StatusGone
				}
				return fixtureResponse(r, status, "text/plain", nil), nil
			})}}
			got, err := resolver.Resolve(t.Context(), "1.2.3", downloadnet.Config{})
			last := test.attempts[len(test.attempts)-1]
			if slices.Contains(test.available, last) {
				want := "mihomo-linux-amd64-"
				if last != "" {
					want += last + "-"
				}
				want += "v1.2.3.gz"
				if err != nil || got.Asset != want {
					t.Fatalf("release=%+v err=%v", got, err)
				}
				if err := validRelease(got); err != nil {
					t.Fatalf("resolver produced invalid release: %v", err)
				}
			} else if err == nil {
				t.Fatalf("unsupported build selected: %+v", got)
			}
			if !slices.Equal(attempts, test.attempts) {
				t.Fatalf("candidate order=%v want=%v", attempts, test.attempts)
			}
		})
	}
}

func TestResolveDoesNotFallbackOnNetworkOrInvalidResponses(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		media  string
		body   []byte
		err    error
	}{
		{"network", 0, "", nil, errors.New("private-network-error")},
		{"forbidden", 403, "text/plain", []byte("private-body"), nil},
		{"rate-limited", 429, "text/plain", []byte("private-body"), nil},
		{"server-error", 502, "text/plain", nil, nil},
		{"html", 200, "text/html", []byte("private-body"), nil},
		{"json", 200, "application/problem+json", []byte("private-body"), nil},
		{"not-gzip", 200, "application/octet-stream", []byte("private-body"), nil},
		{"incomplete-header", 200, "application/gzip", []byte{0x1f, 0x8b}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			cfg := downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: "https://accelerator.invalid/"}
			resolver := Resolver{Arch: "amd64", AMD64Level: 3, HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if !strings.HasPrefix(r.URL.String(), cfg.Prefix+releaseFiles) {
					t.Fatalf("changed route or called API: %s", r.URL)
				}
				if test.err != nil {
					return nil, test.err
				}
				return fixtureResponse(r, test.status, test.media, test.body), nil
			})}}
			if _, err := resolver.Resolve(t.Context(), "v1.2.3", cfg); err == nil || strings.Contains(err.Error(), "private-") {
				t.Fatalf("invalid response accepted or leaked: %v", err)
			}
			if requests != 1 {
				t.Fatalf("failure changed build or route: %d requests", requests)
			}
		})
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

func TestFileProbeIsIndependentAndReadsAtMost4KiB(t *testing.T) {
	release, archive := releaseFixture(t, "v1.2.3")
	body := append(archive, bytes.Repeat([]byte("x"), 10000)...)
	release.Size = int64(len(body))
	consumed := 0
	resolver := Resolver{HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Range") != "bytes=0-4095" || r.URL.String() != "https://accelerator.invalid/"+release.URL {
			t.Fatalf("unexpected probe: %s %+v", r.URL, r.Header)
		}
		response := fixtureResponse(r, 200, "application/gzip", nil)
		response.ContentLength = int64(len(body))
		response.Body = &limitedProbeBody{Reader: *bytes.NewReader(body), read: &consumed}
		return response, nil
	})}}
	if err := resolver.TestFile(t.Context(), release, downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: "https://accelerator.invalid/"}); err != nil {
		t.Fatal(err)
	}
	if consumed != 4096 {
		t.Fatalf("server ignored Range; probe consumed %d bytes", consumed)
	}
}

func TestProbeObservesHTTPSizeSemanticsAndRejectsInvalidRanges(t *testing.T) {
	release, archive := releaseFixture(t, "v1.2.3")
	length := int64(len(archive))
	for _, test := range []struct {
		name         string
		status       int
		length       int64
		contentRange string
		want         int64
		invalid      bool
	}{
		{"full-response", 200, length, "", length, false},
		{"unknown-full-length", 200, -1, "", 0, false},
		{"partial-known-total", 206, length, "bytes 0-" + strconv.FormatInt(length-1, 10) + "/10000", 10000, false},
		{"partial-unknown-total", 206, length, "bytes 0-" + strconv.FormatInt(length-1, 10) + "/*", 0, false},
		{"missing-range", 206, length, "", 0, true},
		{"chunked-partial-truncated", 206, -1, "bytes 0-4095/10000", 0, true},
		{"wrong-start", 206, length, "bytes 1-80/10000", 0, true},
		{"range-too-large", 206, -1, "bytes 0-4096/10000", 0, true},
		{"range-length-mismatch", 206, length, "bytes 0-4095/10000", 0, true},
		{"impossible-total", 206, -1, "bytes 0-4095/1000", 0, true},
		{"overflow-total", 206, -1, "bytes 0-4095/99999999999999999999999", 0, true},
		{"oversize-full", 200, MaxArchiveSize + 1, "", 0, true},
		{"oversize-partial", 206, -1, "bytes 0-4095/67108865", 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := Resolver{Arch: "arm64", HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
				resp := fixtureResponse(r, test.status, "application/gzip", archive)
				resp.ContentLength = test.length
				resp.Header.Set("Content-Range", test.contentRange)
				return resp, nil
			})}}
			got, err := resolver.Resolve(t.Context(), release.Version, downloadnet.Config{})
			if test.invalid {
				if err == nil {
					t.Fatal("invalid size/range accepted")
				}
			} else if err != nil || got.Size != test.want {
				t.Fatalf("size=%d err=%v want=%d", got.Size, err, test.want)
			}
		})
	}
}

func TestValidReleaseRequiresFixedOfficialPlatformAndBoundedSize(t *testing.T) {
	release, _ := releaseFixture(t, "v1.2.3")
	for _, mutate := range []func(*Release){
		func(r *Release) { r.Version = "latest" },
		func(r *Release) { r.Version = "v1.2.3-alpha" },
		func(r *Release) { r.URL = "https://evil.invalid/private-token" },
		func(r *Release) { r.Asset = "mihomo-linux-amd64-v1-v1.2.3.gz" },
		func(r *Release) { r.Platform = "darwin/arm64" },
		func(r *Release) { r.Size = -1 },
		func(r *Release) { r.Size = MaxArchiveSize + 1 },
	} {
		bad := release
		mutate(&bad)
		if err := validRelease(bad); err == nil || strings.Contains(err.Error(), "private-") {
			t.Fatalf("invalid release accepted or leaked: %+v %v", bad, err)
		}
	}
	release.Size = 0
	if err := validRelease(release); err != nil {
		t.Fatalf("unknown transfer size rejected: %v", err)
	}
}

func TestReleaseCancellationAndFileProbeDoNotRequirePriorQuery(t *testing.T) {
	release, archive := releaseFixture(t, "v1.2.3")
	resolver := Resolver{Arch: "arm64", HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) { return nil, context.Canceled })}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := resolver.Resolve(ctx, "latest", downloadnet.Config{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("query cancellation lost: %v", err)
	}
	if _, err := resolver.Resolve(ctx, release.Version, downloadnet.Config{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("probe cancellation lost: %v", err)
	}
	resolver.HTTP.Transport = downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
		return fixtureResponse(r, 200, "application/gzip", archive), nil
	})
	if err := resolver.TestFile(t.Context(), release, downloadnet.Config{}); err != nil {
		t.Fatal("file probe depended on a prior failed version query", err)
	}
}

func TestAcceleratedInstallRoutesVersionAndAllFileRequestsThroughOnePrefix(t *testing.T) {
	release, archive := releaseFixture(t, "v1.2.3")
	requestPaths := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPaths <- r.URL.Path
		switch r.URL.Path {
		case "/proxy/" + stableVersionURL:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, release.Version+"\n")
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
	resolved, err := resolver.Resolve(t.Context(), "latest", cfg)
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
	want := []string{"/proxy/" + stableVersionURL, "/proxy/" + release.URL, "/proxy/" + release.URL, "/proxy/" + release.URL}
	if !slices.Equal(requests, want) {
		t.Fatalf("installation did not use only version text + fixed file: %v", requests)
	}
}
