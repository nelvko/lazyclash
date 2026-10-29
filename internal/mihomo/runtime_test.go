package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const connectionJSON = `{"id":"c088e2c1-b92c-4541-8bee-937c94cbfa12","metadata":{"network":"tcp","type":"HTTP","sourceIP":"127.0.0.1","sourcePort":"54321","destinationIP":"2001:db8::1","destinationPort":"443","host":"fixture.invalid","dnsMode":"normal","process":"fixture","processPath":"/tmp/fixture"},"upload":10,"download":20,"start":"2026-10-29T08:00:00Z","chains":["DIRECT","PROXY"],"rule":"Match","rulePayload":""}`

func TestConnectionsSnapshotTotalsAndSingleConnectionClose(t *testing.T) {
	var closed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-api-secret" {
			t.Error("missing authentication")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/connections":
			connections := "[" + connectionJSON + "]"
			if closed.Load() {
				connections = "null"
			}
			_, _ = fmt.Fprintf(w, `{"uploadTotal":1024,"downloadTotal":4096,"connections":%s,"memory":65536}`, connections)
		case r.Method == http.MethodDelete && r.URL.Path == "/connections/c088e2c1-b92c-4541-8bee-937c94cbfa12":
			if r.URL.RawQuery != "" {
				t.Error("unexpected query")
			}
			closed.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c := Client{BaseURL: server.URL, Secret: "private-api-secret"}
	snapshot, err := c.Connections(t.Context())
	if err != nil || snapshot.UploadTotal != 1024 || snapshot.DownloadTotal != 4096 || len(snapshot.Connections) != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	connection := snapshot.Connections[0]
	if connection.Metadata.DestinationPort != "443" || connection.Metadata.SourcePort != "54321" || connection.Start.IsZero() || connection.Download != 20 || len(connection.Chains) != 2 {
		t.Fatalf("connection not decoded: %+v", connection)
	}
	if err := c.CloseConnection(t.Context(), connection.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = c.Connections(t.Context())
	if err != nil || len(snapshot.Connections) != 0 || snapshot.Connections == nil || snapshot.UploadTotal != 1024 || snapshot.DownloadTotal != 4096 {
		t.Fatalf("closing a connection changed cumulative counters: %+v %v", snapshot, err)
	}
}

func TestConnectionsRejectInvalidResponsesWithoutEchoingContent(t *testing.T) {
	valid := `{"uploadTotal":1,"downloadTotal":2,"connections":[` + connectionJSON + `]}`
	cases := map[string]string{
		"html":             "<html>private-response-secret</html>",
		"null":             "null",
		"wrong-endpoint":   `{"version":"private-response-secret"}`,
		"missing-list":     `{"uploadTotal":1,"downloadTotal":2}`,
		"negative-total":   strings.Replace(valid, `"uploadTotal":1`, `"uploadTotal":-1`, 1),
		"null-total":       strings.Replace(valid, `"uploadTotal":1`, `"uploadTotal":null`, 1),
		"invalid-list":     `{"uploadTotal":1,"downloadTotal":2,"connections":{}}`,
		"missing-metadata": strings.Replace(valid, `"metadata":{`, `"ignoredMetadata":{`, 1),
		"empty-id":         strings.Replace(valid, `c088e2c1-b92c-4541-8bee-937c94cbfa12`, "", 1),
		"unsafe-id":        strings.Replace(valid, `c088e2c1-b92c-4541-8bee-937c94cbfa12`, "../private-response-secret", 1),
		"duplicate-id":     `{"uploadTotal":1,"downloadTotal":2,"connections":[` + connectionJSON + "," + connectionJSON + `]}`,
		"negative-count":   strings.Replace(valid, `"upload":10`, `"upload":-1`, 1),
		"missing-count":    strings.Replace(valid, `"download":20`, `"ignoredDownload":20`, 1),
		"bad-port":         strings.Replace(valid, `"443"`, `"65536"`, 1),
		"numeric-port":     strings.Replace(valid, `"443"`, `443`, 1),
		"bad-time":         strings.Replace(valid, "2026-10-29T08:00:00Z", "private-response-secret", 1),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(payload)) }))
			defer server.Close()
			_, err := (Client{BaseURL: server.URL, Secret: "private-api-secret"}).Connections(t.Context())
			if err == nil || strings.Contains(err.Error(), "private-") {
				t.Fatalf("invalid response accepted or exposed: %v", err)
			}
		})
	}
}

func TestConnectionCloseRejectsEmptyOrEscapingIDsBeforeRequest(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	c := Client{BaseURL: server.URL}
	for _, id := range []string{"", "..", "a/b", "%2f", "id?all=true", "id#fragment", "id\n", "中文", strings.Repeat("a", 129)} {
		if err := c.CloseConnection(t.Context(), id); err == nil {
			t.Fatalf("accepted unsafe ID %q", id)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("invalid IDs reached the API")
	}
}

func TestTrafficSamplesPlainAndChunkedJSONAndClosesStream(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		t.Run(fmt.Sprint(chunked), func(t *testing.T) {
			closed := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/traffic" || r.Header.Get("Authorization") != "Bearer private-api-secret" {
					t.Error("traffic request bypassed authentication or route")
				}
				w.Header().Set("Content-Type", "application/json")
				if chunked {
					for _, part := range []string{" \n{\"up\":", "1024,\"down\":2048,", "\"upTotal\":9999}\n"} {
						_, _ = w.Write([]byte(part))
						w.(http.Flusher).Flush()
					}
					<-r.Context().Done()
				} else {
					_, _ = w.Write([]byte(`{"up":1024,"down":2048}`))
				}
				close(closed)
			}))
			defer server.Close()
			sample, err := (Client{BaseURL: server.URL, Secret: "private-api-secret"}).Traffic(t.Context())
			if err != nil || sample.Up != 1024 || sample.Down != 2048 {
				t.Fatalf("sample=%+v err=%v", sample, err)
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("sample left its stream open")
			}
		})
	}
}

func TestMemoryWaitsForActualSecondFrame(t *testing.T) {
	first := make(chan struct{})
	release := make(chan struct{})
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/memory" || r.Header.Get("Authorization") != "Bearer private-api-secret" {
			t.Error("memory request bypassed authentication or route")
		}
		_, _ = w.Write([]byte("{\"inuse\":0,\"oslimit\":0}\n"))
		w.(http.Flusher).Flush()
		close(first)
		select {
		case <-release:
		case <-r.Context().Done():
			close(closed)
			return
		}
		_, _ = w.Write([]byte("{\"inuse\":16777216,\"oslimit\":0}\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	}))
	defer server.Close()
	type result struct {
		memory uint64
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		memory, err := (Client{BaseURL: server.URL, Secret: "private-api-secret"}).Memory(t.Context())
		resultCh <- result{memory, err}
	}()
	<-first
	select {
	case early := <-resultCh:
		close(release)
		t.Fatalf("reported synthetic first frame as memory: %+v", early)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	resultValue := <-resultCh
	if resultValue.err != nil || resultValue.memory != 16777216 {
		t.Fatalf("memory=%d err=%v", resultValue.memory, resultValue.err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("memory stream was left open")
	}
}

func TestRuntimeSamplesRejectMalformedOrMissingValues(t *testing.T) {
	for _, payload := range []string{"<html>private-response-secret</html>", "null", `{}`, `{"up":-1,"down":0}`, `{"up":1}`, `{"up":0.5,"down":0}`, `{"up":0,"down":null}`, `{"up":1,"down":0,"padding":"` + strings.Repeat("x", maxRuntimeSample) + `"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(payload)) }))
		_, err := (Client{BaseURL: server.URL}).Traffic(t.Context())
		server.Close()
		if err == nil || strings.Contains(err.Error(), "private-") {
			t.Fatalf("invalid traffic accepted or exposed: %v", err)
		}
	}
	for _, final := range []string{`{}`, `null`, `{"inuse":null}`, `{"inuse":-1}`, `{"inuse":"private-response-secret"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprintf(w, "{\"inuse\":0}\n%s\n", final) }))
		_, err := (Client{BaseURL: server.URL}).Memory(t.Context())
		server.Close()
		if err == nil || strings.Contains(err.Error(), "private-") {
			t.Fatalf("invalid second memory frame accepted or exposed: %v", err)
		}
	}
}

func TestRuntimeSampleCancellationAndHTTPFailures(t *testing.T) {
	for _, path := range []string{"/traffic", "/memory"} {
		t.Run(path, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if path == "/memory" {
					_, _ = w.Write([]byte("{\"inuse\":0}\n"))
				} else {
					_, _ = w.Write([]byte("{\"up\":"))
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			var err error
			if path == "/traffic" {
				_, err = (Client{BaseURL: server.URL}).Traffic(ctx)
			} else {
				_, err = (Client{BaseURL: server.URL}).Memory(ctx)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("sample lost cancellation: %v", err)
			}
		})
	}
	for _, code := range []int{401, 403, 429, 500, 302} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "https://example.invalid/private-location-secret")
			w.WriteHeader(code)
			_, _ = w.Write([]byte("private-response-secret"))
		}))
		c := Client{BaseURL: server.URL, Secret: "private-api-secret"}
		_, trafficErr := c.Traffic(t.Context())
		_, memoryErr := c.Memory(t.Context())
		_, connectionsErr := c.Connections(t.Context())
		closeErr := c.CloseConnection(t.Context(), "safe-id")
		server.Close()
		for _, err := range []error{trafficErr, memoryErr, connectionsErr, closeErr} {
			if err == nil || strings.Contains(err.Error(), "private-") {
				t.Fatalf("unsafe HTTP %d error: %v", code, err)
			}
		}
	}
}

func TestConnectionsResponseLimitAndRuntimeTransportPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/connections" {
			_ = json.NewEncoder(w).Encode(map[string]any{"uploadTotal": 0, "downloadTotal": 0, "connections": []any{}, "padding": strings.Repeat("x", MaxConfigSize)})
			return
		}
		_, _ = w.Write([]byte("{\"up\":0,\"down\":0}\n"))
	}))
	defer server.Close()
	if _, err := (Client{BaseURL: server.URL}).Connections(t.Context()); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize snapshot accepted: %v", err)
	}
	var proxyCalls atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyCalls.Add(1); w.WriteHeader(500) }))
	defer proxy.Close()
	proxyURL, _ := http.NewRequest(http.MethodGet, proxy.URL, nil)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL.URL)
	if _, err := (Client{BaseURL: server.URL, HTTP: &http.Client{Transport: transport}}).Traffic(t.Context()); err != nil {
		t.Fatal(err)
	}
	if proxyCalls.Load() != 0 {
		t.Fatal("stream followed HTTP proxy")
	}
	remote := Client{BaseURL: "http://example.invalid:19090"}
	if _, err := remote.Traffic(t.Context()); err == nil {
		t.Fatal("accepted remote controller")
	}
}
