package app

import (
	"bytes"
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSubscriptionErrorResponsesNeverReplacePublishedInputsOrRuntime(t *testing.T) {
	for _, response := range []struct{ name, raw string }{
		{name: "error-json", raw: `{"error":"private-content-token","code":403}`},
		{name: "error-yaml", raw: "error: private-content-token\n"},
		{name: "foreign-outbounds", raw: "outbounds: [{type: direct, tag: private-content-token}]\n"},
		{name: "foreign-dns", raw: "outbounds: null\ndns: {servers: [{tag: private-content-token, type: local}]}\n"},
	} {
		t.Run(response.name, func(t *testing.T) {
			a, svc := installedApp(t)
			var content atomic.Value
			content.Store([]byte("rules: ['MATCH,DIRECT']\n"))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(content.Load().([]byte))
			}))
			defer server.Close()
			a.HTTP = server.Client()
			address := server.URL + "/private-source-token?token=private-source-token"
			if err := a.AddSubscription(t.Context(), "saved", address); err != nil {
				t.Fatal(err)
			}
			if err := a.Apply(t.Context(), "saved"); err != nil {
				t.Fatal(err)
			}
			svc.active = true
			beforeTree, err := installationTree(filepath.Join(a.Home, "installation"))
			if err != nil {
				t.Fatal(err)
			}
			indexPath := filepath.Join(a.Home, "configuration", "library.json")
			beforeIndex := read(t, indexPath)
			snapshotPath := savedSourcePath(t, a, "saved")
			beforeSnapshot := read(t, snapshotPath)
			snapshotsPath := filepath.Dir(snapshotPath)
			beforeEntries, err := os.ReadDir(snapshotsPath)
			if err != nil {
				t.Fatal(err)
			}
			validations := 0
			a.Validate = func(context.Context, string, string, string) error { validations++; return nil }
			content.Store([]byte(response.raw))
			operations := []func() error{
				func() error { return a.Refresh(t.Context(), "saved") },
				func() error { return a.Add(t.Context(), "rejected-profile", address) },
				func() error { return a.AddSource(t.Context(), "rejected-source", address) },
			}
			for _, operation := range operations {
				err := operation()
				if err == nil || strings.Contains(err.Error(), "private-content-token") || strings.Contains(err.Error(), "private-source-token") || strings.Contains(err.Error(), server.URL) {
					t.Fatalf("error response was accepted or exposed sensitive input: %v", err)
				}
			}
			afterTree, err := installationTree(filepath.Join(a.Home, "installation"))
			if err != nil || !maps.Equal(beforeTree, afterTree) || !svc.active || len(svc.actions) != 0 || validations != 0 {
				t.Fatal("format rejection reached native validation or affected the running configuration", err)
			}
			afterEntries, err := os.ReadDir(snapshotsPath)
			if err != nil || len(afterEntries) != len(beforeEntries) || !bytes.Equal(beforeIndex, read(t, indexPath)) || !bytes.Equal(beforeSnapshot, read(t, snapshotPath)) {
				t.Fatal("error response changed published or immutable input data", err)
			}
			assertLibraryEntryMissing(t, a, "rejected-profile")
			assertLibraryEntryMissing(t, a, "rejected-source")
		})
	}
}
