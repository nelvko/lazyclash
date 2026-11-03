package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestProxyMetadataAndEmptyGroups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"proxies":{
			"MAIN":{"name":"incorrect","type":"Selector","now":"AUTO","all":["AUTO","HK"]},
			"AUTO":{"type":"URLTest","now":"HK","all":["HK"]},
			"EMPTY":{"type":"Selector","all":[]},
			"FUTURE":{"type":"FutureGroup","all":[]},
			"HK":{"type":"Shadowsocks","udp":true,"history":[{"time":"2026-11-03T01:02:03Z","delay":41}],"server":"do-not-retain"}
		}}`))
	}))
	defer server.Close()
	proxies, err := (Client{BaseURL: server.URL}).Proxies(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if proxies["MAIN"].Name != "MAIN" || proxies["MAIN"].Now != "AUTO" || proxies["HK"].IsGroup() || !proxies["HK"].UDP || len(proxies["HK"].History) != 1 || proxies["HK"].History[0].Delay != 41 {
		t.Fatalf("metadata=%+v", proxies)
	}
	groups := ProxyGroups(proxies)
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.Name
	}
	if !reflect.DeepEqual(names, []string{"AUTO", "EMPTY", "FUTURE", "MAIN"}) {
		t.Fatalf("groups=%+v", groups)
	}
	raw, err := json.Marshal(proxies)
	if err != nil || strings.Contains(string(raw), "do-not-retain") || strings.Contains(string(raw), "server") {
		t.Fatalf("connection settings retained: %s %v", raw, err)
	}
}

func TestProxyDelayRequestAndResultStates(t *testing.T) {
	for _, tt := range []struct {
		name, body, state string
		status, delay     int
		wantError         bool
	}{
		{name: "success", status: 200, body: `{"delay":87}`, state: "success", delay: 87},
		{name: "timeout", status: 504, body: `{"message":"private-error"}`, state: "timeout"},
		{name: "failed", status: 503, body: `{"message":"private-error"}`, state: "failed"},
		{name: "gone", status: 404, body: `{"message":"private-error"}`, wantError: true},
		{name: "invalid", status: 200, body: `{"delay":0}`, wantError: true},
		{name: "missing", status: 200, body: `{}`, wantError: true},
		{name: "oversize", status: 200, body: `{"delay":65536}`, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			node := "HK /?中文"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/proxies/"+node+"/delay" || !strings.Contains(r.URL.EscapedPath(), "%2F") {
					t.Errorf("bad method/path: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer delay-secret" || r.URL.Query().Get("url") != DefaultProxyDelayURL || r.URL.Query().Get("timeout") != "5000" || len(r.URL.Query()) != 2 {
					t.Errorf("bad test request: %s", r.URL)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			result, err := (Client{BaseURL: server.URL, Secret: "delay-secret"}).TestProxyDelay(t.Context(), node)
			if (err != nil) != tt.wantError || result.State != tt.state || result.Delay != tt.delay || result.URL != DefaultProxyDelayURL || result.Time.IsZero() {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if err != nil && (strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), "secret")) {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
}

func TestProxyDelayCancellationDoesNotBecomeTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	result, err := (Client{BaseURL: server.URL}).TestProxyDelay(ctx, "HK")
	if !errors.Is(err, context.DeadlineExceeded) || result.State != "" {
		t.Fatalf("caller deadline became test result: %+v %v", result, err)
	}
}
