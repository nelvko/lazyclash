//go:build linux

package mihomo

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nelvko/lazyclash/internal/downloadnet"
)

func TestRootMihomoExecutionRequiresDedicatedIdentity(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to verify refusing a root execution")
	}
	for _, ctx := range []context.Context{
		context.Background(),
		WithIdentity(context.Background(), 0, 65534),
		WithIdentity(context.Background(), 65534, 0),
		WithIdentity(context.Background(), -1, 65534),
	} {
		if _, err := executableCommand(ctx, "/usr/bin/id", "-u"); err == nil {
			t.Fatal("root execution without a dedicated UID/GID was accepted")
		}
	}
}

func TestRootMihomoExecutionDropsIdentityAndSupplementaryGroups(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to verify a real UID/GID transition")
	}
	ctx := WithIdentity(context.Background(), 65534, 65534)
	for _, test := range []struct {
		flag string
		want string
	}{
		{"-u", "65534"},
		{"-g", "65534"},
		{"-G", "65534"},
	} {
		cmd, err := executableCommand(ctx, "/usr/bin/id", test.flag)
		if err != nil {
			t.Fatal(err)
		}
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("id %s: %v", test.flag, err)
		}
		if got := strings.TrimSpace(string(out)); got != test.want {
			t.Fatalf("id %s = %q, want %q", test.flag, got, test.want)
		}
		if len(cmd.Env) != 3 || cmd.Dir != "/" {
			t.Fatalf("root environment or working directory leaked: env=%v dir=%q", cmd.Env, cmd.Dir)
		}
	}
}

func TestRootDownloadChecksVersionAsDedicatedIdentity(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to verify downloaded executable ownership and execution")
	}
	dir, err := os.MkdirTemp("/tmp", "lazyclash-root-download-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	_, _ = gz.Write([]byte("#!/bin/sh\n" +
		"[ \"$(id -u)\" = 65534 ] && [ \"$(id -g)\" = 65534 ] && [ \"$(id -G)\" = 65534 ] || exit 1\n" +
		"[ ! -w \"$0\" ] && [ \"$HOME\" = /nonexistent ] || exit 1\n" +
		"echo 'Mihomo Meta v1.2.3 fixture'\n"))
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(archive.Bytes())
	}))
	defer server.Close()
	release, _ := releaseFixture(t, "v1.2.3")
	release.Size = int64(archive.Len())
	ctx := WithIdentity(t.Context(), 65534, 65534)
	dest := filepath.Join(dir, "mihomo")
	if err := DownloadRelease(ctx, dest, release, downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: server.URL}, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if stat.Uid != 0 || stat.Gid != 65534 || info.Mode().Perm() != 0750 {
		t.Fatalf("download ownership/mode = %d:%d %v", stat.Uid, stat.Gid, info.Mode())
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("download staging files survived: %v, %v", files, err)
	}
}
