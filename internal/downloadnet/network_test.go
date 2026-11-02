package downloadnet

import (
	"context"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloadMethodsRouteEveryURLConsistently(t *testing.T) {
	for _, original := range []string{
		"https://github.com/MetaCubeX/mihomo/releases/latest/download/version.txt",
		"https://api.github.com/repos/MetaCubeX/mihomo/releases/tags/v1.2.3",
		"https://github.com/MetaCubeX/mihomo/releases/download/v1.2.3/mihomo.gz",
	} {
		for _, cfg := range []Config{{}, {Method: MethodDirect}, {Method: MethodEnvironment}, {Method: MethodAccelerated, Prefix: "  https://prefix.example/cache///  "}} {
			want := original
			if cfg.Method == MethodAccelerated {
				want = "https://prefix.example/cache/" + original
			}
			got, err := cfg.URL(original)
			if err != nil || got != want {
				t.Fatalf("method=%s got %q %v want %q", cfg.Method, got, err, want)
			}
		}
	}
	for _, cfg := range []Config{
		{Method: "unknown"},
		{Method: MethodAccelerated, Prefix: "https://secret.example?token=private-token"},
		{Method: MethodAccelerated, Prefix: "https://private-user:private-pass@example.org"},
		{Method: MethodAccelerated, Prefix: "https://example.org/private-fragment#token"},
		{Method: MethodAccelerated, Prefix: "https://example.org/{asset}"},
		{Method: MethodAccelerated, Prefix: "ftp://example.org"},
		{Method: MethodAccelerated, Prefix: "https://example.org?"},
	} {
		if err := cfg.Validate(); err == nil || strings.Contains(err.Error(), "private-") {
			t.Fatalf("unsafe validation error: %v", err)
		}
	}
	safe := RedactedURL("https://private-user:private-pass@example.org/private-token?token=private-query")
	if strings.Contains(safe, "private-") || !strings.Contains(safe, "example.org") {
		t.Fatal(safe)
	}
}

func clearProxyEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy", "REQUEST_METHOD"} {
		t.Setenv(name, "")
	}
}

func TestEnvironmentSnapshotAndNoProxyDoNotAffectDirectOrAccelerated(t *testing.T) {
	clearProxyEnvironment(t)
	t.Setenv("HTTP_PROXY", "http://first.example:8080")
	t.Setenv("NO_PROXY", ".internal.example,10.0.0.0/8,exact.example:8081")
	first, err := NewClient(Config{Method: MethodEnvironment}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer first.CloseIdleConnections()
	t.Setenv("HTTP_PROXY", "http://second.example:8080")
	second, err := NewClient(Config{Method: MethodEnvironment}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer second.CloseIdleConnections()
	req, _ := http.NewRequest(http.MethodGet, "http://public.example/file", nil)
	p1, _ := first.Transport.(*http.Transport).Proxy(req)
	p2, _ := second.Transport.(*http.Transport).Proxy(req)
	if p1.Host != "first.example:8080" || p2.Host != "second.example:8080" {
		t.Fatal("environment snapshot was cached globally")
	}
	for _, address := range []string{"http://127.0.0.1/file", "http://localhost/file", "http://service.internal.example/file", "http://10.1.2.3/file", "http://exact.example:8081/file"} {
		r, _ := http.NewRequest(http.MethodGet, address, nil)
		p, err := first.Transport.(*http.Transport).Proxy(r)
		if err != nil || p != nil {
			t.Fatalf("NO_PROXY ignored for %s: %v %v", address, p, err)
		}
	}
	for _, cfg := range []Config{{}, {Method: MethodAccelerated, Prefix: "https://accelerator.example/"}} {
		client, err := NewClient(cfg, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer client.CloseIdleConnections()
		if client.Transport.(*http.Transport).Proxy != nil {
			t.Fatal("direct or accelerated method inherited an environment proxy")
		}
	}
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("ALL_PROXY", "socks5h://fallback.example:1080")
	all, err := NewClient(Config{Method: MethodEnvironment}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer all.CloseIdleConnections()
	p, err := all.Transport.(*http.Transport).Proxy(req)
	if err != nil || p.Scheme != "socks5h" {
		t.Fatalf("ALL_PROXY ignored: %v %v", p, err)
	}
}

func TestRealHTTPAndHTTPSProxyConnect(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "https"}[secure], func(t *testing.T) {
			clearProxyEnvironment(t)
			origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Connection", "close")
				_, _ = io.WriteString(w, "proxy fixture origin")
			}))
			defer origin.Close()
			var calls atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodConnect {
					t.Errorf("expected CONNECT, got %s", r.Method)
					w.WriteHeader(400)
					return
				}
				if r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
					t.Error("missing proxy basic authentication")
				}
				upstream, err := net.Dial("tcp", strings.TrimPrefix(origin.URL, "https://"))
				if err != nil {
					t.Error(err)
					w.WriteHeader(502)
					return
				}
				downstream, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					upstream.Close()
					t.Error(err)
					return
				}
				_, _ = io.WriteString(downstream, "HTTP/1.1 200 Connection Established\r\n\r\n")
				go func() { _, _ = io.Copy(upstream, downstream); upstream.Close(); downstream.Close() }()
				_, _ = io.Copy(downstream, upstream)
				upstream.Close()
				downstream.Close()
			})
			var proxy *httptest.Server
			if secure {
				proxy = httptest.NewTLSServer(handler)
			} else {
				proxy = httptest.NewServer(handler)
			}
			defer proxy.Close()
			parsed, _ := url.Parse(proxy.URL)
			parsed.User = url.UserPassword("user", "pass")
			t.Setenv("HTTPS_PROXY", parsed.String())
			client, err := NewClient(Config{Method: MethodEnvironment}, 3*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			roots := x509.NewCertPool()
			roots.AddCert(origin.Certificate())
			if secure {
				roots.AddCert(proxy.Certificate())
			}
			client.Transport.(*http.Transport).TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			client.Transport.(*http.Transport).TLSClientConfig.RootCAs = roots
			client.Transport.(*http.Transport).TLSClientConfig.ServerName = "example.com"
			originURL, _ := url.Parse(origin.URL)
			resp, err := client.Get("https://origin.example.com:" + originURL.Port() + "/download")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || string(raw) != "proxy fixture origin" || calls.Load() != 1 {
				t.Fatalf("proxy did not carry TLS request: body=%q calls=%d err=%v", raw, calls.Load(), err)
			}
		})
	}
}

func TestRealSOCKS5AndSOCKS5HProxy(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			clearProxyEnvironment(t)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Connection", "close")
				_, _ = io.WriteString(w, "SOCKS fixture")
			}))
			defer origin.Close()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			finished := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					finished <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				header := make([]byte, 2)
				if _, err = io.ReadFull(conn, header); err != nil {
					finished <- err
					return
				}
				methods := make([]byte, int(header[1]))
				if _, err = io.ReadFull(conn, methods); err != nil {
					finished <- err
					return
				}
				_, _ = conn.Write([]byte{5, 2}) // Explicitly exercise username/password auth.
				if _, err = io.ReadFull(conn, header); err != nil {
					finished <- err
					return
				}
				user := make([]byte, int(header[1]))
				_, _ = io.ReadFull(conn, user)
				length := make([]byte, 1)
				_, _ = io.ReadFull(conn, length)
				pass := make([]byte, int(length[0]))
				_, _ = io.ReadFull(conn, pass)
				if string(user) != "user" || string(pass) != "pass" {
					finished <- io.ErrUnexpectedEOF
					return
				}
				_, _ = conn.Write([]byte{1, 0})
				request := make([]byte, 4)
				if _, err = io.ReadFull(conn, request); err != nil {
					finished <- err
					return
				}
				if request[0] != 5 || request[1] != 1 || request[3] != 3 {
					finished <- io.ErrUnexpectedEOF
					return
				}
				_, _ = io.ReadFull(conn, length)
				host := make([]byte, int(length[0]))
				_, _ = io.ReadFull(conn, host)
				port := make([]byte, 2)
				_, _ = io.ReadFull(conn, port)
				if string(host) != "origin.fixture" || binary.BigEndian.Uint16(port) == 0 {
					finished <- io.ErrUnexpectedEOF
					return
				}
				upstream, err := net.Dial("tcp", strings.TrimPrefix(origin.URL, "http://"))
				if err != nil {
					finished <- err
					return
				}
				defer upstream.Close()
				_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				go func() {
					_, _ = io.Copy(upstream, conn)
					// EOF from the client only closes the upstream write half;
					// pending response bytes must remain readable downstream.
					if tcp, ok := upstream.(*net.TCPConn); ok {
						_ = tcp.CloseWrite()
					}
				}()
				_, err = io.Copy(conn, upstream)
				finished <- err
			}()
			t.Setenv("HTTP_PROXY", scheme+"://user:pass@"+listener.Addr().String())
			client, err := NewClient(Config{Method: MethodEnvironment}, 3*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			parsed, _ := url.Parse(origin.URL)
			resp, err := client.Get("http://origin.fixture:" + parsed.Port() + "/download")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || string(raw) != "SOCKS fixture" {
				t.Fatalf("SOCKS transfer failed: %q %v", raw, err)
			}
			select {
			case err := <-finished:
				// The verified response may close either side of the tunnel first.
				if err != nil && !errors.Is(err, net.ErrClosed) {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("SOCKS fixture did not finish")
			}
		})
	}
}

func TestRequestErrorsWithholdCredentialsAndBodies(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Failure(ctx, "version query", context.Canceled)
	if !strings.Contains(err.Error(), "canceled") {
		t.Fatal(err)
	}
	for _, code := range []int{403, 407, 500} {
		err := HTTPError("download", code)
		if !strings.Contains(err.Error(), "HTTP") {
			t.Fatal(err)
		}
	}
}

func TestProxyConnectAuthenticationAndOriginAuthenticationRemainDistinct(t *testing.T) {
	clearProxyEnvironment(t)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("expected CONNECT, got %s", r.Method)
		}
		w.Header().Set("Proxy-Authenticate", "Basic realm=private-realm")
		w.WriteHeader(http.StatusProxyAuthRequired)
		_, _ = io.WriteString(w, "private-proxy-response-body")
	}))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	client, err := NewClient(Config{Method: MethodEnvironment}, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	_, err = client.Get("https://origin.example.invalid/private-url-token")
	if err == nil {
		t.Fatal("CONNECT authentication failure was accepted")
	}
	safe := Failure(t.Context(), "official version query", err)
	var category *Error
	if !errors.As(safe, &category) || category.Kind != "proxy authentication" || category.StatusCode != 407 {
		t.Fatalf("CONNECT response category lost: %v", safe)
	}
	if strings.Contains(safe.Error(), "private-") || strings.Contains(safe.Error(), proxy.URL) {
		t.Fatal("proxy error leaked source material", safe)
	}
	origin := HTTPError("official version query", http.StatusUnauthorized)
	if !errors.As(origin, &category) || category.Kind != "HTTP response" || category.StatusCode != 401 {
		t.Fatalf("origin 401 was confused with proxy authentication: %v", origin)
	}
	socks := Failure(t.Context(), "release-file download", &net.OpError{Op: "socks connect", Net: "tcp", Err: errors.New("private-socks-error")})
	if !errors.As(socks, &category) || category.Kind != "proxy connection" || strings.Contains(socks.Error(), "private-") {
		t.Fatalf("SOCKS connection was not safely classified: %v", socks)
	}
}

func TestEnvironmentStatusWithholdsProxyCredentials(t *testing.T) {
	clearProxyEnvironment(t)
	for _, tc := range []struct {
		proxy, bypass, want string
		invalid             bool
	}{
		{want: "missing"},
		{proxy: "http://private-user:private-pass@proxy.example:8080", want: "configured"},
		{proxy: "socks5h://private-user:private-pass@proxy.example:1080", want: "configured"},
		{proxy: "http://private-user:private-pass@proxy.example:8080", bypass: "github.com", want: "bypassed"},
		{proxy: "ftp://private-user:private-pass@proxy.example", invalid: true},
	} {
		t.Setenv("HTTPS_PROXY", tc.proxy)
		t.Setenv("NO_PROXY", tc.bypass)
		status, err := EnvironmentStatus()
		if tc.invalid {
			if err == nil || strings.Contains(err.Error(), "private-") {
				t.Fatalf("invalid proxy was accepted or exposed: %q %v", status, err)
			}
		} else if err != nil || status != tc.want {
			t.Fatalf("status=%q want=%q err=%v", status, tc.want, err)
		}
	}
}
