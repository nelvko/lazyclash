package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

type rulesService struct{ *installService }

func (s rulesService) Inspect(ctx context.Context) (service.Status, error) {
	return service.Status{State: "active", Active: true, PID: 123}, ctx.Err()
}

type rulesTransport func(*http.Request) (*http.Response, error)

func (f rulesTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRulesCLIUsesSharedReadOnlyUseCaseAndPreservesMatchingOrder(t *testing.T) {
	a, svc := installBackend(t)
	local := filepath.Join(t.TempDir(), "mihomo")
	if err := os.WriteFile(local, []byte("fixture executable"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.Install(t.Context(), local, nil); err != nil {
		t.Fatal(err)
	}
	a.Service = rulesService{svc}
	config := filepath.Join(a.Home, "installation", "current", "config.yaml")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	status, body := 200, `{"rules":[{"index":0,"type":"DomainSuffix","payload":"z.example.invalid","proxy":"PROXY","size":-1},{"index":1,"type":"Domain","payload":"a.example.invalid","proxy":"REJECT","size":-1},{"index":2,"type":"Match","payload":"","proxy":"DIRECT","size":-1}]}`
	a.HTTP = &http.Client{Transport: rulesTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/rules" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("unexpected or unauthenticated request: %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})}
	execute := func() (string, error) {
		cmd := newRulesCommand(func() *app.App { return a })
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		var output bytes.Buffer
		cmd.SetOut(&output)
		err := cmd.ExecuteContext(t.Context())
		return output.String(), err
	}
	output, err := execute()
	var rules []mihomo.Rule
	if err != nil || json.Unmarshal([]byte(output), &rules) != nil || len(rules) != 3 || rules[0].Payload != "z.example.invalid" || rules[1].Payload != "a.example.invalid" || rules[2].Type != "Match" || rules[2].Payload != "" {
		t.Fatalf("wrong effective rules: %s (%v)", output, err)
	}
	status, body = 502, "private-response-token"
	if output, err := execute(); err == nil || output != "" || strings.Contains(err.Error(), "private-response-token") {
		t.Fatalf("failure printed a stale list or private response: %q %v", output, err)
	}
	status, body = 200, `{"rules":[]}`
	if output, err := execute(); err != nil || strings.TrimSpace(output) != "[]" {
		t.Fatalf("valid empty list was not distinguished from failure: %q %v", output, err)
	}
	after, err := os.ReadFile(config)
	if err != nil || !bytes.Equal(before, after) || svc.installs != 1 || svc.actions != 0 {
		t.Fatal("rules command modified the configuration or service", err)
	}
}

func TestRulesCLIRejectsUnavailableCoreAndUnexpectedArguments(t *testing.T) {
	a, svc := installBackend(t)
	for _, args := range [][]string{nil, {"unexpected"}} {
		cmd := newRulesCommand(func() *app.App { return a })
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(t.Context()); err == nil || output.Len() != 0 {
			t.Fatalf("unavailable data appeared as a valid empty list: %q %v", output.String(), err)
		}
	}
	if svc.installs != 0 || svc.actions != 0 {
		t.Fatal("read-only command started or installed a core")
	}
}
