package mihomo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModeRequiresObservedAPIState(t *testing.T) {
	for _, scenario := range []string{"success", "ignored-patch", "patch-failure", "invalid-state"} {
		t.Run(scenario, func(t *testing.T) {
			mode := "rule"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/configs" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method == http.MethodPatch {
					var payload map[string]string
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload) != 1 || payload["mode"] != "direct" {
						t.Errorf("unexpected PATCH payload: %v %v", payload, err)
					}
					if scenario == "patch-failure" {
						w.WriteHeader(500)
						return
					}
					if scenario != "ignored-patch" {
						mode = payload["mode"]
					}
					w.WriteHeader(204)
					return
				}
				if scenario == "invalid-state" {
					_, _ = w.Write([]byte(`{"mode":"direct"}`))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"mode": mode, "tun": map[string]bool{"enable": false}})
			}))
			defer server.Close()
			err := (Client{BaseURL: server.URL}).SetMode(t.Context(), "direct")
			if (err == nil) != (scenario == "success") {
				t.Fatalf("incorrect confirmation result: %v", err)
			}
		})
	}
}

func TestSelectionRequiresObservedAPIState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(204)
			return
		}
		_, _ = w.Write([]byte(`{"proxies":{"PROXY":{"type":"Selector","now":"DIRECT","all":["DIRECT","REJECT"]}}}`))
	}))
	defer server.Close()
	if err := (Client{BaseURL: server.URL}).Select(t.Context(), "PROXY", "REJECT"); err == nil {
		t.Fatal("accepted unchanged observed selection")
	}
}
