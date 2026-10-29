package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

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

func TestLogsCLICollectsOnlyBoundedRedactedLiveEntries(t *testing.T) {
	a := logsBackend(t)
	a.HTTP = &http.Client{Transport: rulesTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"version":"v1.19.33"}`
		if r.URL.Path == "/logs" {
			body = "{\"type\":\"warning\",\"payload\":\"source https://user:private-token@example.invalid/private-path?q=private-query\"}\n{\"type\":\"info\",\"payload\":\"safe message\"}\n"
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	output, err := executeLogs(t, a, "--lines", "2")
	var entries []mihomo.LogEntry
	if err != nil || json.Unmarshal([]byte(output), &entries) != nil || len(entries) != 2 || strings.Contains(output, "private-") {
		t.Fatalf("bad bounded logs: %s %v", output, err)
	}
	if entries[0].Time.IsZero() || entries[1].Message != "safe message" {
		t.Fatal("logs lack observation times or changed safe messages")
	}
}

type quietLogBody struct{ ctx context.Context }

func (b quietLogBody) Read(p []byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }
func (b quietLogBody) Close() error               { return nil }

func TestLogsCLIDistinguishesQuietObservationFromUnreachableAPI(t *testing.T) {
	for _, ready := range []bool{true, false} {
		t.Run(fmt.Sprint(ready), func(t *testing.T) {
			a := logsBackend(t)
			a.HTTP = &http.Client{Transport: rulesTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/version" && ready {
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"version":"v1.19.33"}`)), Request: r}, nil
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: quietLogBody{r.Context()}, Request: r}, nil
			})}
			output, err := executeLogs(t, a, "--duration", "30ms")
			if ready && (err != nil || strings.TrimSpace(output) != "[]") {
				t.Fatalf("quiet window treated as failure: %q %v", output, err)
			}
			if !ready && (err == nil || output != "") {
				t.Fatalf("unreachable API treated as valid empty logs: %q %v", output, err)
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
