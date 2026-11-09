package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/coder/websocket"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func logsBackend(t *testing.T) *app.App {
	t.Helper()
	a, svc := installBackend(t)
	a.Download = func(ctx context.Context, destination string, _ func(string)) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return os.WriteFile(destination, []byte("fixture core"), 0700)
	}
	if err := a.Install(t.Context(), "", nil); err != nil {
		t.Fatal(err)
	}
	a.Service = rulesService{svc}
	return a
}

func executeLogs(t *testing.T, a *app.App, args ...string) (string, error) {
	t.Helper()
	cmd := newLogsCommand(func() *app.App { return a })
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := cmd.ExecuteContext(t.Context())
	return out.String(), err
}

func logsControllerFixture(t *testing.T, a *app.App, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	t.Cleanup(transport.CloseIdleConnections)
	a.HTTP = &http.Client{Transport: rulesTransport(func(request *http.Request) (*http.Response, error) {
		copy := request.Clone(request.Context())
		destination := *request.URL
		destination.Scheme, destination.Host = target.Scheme, target.Host
		copy.URL = &destination
		return transport.RoundTrip(copy)
	})}
}

func TestLogsCLICollectsOnlyBoundedRedactedLiveEntries(t *testing.T) {
	a := logsBackend(t)
	logsControllerFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			fmt.Fprint(w, `{"version":"v1.19.33"}`)
			return
		}
		if r.URL.Path != "/logs" || r.URL.Query().Get("level") != "info" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("unexpected or unauthenticated logs subscription: %s", r.URL)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		for _, raw := range []string{
			`{"type":"warning","payload":"source https://user:private-token@example.invalid/private-path?q=private-query"}`,
			`{"type":"info","payload":"safe message"}`,
		} {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(raw)); err != nil {
				return
			}
		}
		<-conn.CloseRead(r.Context()).Done()
	})
	output, err := executeLogs(t, a, "--lines", "2")
	var entries []mihomo.LogEntry
	if err != nil || json.Unmarshal([]byte(output), &entries) != nil || len(entries) != 2 || strings.Contains(output, "private-") {
		t.Fatalf("bad bounded logs: %s %v", output, err)
	}
	if entries[0].Time.IsZero() || entries[1].Message != "safe message" {
		t.Fatal("logs lack observation times or changed safe messages")
	}
}

func TestLogsCLIDistinguishesQuietObservationFromUnreachableAPI(t *testing.T) {
	for _, ready := range []bool{true, false} {
		t.Run(fmt.Sprint(ready), func(t *testing.T) {
			a := logsBackend(t)
			var subscriptions atomic.Int32
			logsControllerFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/version" {
					if ready {
						fmt.Fprint(w, `{"version":"v1.19.33"}`)
					} else {
						<-r.Context().Done()
					}
					return
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				subscriptions.Add(1)
				<-conn.CloseRead(r.Context()).Done()
			})
			output, err := executeLogs(t, a, "--duration", "200ms")
			if ready && (err != nil || strings.TrimSpace(output) != "[]" || subscriptions.Load() != 1) {
				t.Fatalf("quiet window treated as failure: %q %v", output, err)
			}
			if !ready && (err == nil || output != "" || subscriptions.Load() != 0) {
				t.Fatalf("unreachable API treated as valid empty logs: %q %v", output, err)
			}
		})
	}
}

func TestLogsCLIDoesNotTreatAnUnestablishedWebSocketAsQuietLogs(t *testing.T) {
	for _, stalled := range []bool{false, true} {
		t.Run(fmt.Sprint(stalled), func(t *testing.T) {
			a := logsBackend(t)
			var attempts atomic.Int32
			logsControllerFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/version" {
					fmt.Fprint(w, `{"version":"v1.19.33"}`)
					return
				}
				attempts.Add(1)
				if stalled {
					<-r.Context().Done()
					return
				}
				w.WriteHeader(http.StatusBadGateway)
				fmt.Fprint(w, "private-handshake-response")
			})
			output, err := executeLogs(t, a, "--duration", "200ms")
			if err == nil || output != "" || attempts.Load() != 1 || strings.Contains(err.Error(), "private-") {
				t.Fatalf("failed WS handshake became a valid empty window: %q %v, attempts=%d", output, err, attempts.Load())
			}
		})
	}
}

func TestLogsCLIRejectsInvalidBoundsAndUninstalledCore(t *testing.T) {
	a, _ := installBackend(t)
	for _, args := range [][]string{nil, {"--lines", "0"}, {"--lines", "501"}, {"--duration", "0s"}, {"--duration", "61s"}, {"unexpected"}} {
		if out, err := executeLogs(t, a, args...); err == nil || out != "" {
			t.Fatalf("invalid/unavailable logs succeeded: %v %q %v", args, out, err)
		}
	}
}
