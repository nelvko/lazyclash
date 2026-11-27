package app

import (
	"errors"
	"net/http"
	"testing"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestUpdateActiveGeoChecksOwnedRunningService(t *testing.T) {
	for _, tc := range []struct {
		name, code                      string
		active, invalidUnit, changedPID bool
		calls                           int
	}{
		{"stopped", "diagnostic.geo.requireRunning", false, false, false, 0},
		{"unowned", "", true, true, false, 0},
		{"active", "", true, false, false, 1},
		{"restarted", "diagnostic.geo.updateCoreChanged", true, false, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, svc := installedApp(t)
			unit := &profileLifecycleService{Services: svc, status: service.Status{Active: tc.active, PID: 42, StartTimestampMonotonic: 120}}
			a.Service = unit
			if tc.invalidUnit {
				svc.verifyErr = errors.New("service unit belongs to another installation")
			}
			m, _, err := a.load()
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			a.HTTP.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/configs/geo" || r.URL.Host != m.Controller || r.Header.Get("Authorization") != "Bearer "+m.Secret {
					t.Fatal("request did not target the managed listener")
				}
				if tc.changedPID {
					unit.status.StartTimestampMonotonic++
				}
				return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Request: r}, nil
			})
			err = a.UpdateActiveGeo(t.Context())
			if calls != tc.calls || len(svc.actions) != 0 {
				t.Fatalf("unexpected actions: API=%d service=%v", calls, svc.actions)
			}
			switch {
			case tc.invalidUnit:
				if !errors.Is(err, svc.verifyErr) {
					t.Fatal(err)
				}
			case tc.code != "":
				var detail *diagnostic.Error
				if !errors.As(err, &detail) || detail.Code != tc.code {
					t.Fatal(err)
				}
			case err != nil:
				t.Fatal(err)
			}
		})
	}
}
