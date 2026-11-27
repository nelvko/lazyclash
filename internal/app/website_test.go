package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebsiteTargetsUseConfiguredProxy(t *testing.T) {
	a, svc := installedApp(t)
	settings := availableSettings(t)
	plan, err := a.PrepareRuntimeSettings(t.Context(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyRuntimeSettings(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", settings.MixedPort))
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan string, 4)
	proxy := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.Host
		w.WriteHeader(http.StatusProxyAuthRequired)
	})}
	go proxy.Serve(listener)
	t.Cleanup(func() { proxy.Close() })
	t.Setenv("NO_PROXY", "*")
	svc.active = true
	serveConfigReadback(a, fmt.Sprintf(`{"mixed-port":%d}`, settings.MixedPort))
	sites := append(DefaultWebsites(), Website{URL: "example.net:8443/health"}, Website{URL: "http://example.net/health"})
	wantRequests := []string{"CONNECT github.com:443", "CONNECT www.google.com:443", "CONNECT www.cloudflare.com:443", "CONNECT example.net:8443", "GET example.net"}
	for i, site := range sites {
		if result, err := a.TestWebsite(t.Context(), site); err == nil || result != (WebsiteTestResult{}) {
			t.Fatal("website test reported a measurement after proxy rejection")
		}
		select {
		case request := <-requests:
			if request != wantRequests[i] {
				t.Fatal(request)
			}
		default:
			t.Fatalf("no proxy request for %s", site.Name)
		}
	}
	if _, err := a.TestWebsite(t.Context(), Website{URL: "file:///private"}); err == nil || len(requests) != 0 {
		t.Fatal("expected invalid target to be rejected before sending requests")
	}
}

func TestWebsiteProbeReportsStatusWithoutWaitingForBody(t *testing.T) {
	for _, code := range []int{http.StatusOK, http.StatusForbidden, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			result, err := probeWebsite(ctx, server.Client(), server.URL)
			if err != nil || result.StatusCode != code || result.Duration <= 0 || ctx.Err() != nil {
				t.Fatal(result, err)
			}
		})
	}
}

func TestWebsiteProbePreservesDeadline(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := probeWebsite(ctx, server.Client(), server.URL); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
