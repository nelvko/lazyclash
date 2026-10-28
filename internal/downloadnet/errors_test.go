package downloadnet

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFailureTypedCategoriesHideOriginalDetails(t *testing.T) {
	private := errors.New("private-error-url https://private-user:private-pass@private-host/private-path?token=private-query")
	cases := []struct {
		name, kind string
		cause      error
	}{
		{"DNS", "DNS lookup", &net.DNSError{Err: "private-dns-message", Name: "private-host", Server: "private-server"}},
		{"DNS timeout", "DNS lookup", &net.DNSError{Err: "private-dns-timeout", Name: "private-host", IsTimeout: true}},
		{"unknown authority", "TLS certificate", x509.UnknownAuthorityError{}},
		{"hostname", "TLS certificate", x509.HostnameError{Host: "private-host"}},
		{"invalid certificate", "TLS certificate", x509.CertificateInvalidError{Detail: "private-certificate-detail"}},
		{"system roots", "TLS certificate", x509.SystemRootsError{Err: private}},
		{"verification wrapper", "TLS certificate", &tls.CertificateVerificationError{Err: private}},
		{"insecure algorithm", "TLS certificate", x509.InsecureAlgorithmError(x509.MD5WithRSA)},
		{"certificate constraints", "TLS certificate", x509.ConstraintViolationError{}},
		{"record header", "TLS handshake", tls.RecordHeaderError{Msg: "private-record-message"}},
		{"TLS alert", "TLS handshake", tls.AlertError(40)},
		{"ECH rejection", "TLS handshake", &tls.ECHRejectionError{RetryConfigList: []byte("private-retry-config")}},
		{"timeout", "timeout", timeoutFixture{}},
		{"proxy", "proxy connection", &net.OpError{Op: "proxyconnect", Net: "tcp", Err: private}},
		{"SOCKS", "proxy connection", &net.OpError{Op: "socks connect", Net: "tcp", Err: private}},
		{"other network", "network connection", private},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := &url.Error{Op: "Get", URL: "https://private-host/private-token", Err: fmt.Errorf("private-wrapper: %w", tc.cause)}
			safe := Failure(t.Context(), "version query", wrapped)
			var classified *Error
			if !errors.As(safe, &classified) || classified.Kind != tc.kind {
				t.Fatalf("category=%v want=%s", safe, tc.kind)
			}
			if strings.Contains(safe.Error(), "private-") {
				t.Fatal("typed classification exposed original material", safe)
			}
			if errors.Unwrap(safe) != nil {
				t.Fatal("sensitive original error remained attached")
			}
		})
	}
}

type timeoutFixture struct{}

func (timeoutFixture) Error() string   { return "private-transport-timeout" }
func (timeoutFixture) Timeout() bool   { return true }
func (timeoutFixture) Temporary() bool { return false }

func TestFailureSeparatesCancellationAndDeadlineAndKeepsErrorsIs(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	expired, expireCancel := context.WithDeadline(t.Context(), time.Unix(1, 0))
	defer expireCancel()
	for _, tc := range []struct {
		ctx            context.Context
		cause, errorIs error
		kind           string
	}{
		{canceled, errors.New("private-cancel-reason"), context.Canceled, "canceled"},
		{expired, errors.New("private-deadline-reason"), context.DeadlineExceeded, "timeout"},
		{t.Context(), fmt.Errorf("private-wrapper: %w", context.Canceled), context.Canceled, "canceled"},
		{t.Context(), fmt.Errorf("private-wrapper: %w", context.DeadlineExceeded), context.DeadlineExceeded, "timeout"},
	} {
		safe := Failure(tc.ctx, "release download", tc.cause)
		var classified *Error
		if !errors.As(safe, &classified) || classified.Kind != tc.kind || !errors.Is(safe, tc.errorIs) {
			t.Fatalf("lost precise cancellation: %v", safe)
		}
		if strings.Contains(safe.Error(), "private-") {
			t.Fatal("cancellation exposed raw error", safe)
		}
	}
}

func TestHTTPFailureCategoriesKeepStatusAndAvoidUnsupportedAssumptions(t *testing.T) {
	for _, tc := range []struct {
		status int
		kind   string
	}{
		{http.StatusUnauthorized, "HTTP response"},
		{http.StatusForbidden, "permission denied or rate limit"},
		{http.StatusNotFound, "HTTP response"},
		{http.StatusProxyAuthRequired, "proxy authentication"},
		{http.StatusTooManyRequests, "rate limit"},
		{http.StatusBadGateway, "HTTP response"},
	} {
		safe := HTTPError("official version query", tc.status)
		wrapped := Failure(t.Context(), "file test", fmt.Errorf("private-wrapper: %w", safe))
		var classified *Error
		if !errors.As(wrapped, &classified) || classified.Kind != tc.kind || classified.StatusCode != tc.status {
			t.Fatalf("status %d classified as %v", tc.status, wrapped)
		}
		if strings.Contains(wrapped.Error(), "private-") {
			t.Fatal("HTTP category exposed original details", wrapped)
		}
	}
}
