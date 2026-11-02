// Package downloadnet separates release-file locations from connection proxies.
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
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// Prefix and Mirror rewrite release files only. Version metadata and SHA256
// digests come from official GitHub endpoints through the chosen proxy.
type Config struct {
	Source    string `json:"source"`
	Prefix    string `json:"prefix,omitempty"`
	Mirror    string `json:"mirror,omitempty"`
	ProxyMode string `json:"proxy_mode"`
	ProxyURL  string `json:"proxy_url,omitempty"`
}

func (c Config) Normalized() Config {
	if c.Source == "" {
		c.Source = "official"
	}
	if c.ProxyMode == "" {
		c.ProxyMode = "none"
	}
	return c
}

func webURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Fragment != "" {
		return nil, diagnostic.New("diagnostic.network.absoluteURL", "expected an absolute HTTP(S) URL without a fragment")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return nil, diagnostic.New("diagnostic.network.urlPort", "invalid URL port")
		}
	}
	return u, nil
}
func proxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, diagnostic.New("diagnostic.network.proxyURL", "proxy must be an HTTP, HTTPS, SOCKS5 or SOCKS5H URL without a path or query")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return nil, diagnostic.New("diagnostic.network.proxyPort", "invalid proxy port")
		}
	}
	return u, nil
}

func (c Config) Validate() error {
	c = c.Normalized()
	switch c.Source {
	case "official":
	case "prefix":
		u, err := webURL(c.Prefix)
		if err != nil {
			return diagnostic.New("diagnostic.network.prefixURL", "invalid release-file prefix URL")
		}
		if u.RawQuery != "" || strings.ContainsAny(c.Prefix, "{}") {
			return diagnostic.New("diagnostic.network.prefixTemplate", "prefix must be a plain URL; use mirror for a version/asset template")
		}
	case "mirror":
		if !strings.Contains(c.Mirror, "{version}") || !strings.Contains(c.Mirror, "{asset}") {
			return diagnostic.New("diagnostic.network.mirrorRequired", "mirror URL template must contain {version} and {asset}")
		}
		probe := strings.NewReplacer("{version}", "v1.2.3", "{asset}", "mihomo.gz").Replace(c.Mirror)
		if strings.ContainsAny(probe, "{}") {
			return diagnostic.New("diagnostic.network.mirrorPlaceholder", "unsupported mirror URL template placeholder")
		}
		if _, err := webURL(probe); err != nil {
			return diagnostic.New("diagnostic.network.mirrorURL", "invalid release-file mirror URL template")
		}
	default:
		return diagnostic.New("diagnostic.network.source", "release source must be official, prefix or mirror")
	}
	switch c.ProxyMode {
	case "none", "environment":
	case "custom":
		if _, err := proxyURL(c.ProxyURL); err != nil {
			return err
		}
	default:
		return diagnostic.New("diagnostic.network.proxyMode", "proxy mode must be none, environment or custom")
	}
	return nil
}

func (c Config) AssetURL(original, version, asset string) (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	c = c.Normalized()
	result := original
	switch c.Source {
	case "prefix":
		result = strings.TrimRight(c.Prefix, "/") + "/" + original
	case "mirror":
		result = strings.NewReplacer("{version}", url.PathEscape(version), "{asset}", url.PathEscape(asset)).Replace(c.Mirror)
	}
	if _, err := webURL(result); err != nil {
		return "", diagnostic.New("diagnostic.network.resultURL", "invalid resulting release-file URL")
	}
	return result, nil
}

// RedactedURL omits credentials and paths, where subscription and proxy tokens
// can also live. It is suitable for previews and ordinary diagnostics.
func RedactedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.Scheme == "" {
		return "[URL withheld]"
	}
	result := u.Scheme + "://" + u.Host
	if u.Path != "" && u.Path != "/" {
		result += "/…"
	}
	if u.RawQuery != "" {
		result += "?[redacted]"
	}
	return result
}

// Error carries a stable category without retaining a sensitive URL or body.
type Error struct {
	Kind, Operation string
	StatusCode      int
	cause           error
}

func (e *Error) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("%s failed: HTTP %d (%s)", e.Operation, e.StatusCode, e.Kind)
	}
	return e.Operation + " failed: " + e.Kind
}
func (e *Error) Unwrap() error { return e.cause }

// Diagnostic exposes only the already redacted public fields. The original
// transport error is deliberately excluded: it can contain URL credentials.
func (e *Error) Diagnostic() error {
	operation := networkDiagnosticLabel(e.Operation)
	kind := networkDiagnosticLabel(e.Kind)
	if e.StatusCode != 0 {
		return diagnostic.New("diagnostic.network.httpFailure", "%s failed: HTTP %d (%s)", operation, e.StatusCode, kind)
	}
	return diagnostic.New("diagnostic.network.failure", "%s failed: %s", operation, kind)
}

func networkDiagnosticLabel(value string) error {
	switch value {
	case "connection proxy":
		return diagnostic.New("diagnostic.network.operation.proxy", "connection proxy")
	case "official version query":
		return diagnostic.New("diagnostic.network.operation.versionQuery", "official version query")
	case "official version response":
		return diagnostic.New("diagnostic.network.operation.versionResponse", "official version response")
	case "release-file connection test":
		return diagnostic.New("diagnostic.network.operation.fileTest", "release-file connection test")
	case "release-file download":
		return diagnostic.New("diagnostic.network.operation.download", "release-file download")
	case "canceled":
		return diagnostic.New("diagnostic.network.kind.canceled", "canceled")
	case "timeout":
		return diagnostic.New("diagnostic.network.kind.timeout", "timeout")
	case "DNS lookup":
		return diagnostic.New("diagnostic.network.kind.dns", "DNS lookup")
	case "TLS certificate":
		return diagnostic.New("diagnostic.network.kind.certificate", "TLS certificate")
	case "TLS handshake":
		return diagnostic.New("diagnostic.network.kind.handshake", "TLS handshake")
	case "proxy connection":
		return diagnostic.New("diagnostic.network.kind.proxyConnection", "proxy connection")
	case "network connection":
		return diagnostic.New("diagnostic.network.kind.connection", "network connection")
	case "HTTP response":
		return diagnostic.New("diagnostic.network.kind.response", "HTTP response")
	case "proxy authentication":
		return diagnostic.New("diagnostic.network.kind.proxyAuthentication", "proxy authentication")
	case "rate limit":
		return diagnostic.New("diagnostic.network.kind.rateLimit", "rate limit")
	case "permission denied or rate limit":
		return diagnostic.New("diagnostic.network.kind.permission", "permission denied or rate limit")
	default:
		return errors.New(value)
	}
}

func Failure(ctx context.Context, operation string, err error) error {
	cause := ctx.Err()
	if cause == nil {
		switch {
		case errors.Is(err, context.Canceled):
			cause = context.Canceled
		case errors.Is(err, context.DeadlineExceeded):
			cause = context.DeadlineExceeded
		}
	}
	if cause != nil {
		kind := "canceled"
		if errors.Is(cause, context.DeadlineExceeded) {
			kind = "timeout"
		}
		return &Error{Kind: kind, Operation: operation, cause: cause}
	}
	var safe *Error
	if errors.As(err, &safe) {
		return &Error{Kind: safe.Kind, Operation: operation, StatusCode: safe.StatusCode}
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return &Error{Kind: "DNS lookup", Operation: operation}
	}
	if certificateFailure(err) {
		return &Error{Kind: "TLS certificate", Operation: operation}
	}
	var record tls.RecordHeaderError
	var alert tls.AlertError
	var ech *tls.ECHRejectionError
	if errors.As(err, &record) || errors.As(err, &alert) || errors.As(err, &ech) {
		return &Error{Kind: "TLS handshake", Operation: operation}
	}
	if timedOut(err) {
		return &Error{Kind: "timeout", Operation: operation}
	}
	var op *net.OpError
	if errors.As(err, &op) && (strings.Contains(op.Op, "proxy") || strings.HasPrefix(op.Op, "socks")) {
		return &Error{Kind: "proxy connection", Operation: operation}
	}
	return &Error{Kind: "network connection", Operation: operation}
}

// url.Error and other net.Error wrappers can report Timeout=false when their
// immediate cause is a fmt wrapper. Inspect the complete error tree instead of
// letting the first matching wrapper hide a typed inner transport timeout.
func timedOut(err error) bool {
	if n, ok := err.(net.Error); ok && n.Timeout() {
		return true
	}
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		for _, cause := range e.Unwrap() {
			if timedOut(cause) {
				return true
			}
		}
	case interface{ Unwrap() error }:
		return timedOut(e.Unwrap())
	}
	return false
}

func certificateFailure(err error) bool {
	var verification *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var roots x509.SystemRootsError
	var insecure x509.InsecureAlgorithmError
	var constraint x509.ConstraintViolationError
	return errors.As(err, &verification) || errors.As(err, &unknown) ||
		errors.As(err, &hostname) || errors.As(err, &invalid) ||
		errors.As(err, &roots) || errors.As(err, &insecure) || errors.As(err, &constraint)
}

func HTTPError(operation string, status int) error {
	kind := "HTTP response"
	switch status {
	case http.StatusProxyAuthRequired:
		kind = "proxy authentication"
	case http.StatusTooManyRequests:
		kind = "rate limit"
	case http.StatusForbidden:
		kind = "permission denied or rate limit"
	}
	return &Error{Kind: kind, Operation: operation, StatusCode: status}
}

func NewClient(cfg Config, timeout time.Duration) (*http.Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.Normalized()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	tr.TLSHandshakeTimeout = 15 * time.Second
	tr.ResponseHeaderTimeout = 30 * time.Second
	// Preserve the numeric CONNECT response before net/http turns a rejection
	// into plain status text. No proxy body, headers or credentials are retained.
	tr.OnProxyConnectResponse = func(_ context.Context, _ *url.URL, _ *http.Request, response *http.Response) error {
		if response.StatusCode == http.StatusOK {
			return nil
		}
		classified := HTTPError("connection proxy", response.StatusCode).(*Error)
		if classified.Kind == "HTTP response" {
			classified.Kind = "proxy connection"
		}
		return classified
	}
	switch cfg.ProxyMode {
	case "environment":
		tr.Proxy = environmentProxy()
	case "custom":
		p, _ := proxyURL(cfg.ProxyURL)
		tr.Proxy = http.ProxyURL(p)
	}
	return &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return diagnostic.New("diagnostic.network.redirectLimit", "redirect limit exceeded")
		}
		if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return diagnostic.New("diagnostic.network.httpsDowngrade", "HTTPS downgrade redirect refused")
		}
		req.Header.Del("Referer")
		if len(via) > 0 && via[0].URL.Host != req.URL.Host {
			req.Header.Del("Authorization")
			req.Header.Del("Proxy-Authorization")
		}
		return nil
	}}, nil
}

func envValue(names ...string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return ""
}

// Each new client snapshots the environment. Unlike ProxyFromEnvironment, this
// does not cache the first process-wide values or accidentally affect no-proxy.
func environmentProxy() func(*http.Request) (*url.URL, error) {
	httpProxy := envValue("HTTP_PROXY", "http_proxy")
	httpsProxy := envValue("HTTPS_PROXY", "https_proxy")
	allProxy := envValue("ALL_PROXY", "all_proxy")
	noProxy := envValue("NO_PROXY", "no_proxy")
	cgi := os.Getenv("REQUEST_METHOD") != ""
	return func(req *http.Request) (*url.URL, error) {
		if bypass(req.URL, noProxy) {
			return nil, nil
		}
		raw := allProxy
		if req.URL.Scheme == "https" && httpsProxy != "" {
			raw = httpsProxy
		}
		if req.URL.Scheme == "http" && httpProxy != "" {
			if cgi {
				return nil, diagnostic.New("diagnostic.network.cgiProxy", "HTTP_PROXY is unavailable in CGI environments")
			}
			raw = httpProxy
		}
		if raw == "" {
			return nil, nil
		}
		if !strings.Contains(raw, "://") {
			raw = "http://" + raw
		}
		p, err := proxyURL(raw)
		if err != nil {
			return nil, diagnostic.New("diagnostic.network.environmentProxy", "invalid environment proxy URL")
		}
		return p, nil
	}
}
func bypass(u *url.URL, list string) bool {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	ip := net.ParseIP(host)
	if host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return true
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	for _, item := range strings.Split(list, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if item == "*" {
			return true
		}
		if _, network, err := net.ParseCIDR(item); err == nil {
			if ip != nil && network.Contains(ip) {
				return true
			}
			continue
		}
		entryHost, entryPort, err := net.SplitHostPort(item)
		if err == nil {
			if port != entryPort {
				continue
			}
			item = entryHost
		}
		item = strings.Trim(item, "[]")
		if parsed := net.ParseIP(item); parsed != nil {
			if ip != nil && ip.Equal(parsed) {
				return true
			}
			continue
		}
		if strings.HasPrefix(item, "*.") {
			item = item[1:]
		}
		if strings.HasPrefix(item, ".") {
			if strings.HasSuffix(host, item) {
				return true
			}
		} else if host == item || strings.HasSuffix(host, "."+item) {
			return true
		}
	}
	return false
}
