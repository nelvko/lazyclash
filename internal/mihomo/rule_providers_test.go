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

const ruleProviderJSON = `{"name":"fixture","type":"Rule","vehicleType":"HTTP","behavior":"Domain","format":"YamlRule","ruleCount":42,"updatedAt":"2026-11-06T00:00:00Z"}`

func TestRuleProvidersReadOnlyMetadataAndStableOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/providers/rules" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer private-api-secret" {
			t.Errorf("unexpected provider request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"providers":{
			"fixture":`+ruleProviderJSON+`,
			"inline":{"name":"inline","type":"Rule","vehicleType":"Inline","behavior":"Classical","format":"","ruleCount":0,"updatedAt":"0001-01-01T00:00:00Z","payload":["private-inline-secret"]},
			"a/file":{"name":"a/file","type":"Rule","vehicleType":"File","behavior":"IPCIDR","format":"MrsRule","ruleCount":3,"updatedAt":"2026-11-06T08:00:00+08:00","url":"https://private-url-secret.invalid"}
		}}`)
	}))
	defer server.Close()
	providers, err := (Client{BaseURL: server.URL, Secret: "private-api-secret"}).RuleProviders(t.Context())
	if err != nil || len(providers) != 3 {
		t.Fatalf("providers=%+v err=%v", providers, err)
	}
	var names []string
	for _, p := range providers {
		names = append(names, p.Name)
	}
	if !reflect.DeepEqual(names, []string{"a/file", "fixture", "inline"}) || providers[0].Format != "MrsRule" || providers[1].RuleCount != 42 || providers[1].UpdatedAt.IsZero() || providers[2].Format != "" || providers[2].RuleCount != 0 || !providers[2].UpdatedAt.IsZero() {
		t.Fatalf("provider metadata changed: %+v", providers)
	}
	raw, err := json.Marshal(providers)
	if err != nil || strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "payload") || strings.Contains(string(raw), `"url"`) {
		t.Fatalf("private provider content escaped metadata boundary: %s %v", raw, err)
	}
}

func TestRuleProvidersEmptyMapRemainsAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `{"providers":{}}`) }))
	defer server.Close()
	providers, err := (Client{BaseURL: server.URL}).RuleProviders(t.Context())
	if err != nil || providers == nil || len(providers) != 0 {
		t.Fatalf("valid empty providers confused with missing data: %+v %v", providers, err)
	}
}

func TestRuleProvidersRejectMalformedMetadataWithoutEchoingResponse(t *testing.T) {
	valid := `{"providers":{"fixture":` + ruleProviderJSON + `}}`
	cases := map[string]string{
		"html":              "<html>private-response-secret</html>",
		"null":              "null",
		"missing providers": `{}`,
		"null providers":    `{"providers":null}`,
		"array providers":   `{"providers":[]}`,
		"null provider":     `{"providers":{"fixture":null}}`,
		"missing name":      strings.Replace(valid, `"name":"fixture",`, "", 1),
		"mismatched name":   strings.Replace(valid, `"name":"fixture"`, `"name":"other"`, 1),
		"empty name":        `{"providers":{" ":` + ruleProviderJSON + `}}`,
		"wrong type":        strings.Replace(valid, `"type":"Rule"`, `"type":"Proxy"`, 1),
		"missing vehicle":   strings.Replace(valid, `"vehicleType":"HTTP",`, "", 1),
		"blank vehicle":     strings.Replace(valid, `"vehicleType":"HTTP"`, `"vehicleType":" "`, 1),
		"missing behavior":  strings.Replace(valid, `"behavior":"Domain",`, "", 1),
		"null behavior":     strings.Replace(valid, `"behavior":"Domain"`, `"behavior":null`, 1),
		"missing format":    strings.Replace(valid, `"format":"YamlRule",`, "", 1),
		"empty file format": strings.Replace(valid, `"format":"YamlRule"`, `"format":""`, 1),
		"missing count":     strings.Replace(valid, `"ruleCount":42,`, "", 1),
		"negative count":    strings.Replace(valid, `"ruleCount":42`, `"ruleCount":-1`, 1),
		"fractional count":  strings.Replace(valid, `"ruleCount":42`, `"ruleCount":1.5`, 1),
		"null timestamp":    strings.Replace(valid, `"updatedAt":"2026-11-06T00:00:00Z"`, `"updatedAt":null`, 1),
		"missing timestamp": strings.Replace(valid, `,"updatedAt":"2026-11-06T00:00:00Z"`, "", 1),
		"invalid timestamp": strings.Replace(valid, "2026-11-06T00:00:00Z", "private-response-secret", 1),
		"trailing document": valid + "{}",
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, payload) }))
			defer server.Close()
			_, err := (Client{BaseURL: server.URL, Secret: "private-api-secret"}).RuleProviders(t.Context())
			if err == nil || strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), payload) {
				t.Fatalf("invalid/private providers accepted or exposed: %v", err)
			}
		})
	}
}

func TestUpdateRuleProviderEscapesExactNameAndAcceptsNoContent(t *testing.T) {
	name := "中文规则/one ?#%+&"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPut || r.RequestURI != "/providers/rules/"+url.PathEscape(name) || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer private-api-secret" || r.ContentLength != 0 {
			t.Errorf("update did not preserve exact provider identity: %s %s", r.Method, r.RequestURI)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, Secret: "private-api-secret"}
	if err := client.UpdateRuleProvider(t.Context(), name); err != nil || calls.Load() != 1 {
		t.Fatalf("204 update failed: %v calls=%d", err, calls.Load())
	}
	for _, empty := range []string{"", " \t\n"} {
		if err := client.UpdateRuleProvider(t.Context(), empty); err == nil || calls.Load() != 1 {
			t.Fatal("empty name reached controller")
		}
	}
}

func TestRuleProviderRequestsKeepCredentialsAndHTTPFailuresPrivate(t *testing.T) {
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
					_, err = client.RuleProviders(t.Context())
				} else {
					err = client.UpdateRuleProvider(t.Context(), "private-provider-secret")
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

func TestRuleProviderRequestsCancelAndPreserveShortClientTimeout(t *testing.T) {
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
				_, err = client.RuleProviders(ctx)
			} else {
				err = client.UpdateRuleProvider(ctx, "fixture")
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
	if err := (Client{BaseURL: server.URL, HTTP: httpClient}).UpdateRuleProvider(t.Context(), "fixture"); err == nil || time.Since(start) > time.Second || httpClient.Timeout != 20*time.Millisecond {
		t.Fatalf("short supplied client timeout changed or was ignored: %v", err)
	}
}
