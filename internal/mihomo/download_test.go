package mihomo

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDownloadVerifiesBeforeCommitAndKeepsExistingFiles(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	_, _ = gz.Write([]byte("#!/bin/sh\necho 'Mihomo Meta v1.19.33 test'\n"))
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive.Bytes())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive.Bytes()) }))
	defer server.Close()
	art := artifact{server.URL, hex.EncodeToString(sum[:])}
	dest := filepath.Join(t.TempDir(), "mihomo")
	messages := []string{}
	if err := download(t.Context(), dest, art, server.Client(), func(s string) { messages = append(messages, s) }); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("missing progress: %v", messages)
	}
	original, _ := os.ReadFile(dest)
	if err := download(t.Context(), dest, art, server.Client(), nil); err == nil {
		t.Fatal("silently replaced existing binary")
	}
	actual, _ := os.ReadFile(dest)
	if !bytes.Equal(original, actual) {
		t.Fatal("existing binary changed")
	}
	files, _ := os.ReadDir(filepath.Dir(dest))
	if len(files) != 1 {
		t.Fatalf("staging files leaked: %v", files)
	}
}

func TestChecksumMismatchAndCancellationLeaveNoInstall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("private-download-body")) }))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "mihomo")
	if err := download(t.Context(), dest, artifact{server.URL, "wrong"}, server.Client(), nil); err == nil || !strings.Contains(err.Error(), "checksum mismatch") || strings.Contains(err.Error(), "private-") {
		t.Fatalf("checksum error=%v", err)
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid download installed")
	}
	files, _ := os.ReadDir(filepath.Dir(dest))
	if len(files) != 0 {
		t.Fatal("checksum failure leaked staging")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := download(ctx, dest, artifact{server.URL, "wrong"}, server.Client(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestPinnedReleaseArtifacts(t *testing.T) {
	for _, arch := range []string{"arm64", "amd64"} {
		a, err := releaseArtifact(arch)
		if err != nil || len(a.SHA256) != 64 || !strings.Contains(a.URL, "/"+Version+"/") {
			t.Fatalf("bad artifact %+v: %v", a, err)
		}
	}
	if _, err := releaseArtifact("386"); err == nil {
		t.Fatal("unsupported architecture accepted")
	}
}

// Signal after the client starts consuming the response body, so cancellation
// deterministically occurs after its staging archive has been created.
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

func TestDownloadCancellationDuringTransferRemovesStaging(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	_, _ = gz.Write([]byte("#!/bin/sh\necho 'Mihomo Meta v1.19.33 test'\n"))
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive.Bytes())
	budget, budgetCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer budgetCancel()
	ctx, cancel := context.WithCancel(budget)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Keep a chunked response open after the first portion. No sleeps are
		// needed: the request context is the sole completion signal.
		_, _ = w.Write(archive.Bytes()[:archive.Len()/2])
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
	go func() { done <- download(ctx, dest, artifact{server.URL, hex.EncodeToString(sum[:])}, client, nil) }()
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
	if _, err := os.Lstat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled download published a binary: %v", err)
	}
	files, err = os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("cancelled transfer leaked staging: files=%v err=%v", files, err)
	}
}
