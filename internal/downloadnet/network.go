// Package downloadnet separates release-file locations from connection proxies.
package downloadnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Prefix and Mirror rewrite release files only. Version metadata and SHA256
// digests always come from the official GitHub API through the chosen proxy.
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
		return nil, errors.New("expected an absolute HTTP(S) URL without a fragment")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return nil, errors.New("invalid URL port")
		}
	}
	return u, nil
}
func proxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("proxy must be an HTTP, HTTPS, SOCKS5 or SOCKS5H URL without a path or query")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return nil, errors.New("invalid proxy port")
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
			return errors.New("invalid release-file prefix URL")
		}
		if u.RawQuery != "" || strings.ContainsAny(c.Prefix, "{}") {
			return errors.New("prefix must be a plain URL; use mirror for a version/asset template")
		}
	case "mirror":
		if !strings.Contains(c.Mirror, "{version}") || !strings.Contains(c.Mirror, "{asset}") {
			return errors.New("mirror URL template must contain {version} and {asset}")
		}
		probe := strings.NewReplacer("{version}", "v1.2.3", "{asset}", "mihomo.gz").Replace(c.Mirror)
		if strings.ContainsAny(probe, "{}") {
			return errors.New("unsupported mirror URL template placeholder")
		}
		if _, err := webURL(probe); err != nil {
			return errors.New("invalid release-file mirror URL template")
		}
	default:
		return errors.New("release source must be official, prefix or mirror")
	}
	switch c.ProxyMode {
	case "none", "environment":
	case "custom":
		if _, err := proxyURL(c.ProxyURL); err != nil {
			return err
		}
	default:
		return errors.New("proxy mode must be none, environment or custom")
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
		return "", errors.New("invalid resulting release-file URL")
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
func Failure(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil {
		return &Error{Kind: "canceled or timed out", Operation: operation, cause: ctx.Err()}
	}
	var n net.Error
	if errors.As(err, &n) && n.Timeout() {
		return &Error{Kind: "timeout", Operation: operation}
	}
	var op *net.OpError
	if errors.As(err, &op) && strings.Contains(op.Op, "proxy") {
		return &Error{Kind: "proxy connection", Operation: operation}
	}
	return &Error{Kind: "network connection", Operation: operation}
}
func HTTPError(operation string, status int) error {
	kind := "HTTP response"
	if status == http.StatusProxyAuthRequired {
		kind = "proxy authentication"
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
	switch cfg.ProxyMode {
	case "environment":
		tr.Proxy = environmentProxy()
	case "custom":
		p, _ := proxyURL(cfg.ProxyURL)
		tr.Proxy = http.ProxyURL(p)
	}
	return &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("redirect limit exceeded")
		}
		if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("HTTPS downgrade redirect refused")
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
				return nil, errors.New("HTTP_PROXY is unavailable in CGI environments")
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
			return nil, errors.New("invalid environment proxy URL")
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
