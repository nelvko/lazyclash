package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This opt-in test starts an independent unprivileged core, never a systemd
// service. It uses only synthetic local inputs and does not download anything.
func TestMihomoIntegration(t *testing.T) {
	bin := integrationBinary(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	version, err := BinaryVersion(ctx, bin)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("testing standalone Mihomo %s on %s/%s", version, runtime.GOOS, runtime.GOARCH)

	reserve := func() (net.Listener, int) {
		t.Helper()
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal("cannot reserve a local test port")
		}
		t.Cleanup(func() { _ = listener.Close() })
		return listener, listener.Addr().(*net.TCPAddr).Port
	}
	apiReservation, apiPort := reserve()
	mixedReservation, mixedPort := reserve()
	home := t.TempDir()
	write := func(name string, raw []byte) string {
		t.Helper()
		path := filepath.Join(home, name+".yaml")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal("cannot write private integration fixture")
		}
		return path
	}
	controller := net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort))
	const secret = "synthetic-integration-secret"
	source := []byte(`mode: rule
proxies:
  - {name: fixture-node, type: socks5, server: 127.0.0.1, port: 1}
proxy-groups:
  - {name: PROXY, type: select, proxies: [DIRECT, REJECT, fixture-node]}
dns:
  enable: true
  listen: 0.0.0.0:53
  nameserver: [127.0.0.1]
  proxy-server-nameserver: [127.0.0.1]
rules: ["MATCH,PROXY"]
`)
	opts := BuildOptions{Base: source, Controller: controller, Secret: secret, MixedPort: mixedPort}
	full, err := Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Overrides = []byte("prepend-rules: [\"DOMAIN,integration.invalid,REJECT\"]\n")
	patched, err := Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	defaultConfig, err := Default(controller, secret, mixedPort)
	if err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]string)
	for _, fixture := range []struct {
		name string
		raw  []byte
	}{{"full", full}, {"patched", patched}, {"default", defaultConfig}} {
		paths[fixture.name] = write(fixture.name, fixture.raw)
		// Both ports are deliberately still occupied: -t checks configuration
		// semantics, not whether listeners can actually bind at startup.
		if err := Validate(ctx, bin, paths[fixture.name], home); err != nil {
			t.Fatalf("real core rejected %s fixture: %v", fixture.name, err)
		}
	}
	invalidPath := write("invalid", []byte("rules: [\"MATCH,missing-group\"]\n"))
	if err := Validate(ctx, bin, invalidPath, home); err == nil {
		t.Fatal("real core accepted an unresolved rule target")
	}
	_ = apiReservation.Close()
	_ = mixedReservation.Close()

	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	httpClient := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	client := Client{BaseURL: "http://" + controller, Secret: secret, HTTP: httpClient}
	stop := startIntegrationCore(t, ctx, bin, home, paths["full"], client)
	configuration, err := client.Configuration(ctx)
	if err != nil || configuration.Mode != "rule" || configuration.TUN {
		t.Fatal("real core did not expose the expected runtime settings")
	}
	mixed, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(mixedPort)), time.Second)
	if err != nil {
		t.Fatal("real mixed listener is not ready")
	}
	_ = mixed.Close()
	if err := client.Select(ctx, "PROXY", "REJECT"); err != nil {
		t.Fatal(err)
	}
	reload := func(path string, wantStatus int) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"path": path})
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, client.BaseURL+"/configs?force=true", bytes.NewReader(body))
		if err != nil {
			t.Fatal("cannot construct integration reload request")
		}
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("Content-Type", "application/json")
		response, err := httpClient.Do(req)
		if err != nil {
			t.Fatal("real core reload request failed")
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		if response.StatusCode != wantStatus {
			t.Fatalf("real reload returned HTTP %d, want %d (body withheld)", response.StatusCode, wantStatus)
		}
	}
	assertPatchedRules := func() {
		t.Helper()
		rules, err := client.Rules(ctx)
		if err != nil || len(rules) != 2 || rules[0].Payload != "integration.invalid" || rules[0].Proxy != "REJECT" || rules[1].Type != "Match" || rules[1].Proxy != "PROXY" {
			t.Fatal("real core rules differ from the accepted configuration")
		}
	}
	reload(paths["patched"], http.StatusNoContent)
	assertPatchedRules()
	assertIntegrationSelection(t, ctx, client, "REJECT")
	reload(invalidPath, http.StatusBadRequest)
	assertPatchedRules()
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, patched, 0600); err != nil {
		t.Fatal("cannot write external-path fixture")
	}
	reload(outside, http.StatusBadRequest)
	assertPatchedRules()
	stop()
	startIntegrationCore(t, ctx, bin, home, paths["full"], client)
	assertIntegrationSelection(t, ctx, client, "REJECT")
}

func integrationBinary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("LAZYCLASH_TEST_MIHOMO")
	if bin == "" {
		t.Skip("set LAZYCLASH_TEST_MIHOMO to an absolute Mihomo executable path")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("standalone core integration currently runs on Linux and macOS")
	}
	if !filepath.IsAbs(bin) {
		t.Fatal("LAZYCLASH_TEST_MIHOMO must be an absolute executable path")
	}
	// Do not inherit controller overrides, unsafe paths or post-up/down scripts
	// from a developer's own installation. t.Setenv restores them after the test.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "CLASH_") || key == "SAFE_PATHS" || key == "SKIP_SAFE_PATH_CHECK" {
			t.Setenv(key, "")
		}
	}
	return bin
}

func TestMihomoProviderCacheIntegration(t *testing.T) {
	bin := integrationBinary(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if _, err := BinaryVersion(ctx, bin); err != nil {
		t.Fatal(err)
	}
	var requestsA, requestsB atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node := "from-a"
		if r.URL.Path == "/b" {
			node = "from-b"
			requestsB.Add(1)
		} else {
			requestsA.Add(1)
		}
		_, _ = fmt.Fprintf(w, "proxies: [{name: %s, type: socks5, server: 127.0.0.1, port: 1}]\n", node)
	}))
	t.Cleanup(server.Close)
	reserve := func() (net.Listener, int) {
		t.Helper()
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal("cannot reserve local provider integration ports")
		}
		t.Cleanup(func() { _ = listener.Close() })
		return listener, listener.Addr().(*net.TCPAddr).Port
	}
	apiReservation, apiPort := reserve()
	mixedReservation, mixedPort := reserve()
	controller := net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort))
	const secret = "synthetic-provider-secret"
	home := t.TempDir()
	configs := map[string]string{}
	caches := map[string]string{}
	for _, suffix := range []string{"a", "b"} {
		opts := BuildOptions{
			Base:       []byte(fmt.Sprintf("proxy-providers: {remote: {type: http, url: '%s/%s', path: ./shared.yaml, interval: 3600}}\nproxy-groups: [{name: PROXY, type: select, proxies: [DIRECT], use: [remote]}]\nrules: ['MATCH,PROXY']\n", server.URL, suffix)),
			Controller: controller, Secret: secret, MixedPort: mixedPort,
		}
		raw, err := Build(opts)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeConfig(raw)
		if err != nil {
			t.Fatal(err)
		}
		caches[suffix] = providerPath(t, decoded, "proxy-providers", "remote")
		configs[suffix] = filepath.Join(home, suffix+".yaml")
		if err := os.WriteFile(configs[suffix], raw, 0600); err != nil {
			t.Fatal("cannot write provider integration fixture")
		}
		if err := Validate(ctx, bin, configs[suffix], home); err != nil {
			t.Fatal(err)
		}
	}
	if caches["a"] == caches["b"] {
		t.Fatal("different provider sources still share their legacy cache path")
	}
	_ = apiReservation.Close()
	_ = mixedReservation.Close()
	client := Client{BaseURL: "http://" + controller, Secret: secret}
	waitNode := func(want string) {
		t.Helper()
		readyCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		for {
			groups, err := client.Groups(readyCtx)
			if err == nil {
				for _, group := range groups {
					if group.Name != "PROXY" {
						continue
					}
					for _, node := range group.All {
						if node == want {
							return
						}
					}
				}
			}
			select {
			case <-readyCtx.Done():
				t.Fatal("core did not load the expected source's provider nodes")
			case <-time.After(25 * time.Millisecond):
			}
		}
	}
	stop := startIntegrationCore(t, ctx, bin, home, configs["a"], client)
	waitNode("from-a")
	stop()
	if requestsA.Load() == 0 {
		t.Fatal("first provider was never downloaded")
	}
	if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(caches["a"]))); err != nil {
		t.Fatal("core did not create its HTTP provider cache directory and file")
	}
	startIntegrationCore(t, ctx, bin, home, configs["b"], client)
	waitNode("from-b")
	if requestsB.Load() == 0 {
		t.Fatal("new provider reused old cached nodes without contacting its source")
	}
}

func startIntegrationCore(t *testing.T, ctx context.Context, bin, home, config string, client Client) func() {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin, "-d", home, "-f", config)
	// Never print core output: it may contain credentials even on test failure.
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal("cannot start independent integration core")
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("integration core did not exit after kill")
			}
		}
	}
	t.Cleanup(stop)
	readyCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for {
		if err := client.Ping(readyCtx); err == nil {
			return stop
		}
		select {
		case <-done:
			t.Fatal("integration core exited before API readiness (output withheld)")
		case <-readyCtx.Done():
			t.Fatal("integration core API readiness timed out (output withheld)")
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func assertIntegrationSelection(t *testing.T, ctx context.Context, client Client, want string) {
	t.Helper()
	groups, err := client.Groups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if group.Name == "PROXY" && group.Now == want {
			return
		}
	}
	t.Fatal("real core did not retain the selected node")
}
