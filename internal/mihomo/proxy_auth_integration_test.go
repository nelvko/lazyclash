package mihomo

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMihomoProxyAuthIntegration(t *testing.T) {
	bin := integrationBinary(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	origin := httptest.NewServer(handler)
	defer origin.Close()
	secureOrigin := httptest.NewTLSServer(handler)
	defer secureOrigin.Close()
	var listeners []net.Listener
	reserve := func() *net.TCPAddr {
		ln, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, ln)
		t.Cleanup(func() { _ = ln.Close() })
		return ln.Addr().(*net.TCPAddr)
	}
	controller, mixed, httpPort, socks := reserve(), reserve(), reserve(), reserve()
	noBypass := ""
	opts := BuildOptions{
		AccessOverrides: AccessOverrides{SkipAuthPrefixes: &noBypass},
		Base:            []byte("rules: ['MATCH,DIRECT']\nauthentication: ['inherited:discarded']\nskip-auth-prefixes: ['127.0.0.0/8']\n"),
		Controller:      controller.String(), Secret: "test-controller", MixedPort: mixed.Port,
		Listeners:  ListenerOptions{HTTPPort: httpPort.Port, SOCKSPort: socks.Port},
		ProxyUsers: []string{"test-one:first:password", "test-two:second-password"},
	}
	raw, err := Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	config := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, ln := range listeners {
		_ = ln.Close()
	}
	client := Client{BaseURL: "http://" + controller.String(), Secret: opts.Secret}
	stop := startIntegrationCore(t, ctx, bin, home, config, client)
	defer stop()
	request := func(proxy *url.URL, address string) int {
		tr := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true, TLSClientConfig: secureOrigin.Client().Transport.(*http.Transport).TLSClientConfig}
		defer tr.CloseIdleConnections()
		c := http.Client{Transport: tr, Timeout: 2 * time.Second}
		response, err := c.Get(address)
		if err != nil {
			return 0 // SOCKS negotiation or HTTP CONNECT can fail before a response.
		}
		_ = response.Body.Close()
		return response.StatusCode
	}
	// The control API can answer while Mihomo is still starting its outbound
	// tunnel. Verify the same private origin is reachable before testing denial.
	readyProxy := &url.URL{Scheme: "http", Host: httpPort.String(), User: url.UserPassword("test-one", "first:password")}
	for deadline := time.Now().Add(3 * time.Second); request(readyProxy, origin.URL) != http.StatusNoContent; {
		if time.Now().After(deadline) {
			t.Fatal("independent proxy did not become ready to forward requests")
		}
		time.Sleep(25 * time.Millisecond)
	}
	for _, endpoint := range []string{"http://" + httpPort.String(), "socks5://" + socks.String(), "http://" + mixed.String(), "socks5://" + mixed.String()} {
		for i, creds := range []*url.Userinfo{nil, url.UserPassword("inherited", "discarded"), url.UserPassword("test-one", "first:password"), url.UserPassword("test-two", "second-password")} {
			proxy, _ := url.Parse(endpoint)
			proxy.User = creds
			for _, address := range []string{origin.URL, secureOrigin.URL} {
				status := request(proxy, address)
				if i >= 2 && status != http.StatusNoContent {
					t.Fatalf("%s failed to authenticate fixture %d for %s, status %d", endpoint, i, address, status)
				}
				if i < 2 && status != 0 && status != http.StatusForbidden && status != http.StatusProxyAuthRequired {
					t.Fatalf("%s accepted unauthorized fixture %d for %s, status %d", endpoint, i, address, status)
				}
			}
		}
	}

	readConfig := func() map[string]json.RawMessage {
		t.Helper()
		raw, err := client.request(ctx, http.MethodGet, "/configs", nil)
		if err != nil {
			t.Fatal(err)
		}
		var config map[string]json.RawMessage
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatal(err)
		}
		return config
	}

	// Verify the runtime PATCH semantics against real connections, including
	// loopback clients when allow-lan remains disabled.
	patch := func(fields map[string][]string) {
		t.Helper()
		body, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.request(ctx, http.MethodPatch, "/configs", body); err != nil {
			t.Fatal(err)
		}
		config := readConfig()
		for key, want := range fields {
			var got []string
			if err := json.Unmarshal(config[key], &got); err != nil {
				t.Fatalf("invalid runtime %s list", key)
			}
			prefixes, err := NormalizePrefixes(strings.Join(got, "\n"))
			if err != nil || prefixes != strings.Join(want, "\n") {
				t.Fatalf("runtime %s differs from applied access rules", key)
			}
		}
	}
	anonymous := &url.URL{Scheme: "http", Host: httpPort.String()}
	patch(map[string][]string{"skip-auth-prefixes": {"127.0.0.0/8"}, "lan-allowed-ips": {"127.0.0.0/8"}})
	if status := request(anonymous, origin.URL); status != http.StatusNoContent {
		t.Fatalf("matching exemption did not bypass authentication, status %d", status)
	}
	patch(map[string][]string{"lan-disallowed-ips": {"127.0.0.1/32"}})
	if status := request(readyProxy, origin.URL); status == http.StatusNoContent {
		t.Fatal("deny did not take priority over allow and exemption")
	}
	patch(map[string][]string{"lan-disallowed-ips": {}, "lan-allowed-ips": {}})
	if status := request(readyProxy, origin.URL); status == http.StatusNoContent {
		t.Fatal("an explicitly empty allow list admitted a client")
	}
	patch(map[string][]string{"lan-allowed-ips": {"0.0.0.0/0", "::/0"}, "skip-auth-prefixes": {}})
	if status := request(readyProxy, origin.URL); status != http.StatusNoContent {
		t.Fatalf("restoring all source networks did not admit an authenticated client, status %d", status)
	}
	if status := request(anonymous, origin.URL); status == http.StatusNoContent {
		t.Fatal("an explicitly empty skip list bypassed required credentials")
	}

	// Apply a subscription with its own accounts while LAN is disabled.
	opts.ProxyUsers = nil
	raw, err = Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	current := readConfig()
	var names []string
	if string(current["allow-lan"]) != "false" || json.Unmarshal(current["authentication"], &names) != nil || !slices.Equal(names, []string{"inherited"}) {
		t.Fatal("runtime did not apply the subscription account with LAN disabled")
	}
	inherited := &url.URL{Scheme: "http", Host: httpPort.String(), User: url.UserPassword("inherited", "discarded")}
	if status := request(inherited, origin.URL); status != http.StatusNoContent {
		t.Fatalf("loopback subscription credentials did not authenticate: HTTP %d", status)
	}
	if status := request(anonymous, origin.URL); status == http.StatusNoContent {
		t.Fatal("loopback subscription credentials were optional")
	}
}
