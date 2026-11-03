package mihomo

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/downloadnet"
)

func TestDownloadChecksBeforePublishAndKeepsExistingFiles(t *testing.T) {
	release, archive := releaseFixture(t, "v1.2.3")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "mihomo")
	cfg := downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: server.URL}
	var events []Progress
	if err := DownloadRelease(t.Context(), dest, release, cfg, func(p Progress) { events = append(events, p) }); err != nil {
		t.Fatal(err)
	}
	var stages []string
	for _, p := range events {
		if !slices.Contains(stages, p.Stage) {
			stages = append(stages, p.Stage)
		}
		if strings.Contains(p.Detail, server.URL) {
			t.Fatal("progress exposed source URL")
		}
	}
	if !slices.Equal(stages, []string{"query", "download", "extract", "verify", "done"}) {
		t.Fatalf("incorrect progress stages: %v", stages)
	}
	final := events[len(events)-1]
	if final.Downloaded != release.Size || final.Total != release.Size {
		t.Fatalf("incorrect measured transfer size: %+v", final)
	}
	if !slices.ContainsFunc(events, func(p Progress) bool {
		return p.Stage == "download" && p.Downloaded == release.Size && p.BytesPerSecond > 0
	}) {
		t.Fatal("missing actual transfer rate")
	}
	info, err := os.Stat(dest)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("installed permissions=%v err=%v", info, err)
	}
	if version, err := BinaryVersion(t.Context(), dest); err != nil || version != release.Version {
		t.Fatalf("installed version=%q err=%v", version, err)
	}
	original, _ := os.ReadFile(dest)
	if err := DownloadRelease(t.Context(), dest, release, cfg, nil); err == nil {
		t.Fatal("silently replaced existing binary")
	}
	actual, _ := os.ReadFile(dest)
	if !bytes.Equal(original, actual) {
		t.Fatal("existing binary changed")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatalf("staging files leaked: %v", files)
	}
}

func TestDownloadRejectsInvalidBytesAndWrongExecutablesWithoutPublishing(t *testing.T) {
	_, archive := releaseFixture(t, "v1.2.3")
	_, wrongVersion := releaseFixture(t, "v9.9.9")
	corrupt := bytes.Clone(archive)
	corrupt[len(corrupt)-8] ^= 1
	gzipBytes := func(raw string) []byte {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = io.WriteString(gz, raw)
		_ = gz.Close()
		return buf.Bytes()
	}
	for _, test := range []struct {
		name         string
		media        string
		body         []byte
		expectedSize int64
		length       int64
		want         string
	}{
		{"html", "text/html", []byte("private-download-body"), 0, -1, "HTML"},
		{"json", "application/json", []byte("private-download-body"), 0, -1, "JSON"},
		{"not-gzip", "application/octet-stream", []byte("private-download-body"), 0, -1, "archive is invalid"},
		{"bad-crc", "application/gzip", corrupt, 0, -1, "cannot extract"},
		{"truncated-gzip", "application/gzip", archive[:len(archive)-4], 0, -1, "cannot extract"},
		{"empty-gzip", "application/gzip", gzipBytes(""), 0, -1, "cannot extract"},
		{"wrong-version", "application/gzip", wrongVersion, 0, -1, "does not match"},
		{"execution-failure", "application/gzip", gzipBytes("#!/bin/sh\necho private-executable-output >&2\nexit 1\n"), 0, -1, "cannot execute"},
		{"advertised-oversize", "application/gzip", archive, 0, MaxArchiveSize + 1, "exceeds 64 MiB"},
		{"changed-length", "application/gzip", archive, int64(len(archive) + 1), int64(len(archive)), "probed release size"},
		{"short-unknown-response", "application/gzip", archive, int64(len(archive) + 1), -1, "probed release size"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
				resp := fixtureResponse(r, 200, test.media, test.body)
				resp.ContentLength = test.length
				return resp, nil
			})}
			dir := t.TempDir()
			var stages []string
			err := transfer(t.Context(), filepath.Join(dir, "mihomo"), "https://fixture.invalid/core", test.expectedSize, "v1.2.3", client, func(p Progress) { stages = append(stages, p.Stage) })
			if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), "private-") {
				t.Fatalf("invalid payload accepted or error leaked: %v", err)
			}
			if slices.Contains(stages, "done") || slices.Contains(stages, "checksum") {
				t.Fatalf("invalid progress: %v", stages)
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatalf("failure left files: %v", files)
			}
		})
	}
}

func TestDownloadUnknownTotalReportsActualBytes(t *testing.T) {
	_, archive := releaseFixture(t, "v1.2.3")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.(http.Flusher).Flush()
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "mihomo")
	var final Progress
	err := transfer(t.Context(), dest, server.URL, 0, "v1.2.3", server.Client(), func(p Progress) { final = p })
	if err != nil {
		t.Fatal(err)
	}
	if final.Total != 0 || final.Downloaded != int64(len(archive)) || final.Stage != "done" {
		t.Fatalf("incorrect unknown-length progress %+v", final)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestDownloadBoundsActualCompressedAndExtractedBytes(t *testing.T) {
	t.Run("compressed-stream", func(t *testing.T) {
		client := &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
			resp := fixtureResponse(r, 200, "application/gzip", nil)
			resp.ContentLength = -1
			resp.Body = io.NopCloser(io.LimitReader(zeroReader{}, MaxArchiveSize+100))
			return resp, nil
		})}
		dir := t.TempDir()
		err := transfer(t.Context(), filepath.Join(dir, "mihomo"), "https://fixture.invalid/core", 0, "v1.2.3", client, nil)
		if err == nil || !strings.Contains(err.Error(), "exceeds 64 MiB") {
			t.Fatalf("compressed stream was not bounded: %v", err)
		}
		if files, _ := os.ReadDir(dir); len(files) != 0 {
			t.Fatalf("oversize stream leaked files: %v", files)
		}
	})
	t.Run("extracted-stream", func(t *testing.T) {
		var archive bytes.Buffer
		gz := gzip.NewWriter(&archive)
		_, _ = io.Copy(gz, io.LimitReader(zeroReader{}, (128<<20)+1))
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
			return fixtureResponse(r, 200, "application/gzip", archive.Bytes()), nil
		})}
		dir := t.TempDir()
		err := transfer(t.Context(), filepath.Join(dir, "mihomo"), "https://fixture.invalid/core", int64(archive.Len()), "v1.2.3", client, nil)
		if err == nil || !strings.Contains(err.Error(), "exceeds 128 MiB") {
			t.Fatalf("extracted stream was not bounded: %v", err)
		}
		if files, _ := os.ReadDir(dir); len(files) != 0 {
			t.Fatalf("oversize extraction leaked files: %v", files)
		}
	})
}

func TestDownloadDoesNotReplaceDestinationCreatedDuringVerification(t *testing.T) {
	_, archive := releaseFixture(t, "v1.2.3")
	client := &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
		return fixtureResponse(r, 200, "application/gzip", archive), nil
	})}
	dir := t.TempDir()
	dest := filepath.Join(dir, "mihomo")
	err := transfer(t.Context(), dest, "https://fixture.invalid/core", 0, "v1.2.3", client, func(p Progress) {
		if p.Stage == "verify" {
			if err := os.WriteFile(dest, []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err == nil {
		t.Fatal("replaced concurrently created destination")
	}
	if actual, _ := os.ReadFile(dest); string(actual) != "existing" {
		t.Fatal("concurrent destination changed")
	}
	if files, _ := os.ReadDir(dir); len(files) != 1 {
		t.Fatalf("commit failure leaked staging: %v", files)
	}
}

// Signal after the client starts consuming the response, so cancellation occurs
// deterministically after the staging archive has been created.
type downloadReadSignal struct {
	io.ReadCloser
	started chan struct{}
	once    sync.Once
}

func (r *downloadReadSignal) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.once.Do(func() { close(r.started) })
	}
	return n, err
}

type downloadTransportFunc func(*http.Request) (*http.Response, error)

func (f downloadTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDownloadCancellationBeforeAndDuringTransferRemovesStaging(t *testing.T) {
	_, archive := releaseFixture(t, "v1.2.3")
	budget, budgetCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer budgetCancel()
	ctx, cancel := context.WithCancel(budget)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive[:len(archive)/2])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	started := make(chan struct{})
	transport := server.Client().Transport
	client := &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := transport.RoundTrip(r)
		if err == nil {
			resp.Body = &downloadReadSignal{ReadCloser: resp.Body, started: started}
		}
		return resp, err
	})}
	dir := t.TempDir()
	dest := filepath.Join(dir, "mihomo")
	done := make(chan error, 1)
	go func() { done <- transfer(ctx, dest, server.URL, 0, "v1.2.3", client, nil) }()
	select {
	case <-started:
	case <-budget.Done():
		t.Fatal("download never consumed the fixture response")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 || !strings.HasPrefix(files[0].Name(), ".mihomo-download-") {
		t.Fatalf("transfer did not stage an archive: files=%v err=%v", files, err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("transfer cancellation lost: %v", err)
		}
	case <-budget.Done():
		t.Fatal("cancelled download did not finish")
	}
	if err := transfer(ctx, dest, server.URL, 0, "v1.2.3", client, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("early cancellation lost: %v", err)
	}
	files, err = os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("cancelled transfer leaked staging: files=%v err=%v", files, err)
	}
}
