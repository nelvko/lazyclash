package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const proxyProviderJSON = `{"name":"fixture","type":"Proxy","vehicleType":"HTTP","proxies":[{"name":"private-node-secret"},{"name":"private-node-secret-2"}],"updatedAt":"2026-11-06T00:00:00Z"}`

func TestProxyProvidersReadOnlyMetadataAndStableOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/providers/proxies" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer private-api-secret" {
			t.Errorf("unexpected provider request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"providers":{
			"fixture":`+proxyProviderJSON+`,
			"inline":{"name":"inline","type":"Proxy","vehicleType":"Inline","proxies":[],"payload":["private-inline-secret"]},
			"a/file":{"name":"a/file","type":"Proxy","vehicleType":"File","proxies":[{},{},{}],"updatedAt":"2026-11-06T08:00:00+08:00","url":"https://private-url-secret.invalid"}
		}}`)
	}))
	defer server.Close()
	providers, err := (Client{BaseURL: server.URL, Secret: "private-api-secret"}).ProxyProviders(t.Context())
	if err != nil || len(providers) != 3 {
		t.Fatalf("providers=%+v err=%v", providers, err)
	}
	var names []string
	for _, p := range providers {
		names = append(names, p.Name)
	}
	if !reflect.DeepEqual(names, []string{"a/file", "fixture", "inline"}) || providers[0].ProxyCount != 3 || providers[1].ProxyCount != 2 || providers[1].UpdatedAt.IsZero() || providers[2].ProxyCount != 0 || !providers[2].UpdatedAt.IsZero() {
		t.Fatalf("provider metadata changed: %+v", providers)
	}
	raw, err := json.Marshal(providers)
	if err != nil || strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "payload") || strings.Contains(string(raw), `"url"`) {
		t.Fatalf("private provider content escaped metadata boundary: %s %v", raw, err)
	}
}

func TestProxyProvidersEmptyMapRemainsAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `{"providers":{}}`) }))
	defer server.Close()
	providers, err := (Client{BaseURL: server.URL}).ProxyProviders(t.Context())
	if err != nil || providers == nil || len(providers) != 0 {
		t.Fatalf("valid empty providers confused with missing data: %+v %v", providers, err)
	}
}

func TestProxyProvidersRejectMalformedMetadataWithoutEchoingResponse(t *testing.T) {
	valid := `{"providers":{"fixture":` + proxyProviderJSON + `}}`
	cases := map[string]string{
		"html":              "<html>private-response-secret</html>",
		"null":              "null",
		"missing providers": `{}`,
		"null providers":    `{"providers":null}`,
		"array providers":   `{"providers":[]}`,
		"null provider":     `{"providers":{"fixture":null}}`,
		"missing name":      strings.Replace(valid, `"name":"fixture",`, "", 1),
		"mismatched name":   strings.Replace(valid, `"name":"fixture"`, `"name":"other"`, 1),
		"empty name":        `{"providers":{" ":` + proxyProviderJSON + `}}`,
		"wrong type":        strings.Replace(valid, `"type":"Proxy"`, `"type":"Rule"`, 1),
		"missing vehicle":   strings.Replace(valid, `"vehicleType":"HTTP",`, "", 1),
		"blank vehicle":     strings.Replace(valid, `"vehicleType":"HTTP"`, `"vehicleType":" "`, 1),
		"missing nodes":     `{"providers":{"fixture":{"name":"fixture","type":"Proxy","vehicleType":"HTTP"}}}`,
		"null nodes":        `{"providers":{"fixture":{"name":"fixture","type":"Proxy","vehicleType":"HTTP","proxies":null}}}`,
		"object nodes":      `{"providers":{"fixture":{"name":"fixture","type":"Proxy","vehicleType":"HTTP","proxies":{}}}}`,
		"invalid timestamp": strings.Replace(valid, "2026-11-06T00:00:00Z", "private-response-secret", 1),
		"trailing document": valid + "{}",
	}

	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, payload) }))
			defer server.Close()
			_, err := (Client{BaseURL: server.URL, Secret: "private-api-secret"}).ProxyProviders(t.Context())
			if err == nil || strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), payload) {
				t.Fatalf("invalid/private providers accepted or exposed: %v", err)
			}
		})
	}
}

func TestUpdateProxyProviderEscapesExactNameAndAcceptsNoContent(t *testing.T) {
	name := "中文代理/one ?#%+&"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPut || r.RequestURI != "/providers/proxies/"+url.PathEscape(name) || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer private-api-secret" || r.ContentLength != 0 {
			t.Errorf("update did not preserve exact provider identity: %s %s", r.Method, r.RequestURI)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, Secret: "private-api-secret"}
	if err := client.UpdateProxyProvider(t.Context(), name); err != nil || calls.Load() != 1 {
		t.Fatalf("204 update failed: %v calls=%d", err, calls.Load())
	}
	for _, empty := range []string{"", " \t\n"} {
		if err := client.UpdateProxyProvider(t.Context(), empty); err == nil || calls.Load() != 1 {
			t.Fatal("empty name reached controller")
		}
	}
}

func TestProxyProviderRequestsKeepCredentialsAndHTTPFailuresPrivate(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { forwarded.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer target.Close()
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable, http.StatusFound} {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Location", target.URL+"/private-redirect-secret")
					w.WriteHeader(status)
					_, _ = fmt.Fprint(w, "private-response-secret")
				}))
				defer server.Close()
				client := Client{BaseURL: server.URL, Secret: "private-api-secret"}
				var err error
				if method == http.MethodGet {
					_, err = client.ProxyProviders(t.Context())
				} else {
					err = client.UpdateProxyProvider(t.Context(), "private-provider-secret")
				}
				if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), "private-") {
					t.Fatalf("provider HTTP failure lacked status or leaked private values: %v", err)
				}
			})
		}
	}
	if forwarded.Load() != 0 {
		t.Fatal("provider request followed a redirect")
	}
}

func TestProxyProviderRequestsCancelAndPreserveShortClientTimeout(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			finished := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if method == http.MethodGet {
					_, _ = fmt.Fprint(w, `{"providers":`)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
				close(finished)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			client := Client{BaseURL: server.URL}
			var err error
			if method == http.MethodGet {
				_, err = client.ProxyProviders(ctx)
			} else {
				err = client.UpdateProxyProvider(ctx, "fixture")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("provider cancellation lost: %v", err)
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("cancelled provider request remained open")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	httpClient := &http.Client{Timeout: 20 * time.Millisecond}
	start := time.Now()
	if err := (Client{BaseURL: server.URL, HTTP: httpClient}).UpdateProxyProvider(t.Context(), "fixture"); err == nil || time.Since(start) > time.Second || httpClient.Timeout != 20*time.Millisecond {
		t.Fatalf("short supplied client timeout changed or was ignored: %v", err)
	}
}

func TestProxyProvidersReadOnlyKindsAndUnknownTime(t *testing.T) {
	for _, vehicle := range []string{"Inline", "Compatible", "Unknown"} {
		for _, timestamp := range []string{"", `,"updatedAt":null`} {
			t.Run(vehicle+timestamp, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = fmt.Fprint(w, `{"providers":{"fixture":{"name":"fixture","type":"Proxy","vehicleType":"`+vehicle+`","proxies":[]`+timestamp+`}}}`)
				}))
				defer server.Close()
				providers, err := (Client{BaseURL: server.URL}).ProxyProviders(t.Context())
				if err != nil || len(providers) != 1 || providers[0].VehicleType != vehicle || !providers[0].UpdatedAt.IsZero() {
					t.Fatalf("read-only provider or unknown timestamp rejected: %+v %v", providers, err)
				}
			})
		}
	}
}
