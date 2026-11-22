package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestProfileReloadRestoresPinnedConfigurationAndKeepsStoppedServicesStopped(t *testing.T) {
	for _, tc := range []struct {
		name            string
		rejectOld, stop bool
		wantCode        string
	}{
		{"running", false, false, "diagnostic.profile.reloadRestored"},
		{"stopped externally during update", false, true, "diagnostic.profile.reloadRestoredStopped"},
		{"interrupted recovery is retried", true, false, "diagnostic.profile.reloadRecovery"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = true
			m, old, err := a.load()
			if err != nil {
				t.Fatal(err)
			}
			_, cfgPath, _ := a.paths()
			oldBytes := read(t, cfgPath)
			newBytes := append([]byte("# changed\n"), oldBytes...)
			var bodies []string
			rejectOld := tc.rejectOld
			a.HTTP = &http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
				code, body := 200, `{"version":"v1.19.33"}`
				if req.Method == http.MethodPut {
					request, err := io.ReadAll(req.Body)
					if err != nil || req.URL.RequestURI() != "/configs?force=true" || string(request) != "{}" || req.Header.Get("Authorization") != "Bearer "+m.Secret {
						t.Fatal("unexpected reload request", req.Method, req.URL.Path, err)
					}
					loaded := string(read(t, cfgPath))
					bodies = append(bodies, loaded)
					if loaded == string(newBytes) || rejectOld {
						code = http.StatusBadRequest
					}
					if tc.stop {
						svc.active = false
					}
				}
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Request: req, Header: make(http.Header)}, nil
			})}
			err = a.publishProfileRefresh(t.Context(), old, m, newBytes)
			var diag *diagnostic.Error
			if !errors.As(err, &diag) || diag.Code != tc.wantCode {
				t.Fatalf("unexpected result: %v", err)
			}
			journal := filepath.Join(a.Home, "apply-pending.json")
			if tc.rejectOld {
				if _, err := os.Stat(journal); err != nil {
					t.Fatal("lost retry journal", err)
				}
				rejectOld = false
				if err := a.locked(t.Context(), func() error { return nil }); err != nil {
					t.Fatal("second invocation failed to restore", err)
				}
			}
			assertMissing(t, journal)
			if _, target, err := a.load(); target != old || err != nil || len(svc.actions) > 0 {
				t.Fatalf("unsafe rollback: %s %v %v", target, svc.actions, err)
			}
			if bodies[0] != string(newBytes) {
				t.Fatal("candidate was never published")
			}
			if tc.stop && len(bodies) != 1 {
				t.Fatal("stopped core received a rollback request")
			}
			if !tc.stop && (len(bodies) < 2 || bodies[len(bodies)-1] != string(oldBytes)) {
				t.Fatal("old config not reloaded")
			}
		})
	}
}

func TestUnchangedEffectiveProfileAdvancesRecipeWithoutContactingService(t *testing.T) {
	a, svc := installedApp(t)
	m, old, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	m.Build = &buildRecord{Profile: "source"}
	_, config, _ := a.paths()
	previous := read(t, config)
	svc.verifyErr = errors.New("service must never be contacted")
	a.HTTP = &http.Client{Transport: fixtureTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("unchanged settings contacted core")
		return nil, nil
	})}
	if err := a.publishProfileRefresh(context.Background(), old, m, previous); err != nil {
		t.Fatal(err)
	}
	fresh, current, err := a.load()
	if err != nil || fresh.Build.Profile != "source" || current == old || fresh.SHA != m.SHA || len(svc.actions) != 0 {
		t.Fatal("recipe not advanced", err)
	}
}

func TestLostProfileReloadReplyFencesOnlyTheUncertainProcess(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		duringRequest          func(*service.Status)
		duringTryRestart       func(*service.Status)
		stopIntent             bool
		wantRestarts, wantPuts int
		wantCode               string
	}{
		{name: "still-running", wantRestarts: 1, wantPuts: 1, wantCode: "diagnostic.profile.reloadRestoredNewProcess"},
		{name: "new-process", duringRequest: func(s *service.Status) { s.PID++; s.StartTimestampMonotonic++ }, wantPuts: 2, wantCode: "diagnostic.profile.reloadRestoredNewProcess"},
		{name: "process-stopped", duringRequest: func(s *service.Status) { *s = service.Status{State: "inactive"} }, wantPuts: 1, wantCode: "diagnostic.profile.reloadRestoredStopped"},
		{name: "stopped-between-inspect-and-try-restart", duringTryRestart: func(s *service.Status) { *s = service.Status{State: "inactive"} }, wantRestarts: 1, wantPuts: 1, wantCode: "diagnostic.profile.reloadRestoredStopped"},
		{name: "stop-already-requested", stopIntent: true, wantPuts: 1, wantCode: "diagnostic.profile.reloadRecovery"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, svc := installedApp(t)
			unit := &profileLifecycleService{Services: svc, status: service.Status{State: "active", Active: true, PID: 42, StartTimestampMonotonic: 1234}}
			a.Service = unit
			m, old, err := a.load()
			if err != nil {
				t.Fatal(err)
			}
			_, cfgPath, _ := a.paths()
			original := read(t, cfgPath)
			newBytes := append([]byte("# changed\n"), original...)
			puts := 0
			svc.actionHook = func(_ context.Context, action string) error {
				if action != "try-restart" || string(read(t, cfgPath)) != string(original) {
					t.Fatal("unexpected recovery control", action)
				}
				if tc.duringTryRestart != nil {
					tc.duringTryRestart(&unit.status)
				} else {
					unit.status.PID++
					unit.status.StartTimestampMonotonic++
				}
				return nil
			}
			a.HTTP = &http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodPut {
					puts++
					if puts == 1 {
						if tc.stopIntent {
							if err := a.rememberStop(); err != nil {
								t.Fatal(err)
							}
						}
						if tc.duringRequest != nil {
							tc.duringRequest(&unit.status)
						}
						return nil, io.ErrUnexpectedEOF
					}
					if string(read(t, cfgPath)) != string(original) {
						t.Fatal("replacement was not given the restored configuration")
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"version":"v1.19.33"}`)), Header: make(http.Header), Request: req}, nil
			})}
			err = a.publishProfileRefresh(t.Context(), old, m, newBytes)
			var diag *diagnostic.Error
			if !errors.As(err, &diag) || diag.Code != tc.wantCode || len(svc.actions) != tc.wantRestarts || puts != tc.wantPuts {
				t.Fatalf("recovery result: err=%v actions=%v puts=%d", err, svc.actions, puts)
			}
			_, current, err := a.load()
			if err != nil || current != old {
				t.Fatal("previous generation not restored", err)
			}
			if tc.stopIntent {
				var journal pending
				if json.Unmarshal(read(t, filepath.Join(a.Home, "apply-pending.json")), &journal) != nil || journal.Reloading == nil {
					t.Fatal("lost outstanding reload journal")
				}
				unit.status = service.Status{State: "inactive"}
				if err := a.recover(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
		})
	}
}
