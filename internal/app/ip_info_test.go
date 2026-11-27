package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestLookupIPInfoUsesConfirmedProxyAndCredentials(t *testing.T) {
	a, svc := installedApp(t)
	settings := availableSettings(t)
	plan, err := a.PrepareRuntimeSettings(t.Context(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyRuntimeSettings(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	auth, err := a.GetProxyAuth(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	user := ProxyAuthUser{Username: "fixture", Password: "private:password"}
	plan, err = a.PrepareProxyAuth(t.Context(), auth, []ProxyAuthUser{user})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyRuntimeSettings(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", settings.MixedPort))
	if err != nil {
		t.Fatal(err)
	}
	var connects atomic.Int32
	proxy := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connects.Add(1)
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(user.Username+":"+user.Password))
		if r.Method != "CONNECT" || r.Host != "ipwho.is:443" || r.Header.Get("Proxy-Authorization") != want || r.Header.Get("Authorization") != "" {
			t.Error("unexpected tunnel destination or credentials")
		}
		w.WriteHeader(http.StatusProxyAuthRequired)
	})}
	go proxy.Serve(l)
	defer proxy.Close()
	t.Setenv("NO_PROXY", "*")
	serveConfigReadback(a, fmt.Sprintf(`{"mixed-port":%d}`, settings.MixedPort))
	for _, state := range []string{"stopped", "foreign", "unconfirmed", "ready"} {
		svc.active = state != "stopped"
		svc.verifyErr = nil
		if state == "foreign" {
			svc.verifyErr = errors.New("unit belongs to a different installation")
		}
		if state == "unconfirmed" {
			serveConfigReadback(a, `{"mixed-port":0}`)
		}
		if state == "ready" {
			serveConfigReadback(a, fmt.Sprintf(`{"mixed-port":%d}`, settings.MixedPort))
		}
		_, err := a.LookupIPInfo(t.Context())
		if err == nil || strings.Contains(err.Error(), user.Password) {
			t.Fatalf("%s returned success or exposed proxy credentials", state)
		}
		if c := connects.Load(); (state == "ready" && c != 1) || (state != "ready" && c != 0) {
			t.Fatalf("%s issued %d CONNECT requests", state, c)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.LookupIPInfo(ctx); !errors.Is(err, context.Canceled) || connects.Load() != 1 {
		t.Fatal("cancelled lookup reached the proxy or lost cancellation", err)
	}
}

func TestIPInfoProxySelectsEnabledListener(t *testing.T) {
	for _, tc := range []struct {
		mixed, http, socks int
		want               string
	}{
		{1080, 8080, 1081, "http://[::1]:1080"},
		{0, 8080, 1081, "http://[::1]:8080"},
		{0, 0, 1081, "socks5://[::1]:1081"},
		{0, 0, 0, ""},
	} {
		a, _ := testApp(t)
		m := manifest{Controller: "127.0.0.1:9090", Port: tc.mixed, Listeners: mihomo.ListenerOptions{
			AllowLAN: true, BindAddress: "[::]", HTTPPort: tc.http, SOCKSPort: tc.socks,
		}}
		serveConfigReadback(a, fmt.Sprintf(`{"mixed-port":%d,"port":%d,"socks-port":%d}`, tc.mixed, tc.http, tc.socks))
		proxy, err := ipInfoProxy(t.Context(), m, a.client(m))
		if tc.want == "" {
			if err == nil || proxy != nil {
				t.Fatal("lookup accepted disabled proxy listeners")
			}
		} else if err != nil || proxy.String() != tc.want {
			t.Fatal(proxy, err)
		}
	}
}

func TestFetchIPInfoValidatesResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
		want IPInfo
	}{
		{"full", 200, `{"success":true,"ip":"203.0.113.7","country":"Japan","city":"Tokyo","region":"Tokyo","connection":{"asn":64500,"isp":"Example ISP","org":"Example"},"timezone":{"id":"Asia/Tokyo"}}`,
			IPInfo{IP: "203.0.113.7", Country: "Japan", City: "Tokyo", Region: "Tokyo", ISP: "Example ISP", ASN: "AS64500"}},
		{"partial IPv6", 200, `{"success":true,"ip":"2001:db8::1","region":"\u001b[2Jprivate"}`, IPInfo{IP: "2001:db8::1"}},
		{"rate limited", 429, "private", IPInfo{}},
		{"provider error", 200, `{"success":false,"ip":"203.0.113.7","message":"private"}`, IPInfo{}},
		{"invalid IP", 200, `{"success":true,"ip":"private"}`, IPInfo{}},
		{"broken JSON", 200, `{"success":true`, IPInfo{}},
		{"oversized", 200, `{"success":true,"ip":"203.0.113.7"}` + strings.Repeat(" ", 16<<10), IPInfo{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.code, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			info, err := fetchIPInfo(t.Context(), client)
			if info != tc.want || (err == nil) != (tc.want.IP != "") || err != nil && strings.Contains(err.Error(), "private") {
				t.Fatalf("got %+v, err %v", info, err)
			}
		})
	}
}
