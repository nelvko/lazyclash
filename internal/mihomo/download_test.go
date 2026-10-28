package mihomo

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
