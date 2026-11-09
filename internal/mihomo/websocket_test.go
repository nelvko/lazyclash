package mihomo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func acceptTestWebSocket(t *testing.T, w http.ResponseWriter, r *http.Request) *websocket.Conn {
	t.Helper()
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		t.Errorf("accept WebSocket: %v", err)
	}
	return conn
}

func TestWebSocketRuntimeSourcesKeepOneConnectionForMultipleFrames(t *testing.T) {
	for _, test := range []struct {
		name, path string
		frames     []string
		stream     func(Client, context.Context, func(string)) error
		want       []string
	}{
		{
			name: "traffic", path: "/traffic",
			frames: []string{`{"up":1,"down":2,"upTotal":100,"downTotal":200}`, `{"up":0,"down":3,"upTotal":100,"downTotal":203}`},
			stream: func(c Client, ctx context.Context, emit func(string)) error {
				return c.StreamTraffic(ctx, func(v Traffic) { emit(fmt.Sprintf("%d/%d/%d/%d", v.Up, v.Down, v.UpTotal, v.DownTotal)) })
			},
			want: []string{"1/2/100/200", "0/3/100/203"},
		},
		{
			name: "memory", path: "/memory",
			frames: []string{`{"inuse":0,"oslimit":0}`, `{"inuse":65536,"oslimit":0}`, `{"inuse":0,"oslimit":0}`},
			stream: func(c Client, ctx context.Context, emit func(string)) error {
				return c.StreamMemory(ctx, func(v uint64) { emit(fmt.Sprint(v)) })
			},
			want: []string{"65536", "0"},
		},
		{
			name: "connections", path: "/connections?interval=1000",
			frames: []string{`{"uploadTotal":100,"downloadTotal":200,"connections":[` + connectionJSON + `]}`, `{"uploadTotal":100,"downloadTotal":220,"connections":null}`},
			stream: func(c Client, ctx context.Context, emit func(string)) error {
				return c.StreamConnections(ctx, func(v ConnectionSnapshot) {
					if v.Connections == nil {
						t.Error("missing connections array was not normalized")
					}
					emit(fmt.Sprintf("%d/%d/%d", len(v.Connections), v.UploadTotal, v.DownloadTotal))
				})
			},
			want: []string{"1/100/200", "0/100/220"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var handshakes atomic.Int64
			finished := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(finished)
				handshakes.Add(1)
				if r.Method != http.MethodGet || r.URL.RequestURI() != test.path || r.Header.Get("Authorization") != "Bearer private-secret" {
					t.Error("stream did not authenticate the expected endpoint")
				}
				conn := acceptTestWebSocket(t, w, r)
				if conn == nil {
					return
				}
				defer conn.CloseNow()
				for _, raw := range test.frames {
					if err := conn.Write(t.Context(), websocket.MessageText, []byte(raw)); err != nil {
						t.Errorf("write fixture frame: %v", err)
						return
					}
				}
				_, _, _ = conn.Read(t.Context())
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var got []string
			err := test.stream(Client{BaseURL: server.URL, Secret: "private-secret"}, ctx, func(value string) {
				got = append(got, value)
				if len(got) == len(test.want) {
					cancel()
				}
			})
			if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, test.want) || handshakes.Load() != 1 {
				t.Fatalf("stream frames=%v, connections=%d, err=%v", got, handshakes.Load(), err)
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("cancellation left the WebSocket open")
			}
		})
	}
}

func TestWebSocketStreamsRejectMalformedAndOversizedMessages(t *testing.T) {
	for _, test := range []struct {
		name, raw string
		kind      websocket.MessageType
		stream    func(Client, context.Context, func()) error
	}{
		{name: "traffic missing totals", raw: `{"up":0,"down":0}`, kind: websocket.MessageText},
		{name: "traffic negative totals", raw: `{"up":0,"down":0,"upTotal":-1,"downTotal":0}`, kind: websocket.MessageText},
		{name: "traffic negative rate", raw: `{"up":-1,"down":0,"upTotal":0,"downTotal":0}`, kind: websocket.MessageText},
		{name: "traffic invalid JSON", raw: "private-response-secret", kind: websocket.MessageText},
		{name: "traffic binary", raw: `{"up":0,"down":0,"upTotal":0,"downTotal":0}`, kind: websocket.MessageBinary},
		{name: "traffic too large", raw: strings.Repeat("x", maxRuntimeSample+1), kind: websocket.MessageText},
		{name: "memory invalid first frame", raw: `{"inuse":null}`, kind: websocket.MessageText,
			stream: func(c Client, ctx context.Context, emit func()) error {
				return c.StreamMemory(ctx, func(uint64) { emit() })
			}},
		{name: "connections missing list", raw: `{"uploadTotal":0,"downloadTotal":0}`, kind: websocket.MessageText,
			stream: func(c Client, ctx context.Context, emit func()) error {
				return c.StreamConnections(ctx, func(ConnectionSnapshot) { emit() })
			}},
		{name: "connections too large", raw: strings.Repeat("x", MaxConfigSize+1), kind: websocket.MessageText,
			stream: func(c Client, ctx context.Context, emit func()) error {
				return c.StreamConnections(ctx, func(ConnectionSnapshot) { emit() })
			}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn := acceptTestWebSocket(t, w, r)
				if conn == nil {
					return
				}
				defer conn.CloseNow()
				_ = conn.Write(t.Context(), test.kind, []byte(test.raw))
				_, _, _ = conn.Read(t.Context())
			}))
			defer server.Close()
			called := false
			stream := test.stream
			if stream == nil {
				stream = func(c Client, ctx context.Context, emit func()) error {
					return c.StreamTraffic(ctx, func(Traffic) { emit() })
				}
			}
			err := stream(Client{BaseURL: server.URL}, t.Context(), func() { called = true })
			if err == nil || called || strings.Contains(err.Error(), "private-") {
				t.Fatalf("invalid frame published or leaked: called=%v err=%v", called, err)
			}
		})
	}
}

func TestWebSocketTransportIgnoresHTTPProxyAndRejectsRemoteControllers(t *testing.T) {
	var proxyCalls atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyCalls.Add(1); w.WriteHeader(500) }))
	defer proxy.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn := acceptTestWebSocket(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		_ = conn.Write(t.Context(), websocket.MessageText, []byte(`{"up":0,"down":0,"upTotal":0,"downTotal":0}`))
		_, _, _ = conn.Read(t.Context())
	}))
	defer server.Close()
	proxyRequest, _ := http.NewRequest(http.MethodGet, proxy.URL, nil)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyRequest.URL)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := (Client{BaseURL: server.URL, HTTP: &http.Client{Transport: transport}}).StreamTraffic(ctx, func(Traffic) { cancel() })
	if !errors.Is(err, context.Canceled) || proxyCalls.Load() != 0 {
		t.Fatalf("stream used HTTP proxy or lost cancellation: calls=%d err=%v", proxyCalls.Load(), err)
	}
	for _, address := range []string{"http://example.invalid:19090", "http://private-secret@127.0.0.1:19090", "http://127.0.0.1:19090/private-path", "http://127.0.0.1:19090?private-secret"} {
		err := (Client{BaseURL: address}).StreamTraffic(t.Context(), func(Traffic) {})
		if err == nil || strings.Contains(err.Error(), "private-") {
			t.Fatalf("invalid controller URL accepted or leaked: %v", err)
		}
	}
}

func TestWebSocketHandshakeFailuresAreRedactedAndCancellationClosesRequest(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("private-response-secret"))
			}))
			defer server.Close()
			err := (Client{BaseURL: server.URL, Secret: "private-controller-secret"}).StreamTraffic(t.Context(), func(Traffic) {})
			if err == nil || strings.Contains(err.Error(), "private-") {
				t.Fatalf("unsafe handshake error: %v", err)
			}
		})
	}
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(closed)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	err := (Client{BaseURL: server.URL}).StreamTraffic(ctx, func(Traffic) {})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("handshake lost cancellation: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("cancelled handshake left its request open")
	}
}

func TestWebSocketStreamsRequireConsumers(t *testing.T) {
	c := Client{BaseURL: "http://127.0.0.1:1"}
	for _, err := range []error{c.StreamTraffic(t.Context(), nil), c.StreamMemory(t.Context(), nil), c.StreamConnections(t.Context(), nil), c.StreamLogs(t.Context(), nil)} {
		if err == nil || !strings.Contains(err.Error(), "consumer") {
			t.Fatalf("nil consumer reached the network: %v", err)
		}
	}
}

func TestStalledMetricStreamsExpireWhileQuietLogsStayConnected(t *testing.T) {
	started := make(chan struct{}, 4)
	finished := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { finished <- struct{}{} }()
		conn := acceptTestWebSocket(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		started <- struct{}{}
		// A successful handshake followed by no samples is a stalled metric
		// source, but the same silence is normal on a log connection.
		_, _, _ = conn.Read(t.Context())
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := Client{BaseURL: server.URL}
	metrics, logs := make(chan error, 3), make(chan error, 1)
	go func() {
		metrics <- client.StreamTraffic(ctx, func(Traffic) { t.Error("stalled traffic fabricated a sample") })
	}()
	go func() {
		metrics <- client.StreamMemory(ctx, func(uint64) { t.Error("stalled memory fabricated a sample") })
	}()
	go func() {
		metrics <- client.StreamConnections(ctx, func(ConnectionSnapshot) { t.Error("stalled connections fabricated a sample") })
	}()
	go func() { logs <- client.StreamLogs(ctx, func(LogEntry) { t.Error("quiet logs fabricated an event") }) }()
	for range 4 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("fixture WebSocket did not connect")
		}
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for range 3 {
		select {
		case err := <-metrics:
			if err == nil || !strings.Contains(err.Error(), "timed out") {
				t.Fatalf("stalled metric did not report timeout: %v", err)
			}
		case <-deadline.C:
			t.Fatal("stalled metric WebSocket retained live state indefinitely")
		}
	}
	select {
	case err := <-logs:
		t.Fatalf("quiet log stream inherited a metric deadline: %v", err)
	default:
	}
	cancel()
	select {
	case err := <-logs:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("quiet log stream did not preserve cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("quiet log stream did not cancel")
	}
	for range 4 {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("stalled stream retained an open WebSocket")
		}
	}
}
