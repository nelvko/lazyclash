package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAPIGroupsSelectionAndEscapedPath(t *testing.T) {
	group := "PROXY /?中文"
	selected := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-api-secret" {
			t.Error("missing credentials")
		}
		switch {
		case r.URL.Path == "/version":
			_, _ = w.Write([]byte(`{"version":"v1.19.33","meta":true}`))
		case r.URL.Path == "/proxies":
			_ = json.NewEncoder(w).Encode(map[string]any{"proxies": map[string]any{group: Group{Type: "Selector", Now: "DIRECT", All: []string{"DIRECT", "REJECT"}}, "DIRECT": map[string]any{"type": "Direct"}}})
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/"+group:
			if !strings.Contains(r.URL.EscapedPath(), "%2F") || r.URL.RawQuery != "" {
				t.Error("group name was not path escaped")
			}
			var body struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			selected = body.Name
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c := Client{BaseURL: server.URL, Secret: "private-api-secret"}
	if err := c.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	groups, err := c.Groups(t.Context())
	if err != nil || len(groups) != 1 || groups[0].Name != group {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	if err := c.Select(t.Context(), group, "REJECT"); err != nil {
		t.Fatal(err)
	}
	if selected != "REJECT" {
		t.Fatal("node not selected")
	}
	if err := c.Select(t.Context(), group, "unknown"); err == nil {
		t.Fatal("accepted unknown node")
	}
}

func TestAPIRejectsRedirectsAndWithholdsErrorBody(t *testing.T) {
	for _, code := range []int{401, 403, 500, 302} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "https://example.com/private-token")
			w.WriteHeader(code)
			_, _ = w.Write([]byte("private-response-secret"))
		}))
		c := Client{BaseURL: server.URL, Secret: "private-api-secret"}
		err := c.Ping(t.Context())
		server.Close()
		if err == nil || strings.Contains(err.Error(), "private-") {
			t.Fatalf("unsafe HTTP %d error: %v", code, err)
		}
	}
	if err := (Client{BaseURL: "http://example.com:19090"}).Ping(t.Context()); err == nil {
		t.Fatal("accepted remote API")
	}
}

func TestAPIHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := (Client{BaseURL: server.URL}).Ping(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline lost: %v", err)
	}
}

func TestAPIDisablesEnvironmentProxy(t *testing.T) {
	proxyCalls := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyCalls++; w.WriteHeader(500) }))
	defer proxy.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"version":"v1.19.33"}`)) }))
	defer server.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxyURL, _ := http.NewRequest(http.MethodGet, proxy.URL, nil)
	transport.Proxy = http.ProxyURL(proxyURL.URL)
	c := Client{BaseURL: server.URL, HTTP: &http.Client{Transport: transport}}
	if err := c.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	if proxyCalls != 0 {
		t.Fatal("API followed configured HTTP proxy")
	}
}
