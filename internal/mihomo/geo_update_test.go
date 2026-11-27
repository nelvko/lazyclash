package mihomo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

func TestUpdateGeoUsesAuthenticatedLocalControllerAndRedactsErrors(t *testing.T) {
	const sensitive = "https://private.example/geo?token=private"
	for _, tc := range []struct {
		name   string
		status int
		code   string
	}{
		{"success", http.StatusNoContent, ""},
		{"credentials", http.StatusUnauthorized, "diagnostic.api.httpCredentials"},
		{"updater error", http.StatusInternalServerError, "diagnostic.api.http"},
		{"redirect", http.StatusFound, "diagnostic.api.http"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := Client{BaseURL: "http://127.0.0.1:9090", Secret: "fixture", HTTP: &http.Client{Timeout: time.Second}}
			client.HTTP.Transport = downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) < time.Minute || time.Until(deadline) > 2*time.Minute {
					t.Fatal("unexpected GEO update deadline", deadline)
				}
				if r.Method != http.MethodPost || r.URL.RequestURI() != "/configs/geo" || r.URL.Host != "127.0.0.1:9090" ||
					r.Header.Get("Authorization") != "Bearer fixture" || r.ContentLength != 0 {
					t.Fatal("unexpected GEO update request")
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(sensitive)),
					Header: http.Header{"Location": []string{sensitive}}, Request: r}, nil
			})
			err := client.UpdateGeo(t.Context())
			var detail *diagnostic.Error
			if tc.code == "" && err != nil || tc.code != "" && (!errors.As(err, &detail) || detail.Code != tc.code || strings.Contains(err.Error(), sensitive)) {
				t.Fatalf("unexpected update result: %v", err)
			}
			if calls != 1 || client.HTTP.Timeout != time.Second {
				t.Fatal("followed a redirect or changed the shared HTTP client")
			}
		})
	}
}

func TestUpdateGeoDistinguishesPreflightCancellationFromAbandonedWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, mode := range []string{"preflight", "cancel after dispatch", "update timeout"} {
			ctx, cancel := context.WithCancel(t.Context())
			before := time.Now()
			calls := 0
			client := Client{BaseURL: "http://127.0.0.1:9090", HTTP: &http.Client{}}
			client.HTTP.Transport = downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if mode == "cancel after dispatch" {
					cancel()
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})
			if mode == "preflight" {
				cancel()
			}
			err := client.UpdateGeo(ctx)
			cancel()
			want := context.Canceled
			if mode == "update timeout" {
				want = context.DeadlineExceeded
				if time.Since(before) != 2*time.Minute {
					t.Fatalf("wrong waiting interval: %s", time.Since(before))
				}
			}
			if !errors.Is(err, want) {
				t.Fatalf("%s: unexpected result %v", mode, err)
			}
			if mode == "preflight" {
				if calls != 0 || !diagnostic.CancellationOnly(err) {
					t.Fatal("canceled caller dispatched a request or reported a background update")
				}
			} else if calls != 1 || !strings.Contains(err.Error(), "may continue updating") {
				t.Fatalf("%s: lost background-update warning: %v", mode, err)
			}
		}
	})
}
