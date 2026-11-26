package mihomo

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestProxyPortRangeAndPlatformValidation(t *testing.T) {
	for _, port := range []int{0, 1, 80, 1023, 65535} {
		if err := (ListenerOptions{}).Validate("127.0.0.1:19097", "fixture", port); err != nil {
			t.Fatalf("port %d: %v", port, err)
		}
	}
	for _, platform := range []struct {
		os               string
		redirect, tproxy bool
	}{
		{"windows", false, false}, {"darwin", true, false}, {"linux", true, true},
		{"android", true, true}, {"", true, true},
	} {
		t.Run(platform.os, func(t *testing.T) {
			const controller = "127.0.0.1:19097"
			opts := ListenerOptions{CoreOS: platform.os, RedirPort: 81, TProxyPort: 82}
			if err := opts.Validate(controller, "fixture", 80); err != nil {
				t.Fatal(err)
			}
			endpoints := opts.Endpoints(controller, 80)
			if slices.Contains(endpoints, ListenerEndpoint{"tcp", "127.0.0.1:81"}) != platform.redirect ||
				slices.Contains(endpoints, ListenerEndpoint{"udp", "127.0.0.1:82"}) != platform.tproxy {
				t.Fatal("wrong platform sockets", endpoints)
			}
			opts.RedirPort, opts.TProxyPort = 0, 80
			if err := opts.Validate(controller, "fixture", 80); (err != nil) != platform.tproxy {
				t.Fatal("unexpected TProxy conflict result", err)
			}
			opts.RedirPort, opts.TProxyPort = -1, 0
			if err := opts.Validate(controller, "fixture", 80); (err != nil) != platform.redirect {
				t.Fatal("unexpected Redirect range result", err)
			}
		})
	}
}

func TestManagedControllerAddressesAndAdditionalEndpoints(t *testing.T) {
	if target, err := ControllerTarget("[::]:19097"); err != nil || target != "[::1]:19097" {
		t.Fatal(target, err)
	}
	if target, err := ControllerTarget("0.0.0.0:19097"); err != nil || target != "127.0.0.1:19097" {
		t.Fatal(target, err)
	}
	if _, err := ControllerTarget("192.0.2.99:19097"); err == nil {
		t.Fatal("would send API credentials to remote controller")
	}
	if target, err := ResolveController(t.Context(), "localhost:19097"); err != nil || target != "127.0.0.1:19097" {
		t.Fatal(target, err)
	}
	for _, opt := range []ListenerOptions{
		{HTTPPort: 19097}, {RedirPort: -1}, {SOCKSPort: 12001, TProxyPort: 12001},
		{HTTPSAddress: "127.0.0.1:19997"}, {DoHPath: "//private/invalid"}, {AllowedOrigins: "https://user:password@dashboard.invalid"},
	} {
		if err := opt.Validate("127.0.0.1:19097", "private", 0); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}

func TestMihomoAdditionalListenerIntegration(t *testing.T) {
	bin := integrationBinary(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	home, err := os.MkdirTemp("/tmp", "lc-access-") // Unix socket paths are length-limited on both supported platforms.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	certServer := httptest.NewTLSServer(http.NotFoundHandler())
	pair := certServer.TLS.Certificates[0]
	certServer.Close()
	key, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]})
	if err = os.WriteFile(filepath.Join(home, "api.crt"), cert, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(home, "api.key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	var reservations []net.Listener
	reserve := func() int {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		reservations = append(reservations, l)
		return l.Addr().(*net.TCPAddr).Port
	}
	controller := fmt.Sprintf("127.0.0.1:%d", reserve())
	opts := ListenerOptions{HTTPSAddress: fmt.Sprintf("127.0.0.1:%d", reserve()), HTTPPort: reserve(), SOCKSPort: reserve(), CertificatePath: "api.crt", PrivateKeyPath: "api.key", UnixSocket: "control.sock", AllowedOrigins: "https://panel.example", DoHPath: "/dns-query"}
	const secret = "synthetic-access-fixture"
	config, err := Build(BuildOptions{Base: []byte(DefaultProfile), Controller: controller, Secret: secret, Listeners: opts})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "config.yaml")
	if err = os.WriteFile(path, config, 0600); err != nil {
		t.Fatal(err)
	}
	if err = Validate(ctx, bin, path, home); err != nil {
		t.Fatal(err)
	}
	for _, l := range reservations {
		_ = l.Close()
	}
	httpClient := Client{BaseURL: "http://" + controller, Secret: secret}
	startIntegrationCore(t, ctx, bin, home, path, httpClient)
	if err := httpClient.ConfirmProxyPorts(ctx, map[string]int{"mixed-port": 0, "port": opts.HTTPPort, "socks-port": opts.SOCKSPort, "redir-port": 0, "tproxy-port": 0}); err != nil {
		t.Fatal("core listener status differs from applied configuration", err)
	}
	if bad := (Client{BaseURL: "http://" + controller, Secret: "old"}).Ping(ctx); bad == nil {
		t.Fatal("controller ignored the API credential")
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(cert)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	defer transport.CloseIdleConnections()
	httpsClient := Client{BaseURL: "https://" + opts.HTTPSAddress, Secret: secret, HTTP: &http.Client{Transport: transport}}
	if err := httpsClient.Ping(ctx); err != nil {
		t.Fatal("TLS API did not accept the managed certificate and credential", err)
	}
	for _, port := range []int{opts.HTTPPort, opts.SOCKSPort} {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err != nil {
			t.Fatal("proxy listener unavailable", err)
		}
		_ = conn.Close()
	}
	conn, err := net.DialTimeout("unix", filepath.Join(home, "control.sock"), time.Second)
	if err != nil {
		t.Fatal("Unix listener unavailable", err)
	}
	_ = conn.Close()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+controller+"/version", nil)
	req.Header.Set("Origin", "https://panel.example")
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") != "https://panel.example" {
		t.Fatal("CORS origin missing")
	}
}
