package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

func TestTerminalProxyEndpointRequiresOwnedRunningCoreAndObservedPort(t *testing.T) {
	for _, tc := range []struct {
		name, port            string
		active, foreign, want bool
	}{
		{"actual port differs from manifest", `,"mixed-port":18789`, true, false, true},
		{"missing port", "", true, false, false},
		{"disabled mixed listener", `,"mixed-port":0`, true, false, false},
		{"stopped core", `,"mixed-port":18789`, false, false, false},
		{"foreign service", `,"mixed-port":18789`, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = tc.active
			if tc.foreign {
				svc.verifyErr = errors.New("foreign service")
			}
			monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"mode":"rule","tun":{"enable":false}%s}`, tc.port)
			})
			probed := 0
			a.ProbeProxy = func(_ context.Context, port int) error { probed = port; return nil }
			err := a.locked(t.Context(), func() error {
				e, err := a.terminalEndpointLocked(t.Context())
				if err == nil && (e.Port != 18789 || e.Host != "127.0.0.1" || e.Namespace == "") {
					t.Fatal(e)
				}
				return err
			})
			if (err == nil) != tc.want {
				t.Fatal(err)
			}
			if tc.want && probed != 18789 {
				t.Fatal("manifest port was used instead of actual port", probed)
			}
			if !tc.want && probed != 0 {
				t.Fatal("unverified endpoint probed", probed)
			}
		})
	}
}

// A slow shell-state write must not let another application operation commit
// newer ports before this observation publishes its old port.
func TestTerminalProxyObservationPublishesBeforeReleasingApplicationLock(t *testing.T) {
	cache, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", cache)
	state := filepath.Join(cache, "lazyclash", "shell")
	if err := os.MkdirAll(filepath.Join(state, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	unlock, err := store.Lock(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	a, svc := installedApp(t)
	svc.active = true
	observed := make(chan struct{})
	monitorFixture(t, a, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/configs":
			fmt.Fprint(w, `{"mode":"rule","tun":{"enable":false},"mixed-port":18789}`)
			close(observed)
		case "/proxies":
			fmt.Fprint(w, `{"proxies":{}}`)
		default:
			monitorResponse(w, r, mihomo.ConnectionSnapshot{})
		}
	})
	done := make(chan error, 1)
	go func() { _, err := a.Snapshot(ctx); done <- err }()
	select {
	case <-observed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	acquired := make(chan struct{})
	writer := make(chan error, 1)
	go func() { writer <- a.locked(ctx, func() error { close(acquired); return nil }) }()
	select {
	case <-acquired:
		t.Fatal("observation released application lock before publishing endpoint")
	case <-time.After(150 * time.Millisecond):
	}
	unlock()
	unlock = nil
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-writer; err != nil {
		t.Fatal(err)
	}
}
