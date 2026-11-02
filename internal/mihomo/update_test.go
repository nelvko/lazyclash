package mihomo

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/downloadnet"
)

func TestLatestVersionUsesOfficialTextWithoutReleaseAPIOrAssetRewrite(t *testing.T) {
	for _, test := range []struct {
		channel Channel
		url     string
		body    string
		want    string
	}{
		{ChannelStable, stableVersionURL, "v1.19.33\n", "v1.19.33"},
		{ChannelStable, stableVersionURL, "1.19.33\r\n", "1.19.33"},
		{ChannelAlpha, alphaVersionURL, "alpha-69c1631\n", "alpha-69c1631"},
		{ChannelStable, stableVersionURL, "v2.0\n", "v2.0"},
	} {
		t.Run(string(test.channel)+"/"+test.want, func(t *testing.T) {
			requests := 0
			resolver := Resolver{HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.URL.String() != test.url {
					t.Errorf("version query used %s", r.URL.String())
				}
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 30*time.Second {
					t.Error("version query lacks bounded deadline")
				}
				return fixtureResponse(r, 200, "application/octet-stream", []byte(test.body)), nil
			})}}
			cfg := downloadnet.Config{Source: "mirror", Mirror: "https://mirror.invalid/{version}/{asset}"}
			got, err := resolver.LatestVersion(t.Context(), test.channel, cfg)
			if err != nil || got != test.want || requests != 1 {
				t.Fatalf("version=%q err=%v requests=%d", got, err, requests)
			}
		})
	}
}

func TestLatestVersionRejectsInvalidResponsesWithoutRetainingBody(t *testing.T) {
	for _, test := range []struct {
		name, media, body string
		status            int
	}{
		{"empty", "text/plain", " \r\n", 200},
		{"html", "text/plain", "<html>private-token</html>", 200},
		{"html-media", "text/html", "v1.19.33", 200},
		{"json-media", "application/problem+json", "v1.19.33", 200},
		{"error-text", "text/plain", "NotFound", 200},
		{"multiple", "text/plain", "v1.19.33\nv1.19.34", 200},
		{"control", "text/plain", "v1.19.33\x1b[0m", 200},
		{"path", "text/plain", "v1.19.33/../../private-token", 200},
		{"nul", "text/plain", "v1.19.33\x00", 200},
		{"long-token", "text/plain", "v" + strings.Repeat("1", 64), 200},
		{"long-body", "text/plain", strings.Repeat("v", 300), 200},
		{"http-error", "text/html", "private-token", 503},
		{"no-content", "text/plain", "", 204},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := Resolver{HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
				return fixtureResponse(r, test.status, test.media, []byte(test.body)), nil
			})}}
			if _, err := resolver.LatestVersion(t.Context(), ChannelStable, downloadnet.Config{}); err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("invalid response accepted or exposed: %v", err)
			}
		})
	}
	for _, channel := range []Channel{ChannelStable, ChannelAlpha} {
		body := "v1.19.33"
		if channel == ChannelStable {
			body = "alpha-69c1631"
		}
		resolver := Resolver{HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
			return fixtureResponse(r, 200, "text/plain", []byte(body)), nil
		})}}
		if _, err := resolver.LatestVersion(t.Context(), channel, downloadnet.Config{}); err == nil {
			t.Fatal("cross-channel response accepted")
		}
	}
}

func TestLatestVersionBoundsReading(t *testing.T) {
	consumed := 0
	resolver := Resolver{HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
		response := fixtureResponse(r, 200, "text/plain", nil)
		response.Body = &limitedProbeBody{Reader: *bytes.NewReader(bytes.Repeat([]byte("x"), 10000)), read: &consumed}
		return response, nil
	})}}
	if _, err := resolver.LatestVersion(t.Context(), ChannelStable, downloadnet.Config{}); err == nil || consumed != 257 {
		t.Fatalf("response unbounded: read=%d err=%v", consumed, err)
	}
}

func TestLatestVersionTimeoutCancellationAndUnknownChannel(t *testing.T) {
	resolver := Resolver{HTTP: &http.Client{Transport: downloadTransportFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := resolver.LatestVersion(ctx, ChannelStable, downloadnet.Config{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout lost: %v", err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	cancel()
	if _, err := resolver.LatestVersion(ctx, ChannelAlpha, downloadnet.Config{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if _, err := resolver.LatestVersion(t.Context(), ChannelUnknown, downloadnet.Config{}); err == nil {
		t.Fatal("unknown channel accepted")
	}
}
