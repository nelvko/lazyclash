package mihomo

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This opt-in test starts an isolated unprivileged process with synthetic local
// sources. It never contacts systemd, enables TUN or reads a user's profiles.
func TestMihomoProxyProviderAPIIntegration(t *testing.T) {
	bin := integrationBinary(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if _, err := BinaryVersion(ctx, bin); err != nil {
		t.Fatal("cannot inspect integration core executable")
	}
	var revision, requests atomic.Int32
	const sourceSecret = "synthetic-provider-source-secret"
	const sourcePath = "/private-fixture-provider"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != sourcePath || r.Header.Get("Authorization") != "Bearer "+sourceSecret {
			t.Error("provider download differed from the synthetic source request")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		requests.Add(1)
		w.Header().Set("Content-Type", "application/yaml")
		if revision.Load() == 0 {
			_, _ = fmt.Fprint(w, "proxies: [{name: old-node, type: socks5, server: 127.0.0.1, port: 1}]\n")
			return
		}
		_, _ = fmt.Fprint(w, "proxies: [{name: new-node-a, type: socks5, server: 127.0.0.1, port: 1}, {name: new-node-b, type: socks5, server: 127.0.0.1, port: 2}]\n")
	}))
	t.Cleanup(server.Close)
	reserve := func() (net.Listener, int) {
		t.Helper()
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal("cannot reserve local proxy provider test port")
		}
		t.Cleanup(func() { _ = listener.Close() })
		return listener, listener.Addr().(*net.TCPAddr).Port
	}
	apiReservation, apiPort := reserve()
	mixedReservation, mixedPort := reserve()
	controller := net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort))
	const controllerSecret = "synthetic-provider-controller-secret"
	home := t.TempDir()
	source := fmt.Sprintf(`proxies:
  - {name: static-node, type: socks5, server: 127.0.0.1, port: 1}
proxy-providers:
  remote:
    type: http
    url: '%s%s'
    interval: 3600
    header: {Authorization: ['Bearer %s']}
    health-check: {enable: false}
  local-inline:
    type: inline
    payload: [{name: inline-node, type: socks5, server: 127.0.0.1, port: 1}]
    health-check: {enable: false}
proxy-groups:
  - {name: PROXY, type: select, proxies: [DIRECT, static-node], use: [remote, local-inline]}
rules: ['MATCH,PROXY']
`, server.URL, sourcePath, sourceSecret)
	raw, err := Build(BuildOptions{Base: []byte(source), Controller: controller, Secret: controllerSecret, MixedPort: mixedPort})
	if err != nil {
		t.Fatal("cannot build isolated proxy provider fixture")
	}
	config := filepath.Join(home, "provider-api.yaml")
	if err := os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal("cannot write isolated proxy provider fixture")
	}
	if err := Validate(ctx, bin, config, home); err != nil {
		t.Fatal("real core rejected isolated proxy provider fixture (output withheld)")
	}
	_ = apiReservation.Close()
	_ = mixedReservation.Close()
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	client := Client{BaseURL: "http://" + controller, Secret: controllerSecret, HTTP: &http.Client{Transport: transport, Timeout: 2 * time.Second}}
	startIntegrationCore(t, ctx, bin, home, config, client)

	// Provider initialization can finish after controller readiness. Observe the
	// provider metadata and group membership together before testing mutation.
	// Mihomo keeps provider-owned node details out of the /proxies directory;
	// its group All fields still expose the effective provider membership.
	observe := func(count int, wantNodes, removedNodes []string) []ProxyProvider {
		t.Helper()
		readyCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		var providerOK, proxiesOK, groupsOK, remoteOK, groupOK bool
		var missing string
		for {
			providers, err := client.ProxyProviders(readyCtx)
			proxies, proxiesErr := client.Proxies(readyCtx)
			groups, groupsErr := client.Groups(readyCtx)
			providerOK, proxiesOK, groupsOK = err == nil, proxiesErr == nil, groupsErr == nil
			if err == nil && proxiesErr == nil && groupsErr == nil {
				remoteReady, groupReady := false, false
				for _, provider := range providers {
					if provider.Name == "remote" {
						remoteReady = provider.VehicleType == "HTTP" && provider.ProxyCount == count && !provider.UpdatedAt.IsZero()
					}
				}
				for _, group := range groups {
					if group.Name != "PROXY" {
						continue
					}
					groupReady = true
					missing = ""
					for _, name := range wantNodes {
						inProxyGroup := slices.Contains(proxies["PROXY"].All, name)
						if !inProxyGroup || !slices.Contains(group.All, name) {
							missing += fmt.Sprintf("%s(inProxyGroup=%t,inGroup=%t) ", name, inProxyGroup, slices.Contains(group.All, name))
						}
						groupReady = groupReady && inProxyGroup && slices.Contains(group.All, name)
					}
					for _, name := range removedNodes {
						groupReady = groupReady && !slices.Contains(proxies["PROXY"].All, name) && !slices.Contains(group.All, name)
					}
				}
				remoteOK, groupOK = remoteReady, groupReady
				if remoteReady && groupReady {
					return providers
				}
			}
			select {
			case <-readyCtx.Done():
				t.Fatalf("real proxy provider API or live proxy groups did not converge: metadata=%t proxies=%t groups=%t remote=%t members=%t missing=%s (response withheld)", providerOK, proxiesOK, groupsOK, remoteOK, groupOK, missing)
			case <-time.After(25 * time.Millisecond):
			}
		}
	}
	initial := observe(1, []string{"old-node", "inline-node", "static-node"}, nil)
	var before time.Time
	compatible, inline := false, false
	for _, provider := range initial {
		if provider.Name == "remote" {
			before = provider.UpdatedAt
		}
		compatible = compatible || provider.VehicleType == "Compatible"
		inline = inline || (provider.Name == "local-inline" && provider.VehicleType == "Inline" && provider.ProxyCount == 1)
	}
	if !compatible || !inline {
		t.Fatal("real core did not expose Compatible and Inline provider metadata")
	}
	assertPrivate := func(providers []ProxyProvider) {
		t.Helper()
		metadata, err := json.Marshal(providers)
		if err != nil {
			t.Fatal("cannot encode provider metadata")
		}
		for _, private := range []string{server.URL, sourcePath, sourceSecret, controllerSecret, `"proxies"`, `"url"`, `"server"`, `"header"`, `"payload"`} {
			if strings.Contains(string(metadata), private) {
				t.Fatal("private proxy provider details escaped metadata boundary")
			}
		}
	}
	assertPrivate(initial)
	beforeRequests := requests.Load()
	revision.Store(1)
	if err := client.UpdateProxyProvider(ctx, "remote"); err != nil {
		t.Fatal("real proxy provider update failed (response withheld)")
	}
	fresh := observe(2, []string{"new-node-a", "new-node-b", "inline-node", "static-node"}, []string{"old-node"})
	assertPrivate(fresh)
	if requests.Load() <= beforeRequests {
		t.Fatal("explicit provider update did not download the changed source")
	}
	for _, provider := range fresh {
		if provider.Name == "remote" && !provider.UpdatedAt.After(before) {
			t.Fatal("real proxy provider update did not advance its timestamp")
		}
	}
	settings, err := client.Configuration(ctx)
	if err != nil || settings.TUN {
		t.Fatal("isolated provider test did not retain TUN disabled")
	}
}
