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
	"unicode"
	"unicode/utf8"

	"github.com/coder/websocket"
)

func TestLogStreamAuthenticatesRedactsAndCancels(t *testing.T) {
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		if r.Method != "GET" || r.URL.Path != "/logs" || r.URL.Query().Get("level") != "info" || r.Header.Get("Authorization") != "Bearer private-controller-secret" {
			t.Error("log request did not use the authenticated live endpoint")
		}
		conn := acceptTestWebSocket(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		raw, _ := json.Marshal(map[string]string{"type": "warning", "payload": "\x1b]52;c;dG9rZW4=\a request https://user:private-url-token@example.invalid/private-path?unknown=private-query-token failed; private-controller-secret Authorization: Bearer private-bearer-token"})
		_ = conn.Write(t.Context(), websocket.MessageText, raw)
		_, _, _ = conn.Read(t.Context())
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var entries []LogEntry
	err := (Client{BaseURL: server.URL, Secret: "private-controller-secret"}).StreamLogs(ctx, func(entry LogEntry) {
		entries = append(entries, entry)
		cancel()
	})
	if !errors.Is(err, context.Canceled) || len(entries) != 1 || entries[0].Level != "warning" || entries[0].Time.IsZero() {
		t.Fatalf("stream observation/cancel failed: %+v %v", entries, err)
	}
	for _, private := range []string{"private-", "\x1b", "dG9rZW4="} {
		if strings.Contains(entries[0].Message, private) {
			t.Fatalf("log entry retained private data %q", private)
		}
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("cancelled stream left its HTTP request open")
	}
}

func TestLogStreamRejectsBadOrOversizedEventsWithoutEchoingBody(t *testing.T) {
	for name, payload := range map[string]string{
		"html": "<html>private-body</html>", "null": "null", "missing": `{"type":"info"}`,
		"bad-type":    `{"type":"private-body","payload":"message"}`,
		"bad-payload": `{"type":"info","payload":123}`,
		"oversized":   `{"type":"info","payload":"private-body` + strings.Repeat("x", maxLogFrame) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn := acceptTestWebSocket(t, w, r)
				if conn == nil {
					return
				}
				defer conn.CloseNow()
				_ = conn.Write(t.Context(), websocket.MessageText, []byte(payload))
				_, _, _ = conn.Read(t.Context())
			}))
			defer server.Close()
			called := false
			err := (Client{BaseURL: server.URL}).StreamLogs(t.Context(), func(LogEntry) { called = true })
			if err == nil || called || strings.Contains(err.Error(), "private-body") {
				t.Fatalf("invalid response was published or exposed: called=%v error=%v", called, err)
			}
		})
	}
}

func TestQuietLogStreamHasNoWholeResponseTimeoutAndStillCancels(t *testing.T) {
	started, finished := make(chan struct{}), make(chan struct{})
	ready := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		conn := acceptTestWebSocket(t, w, r)
		if conn == nil {
			return
		}
		defer conn.CloseNow()
		close(started)
		_, _, _ = conn.Read(t.Context())
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- (Client{BaseURL: server.URL, HTTP: &http.Client{Timeout: 10 * time.Millisecond}, StreamReady: func() { ready <- struct{}{} }}).StreamLogs(ctx, func(LogEntry) { t.Error("quiet stream fabricated a log event") })
	}()
	<-started
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("quiet WebSocket did not publish its successful handshake")
	}
	select {
	case err := <-done:
		t.Fatalf("quiet live connection inherited buffered API timeout: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("quiet stream did not cancel")
	}
	<-finished
	if len(ready) != 0 {
		t.Fatal("live stream published more than one handshake")
	}
}

func TestLogStreamNeverRedirectsCredentials(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/private-redirect-token", http.StatusFound)
	}))
	defer server.Close()
	err := (Client{BaseURL: server.URL, Secret: "private-controller-secret"}).StreamLogs(t.Context(), func(LogEntry) {})
	if called || err == nil || strings.Contains(err.Error(), "private-") {
		t.Fatalf("unsafe redirect handling: called=%v error=%v", called, err)
	}
}

func TestSafeLogMessageConservativelyRedactsCredentialsAndBoundsUTF8(t *testing.T) {
	for _, message := range []string{
		`password = "private-password" other=value`, `{"token":"private-token","other":"value"}`,
		`{\"token\":\"private-escaped\"}`, `https:\/\/example.invalid\/private-path`,
		"custom+transport://user:private-custom@host.invalid/path?unknown=value", "wss://host.invalid/private-path",
		"Bearer private-bearer-token", "Basic cHJpdmF0ZS1iYXNpYw==", "user:private-userinfo@example.invalid:8080",
		"subscription https://example.invalid/private-path?unknown=private-query", "ss://private-encoded@host.invalid",
		"private-controller-secret", "private-controller-\x1b[31msecret", "api_key: private-api-key",
	} {
		got := SafeLogMessage(message, "private-controller-secret")
		if strings.Contains(got, "private-") || strings.Contains(got, "cHJpdmF0ZS1iYXNpYw==") {
			t.Fatalf("unredacted secret in %q", got)
		}
	}
	got := SafeLogMessage(strings.Repeat("中文", 3000)+"\n\x1b[31m\u202e", "")
	if len(got) > MaxLogMessage+len("…") || !utf8.ValidString(got) || strings.IndexFunc(got, unicode.IsControl) >= 0 || strings.ContainsRune(got, '\u202e') {
		t.Fatal("display message was unbounded, invalid UTF-8 or retained controls")
	}
}
