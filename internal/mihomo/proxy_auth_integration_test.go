package mihomo

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
	opts := BuildOptions{
		Base:       []byte("rules: ['MATCH,DIRECT']\nauthentication: ['inherited:discarded']\nskip-auth-prefixes: ['127.0.0.0/8']\n"),
		Controller: controller.String(), Secret: "test-controller", MixedPort: mixed.Port,
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
	stop := startIntegrationCore(t, ctx, bin, home, config, Client{BaseURL: "http://" + controller.String(), Secret: opts.Secret})
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
}
