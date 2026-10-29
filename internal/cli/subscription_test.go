package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
)

func runSubscriptionCLI(t *testing.T, backend *app.App, args ...string) (string, error) {
	t.Helper()
	cmd := newSubscriptionCommand(func() *app.App { return backend })
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	return output.String(), err
}

func TestSubscriptionCLIRefreshFailurePreservesRevisionAndHidesCredentials(t *testing.T) {
	a, service := installBackend(t)
	raw := "proxies: []\nrules: ['MATCH,DIRECT']\n"
	var status atomic.Int32
	status.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte(raw))
	}))
	defer server.Close()
	source := server.URL + "/subscription?token=private-subscription-token"
	if _, err := runSubscriptionCLI(t, a, "add", "remote", source); err != nil {
		t.Fatal(err)
	}
	output, err := runSubscriptionCLI(t, a, "list")
	var sources []app.SourceInfo
	if err != nil || json.Unmarshal([]byte(output), &sources) != nil || len(sources) != 1 || sources[0].Name != "remote" || sources[0].Revision != configDigest(raw) || !sources[0].Refreshable {
		t.Fatalf("source listing: %s %v", output, err)
	}
	if strings.Contains(output, "private-subscription-token") || strings.Contains(output, server.URL) {
		t.Fatal("source listing exposed subscription credentials")
	}
	status.Store(http.StatusServiceUnavailable)
	output, err = runSubscriptionCLI(t, a, "refresh", "remote")
	if err == nil {
		t.Fatal("HTTP failure was accepted")
	}
	if strings.Contains(output+err.Error(), "private-subscription-token") || strings.Contains(output+err.Error(), server.URL) {
		t.Fatal("refresh failure exposed subscription credentials")
	}
	current, listErr := a.ListSources(t.Context())
	if listErr != nil || len(current) != 1 || current[0].Revision != sources[0].Revision {
		t.Fatalf("failed download replaced old source: %+v %v", current, listErr)
	}
	if service.installs != 0 || service.actions != 0 {
		t.Fatal("subscription operation touched core lifecycle")
	}
	if _, err := runSubscriptionCLI(t, a, "delete", "remote"); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionCommandRegisteredWithoutInstalledCore(t *testing.T) {
	root := New()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"--home", t.TempDir(), "subscription", "list"})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != "[]" {
		t.Fatal(output.String())
	}
}
