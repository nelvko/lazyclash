package mihomo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const ruleJSON = `{"index":0,"type":"DomainSuffix","payload":"example.invalid","proxy":"PROXY","size":-1}`

func TestRulesPreserveMatchingOrderDuplicatesAndOptionalMetadata(t *testing.T) {
	payload := `{"rules":[
		{"index":4,"type":"DomainSuffix","payload":"example.invalid","proxy":"PROXY","size":-1},
		{"index":2,"type":"DomainSuffix","payload":"example.invalid","proxy":"PROXY","size":-1,"extra":{"disabled":true,"hitCount":2,"hitAt":"2026-10-29T08:00:00Z","missCount":3,"missAt":"0001-01-01T00:00:00Z"}},
		{"index":9,"type":"FutureRule","payload":"logical-payload","proxy":"","size":0},
		{"index":10,"type":"Match","payload":"","proxy":"DIRECT","size":-1}
	]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rules" || r.URL.RawQuery != "" {
			t.Errorf("unexpected or mutating request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer private-api-secret" {
			t.Error("rules request bypassed authentication")
		}
		_, _ = fmt.Fprint(w, payload)
	}))
	defer server.Close()
	rules, err := (Client{BaseURL: server.URL, Secret: "private-api-secret"}).Rules(t.Context())
	if err != nil || len(rules) != 4 {
		t.Fatalf("rules=%+v err=%v", rules, err)
	}
	for i, index := range []int{4, 2, 9, 10} {
		if rules[i].Index != index {
			t.Fatal("API matching order was changed")
		}
	}
	if rules[0].Extra != nil || rules[0].Size != -1 || rules[0].Payload != rules[1].Payload || rules[1].Extra == nil || !rules[1].Extra.Disabled || rules[1].Extra.HitCount != 2 || rules[1].Extra.MissCount != 3 || rules[1].Extra.HitAt.IsZero() || !rules[1].Extra.MissAt.IsZero() {
		t.Fatalf("duplicates or optional wrapper data were lost: %+v", rules)
	}
	if rules[2].Type != "FutureRule" || rules[2].Size != 0 || rules[2].Proxy != "" || rules[3].Payload != "" {
		t.Fatal("unknown type, zero size or Match payload was misrepresented")
	}
}

func TestRulesEmptyListRemainsAnAvailableEmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `{"rules":[]}`) }))
	defer server.Close()
	rules, err := (Client{BaseURL: server.URL}).Rules(t.Context())
	if err != nil || rules == nil || len(rules) != 0 {
		t.Fatalf("empty result confused with missing API data: %+v %v", rules, err)
	}
}

func TestRulesRejectInvalidSchemaWithoutEchoingContent(t *testing.T) {
	valid := `{"rules":[` + ruleJSON + `]}`
	cases := map[string]string{
		"html":          "<html>private-response-secret</html>",
		"null":          "null",
		"missing-list":  `{}`,
		"null-list":     `{"rules":null}`,
		"object-list":   `{"rules":{}}`,
		"null-rule":     `{"rules":[null]}`,
		"missing-index": strings.Replace(valid, `"index":0,`, "", 1),
		"null-index":    strings.Replace(valid, `"index":0`, `"index":null`, 1),
		"bad-index":     strings.Replace(valid, `"index":0`, `"index":-1`, 1),
		"duplicate-id":  `{"rules":[` + ruleJSON + `,` + ruleJSON + `]}`,
		"missing-type":  strings.Replace(valid, `"type":"DomainSuffix",`, "", 1),
		"empty-type":    strings.Replace(valid, `"DomainSuffix"`, `" "`, 1),
		"null-payload":  strings.Replace(valid, `"payload":"example.invalid"`, `"payload":null`, 1),
		"null-proxy":    strings.Replace(valid, `"proxy":"PROXY"`, `"proxy":null`, 1),
		"missing-size":  strings.Replace(valid, `,"size":-1`, "", 1),
		"bad-size":      strings.Replace(valid, `"size":-1`, `"size":-2`, 1),
		"fraction":      strings.Replace(valid, `"size":-1`, `"size":1.5`, 1),
		"bad-extra":     strings.Replace(valid, `"size":-1`, `"size":-1,"extra":{"hitCount":-1}`, 1),
		"bad-extra-at":  strings.Replace(valid, `"size":-1`, `"size":-1,"extra":{"hitAt":"private-response-secret"}`, 1),
		"trailing-html": valid + "<html>private-response-secret</html>",
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, payload) }))
			defer server.Close()
			_, err := (Client{BaseURL: server.URL, Secret: "private-api-secret"}).Rules(t.Context())
			if err == nil || strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), payload) {
				t.Fatalf("invalid or private response accepted/exposed: %v", err)
			}
		})
	}
}

func TestRulesUseBoundedRequestsAndWithholdHTTPBodies(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { forwarded.Add(1); _, _ = fmt.Fprint(w, `{"rules":[]}`) }))
	defer target.Close()
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", target.URL+"/private-redirect-secret")
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, "private-response-secret")
			}))
			defer server.Close()
			_, err := (Client{BaseURL: server.URL, Secret: "private-api-secret"}).Rules(t.Context())
			if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), "private-") {
				t.Fatalf("unsafe or vague HTTP error: %v", err)
			}
		})
	}
	if forwarded.Load() != 0 {
		t.Fatal("rules followed a redirect and risked forwarding credentials")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, strings.Repeat("x", MaxConfigSize+1))
	}))
	defer server.Close()
	_, err := (Client{BaseURL: server.URL}).Rules(t.Context())
	if err == nil || !strings.Contains(err.Error(), "8 MiB") {
		t.Fatalf("oversized rules response not bounded: %v", err)
	}
}

func TestRulesHonorCancellationBeforeAndDuringResponse(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			closed := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if partial {
					_, _ = fmt.Fprint(w, `{"rules":[`)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
				close(closed)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			_, err := (Client{BaseURL: server.URL}).Rules(ctx)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("cancellation lost: %v", err)
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("cancelled rules request remained open")
			}
		})
	}
}
