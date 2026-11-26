package mihomo

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// ListenerOptions contains only non-secret, instance-owned settings. AllowedOrigins
// stores one origin per line so the settings can be compared as a single value.
type ListenerOptions struct {
	CoreOS              string `json:"-" yaml:"-"` // Observed from the installed core.
	HTTPPort            int    `json:"http_port,omitempty"`
	SOCKSPort           int    `json:"socks_port,omitempty"`
	RedirPort           int    `json:"redir_port,omitempty"`
	TProxyPort          int    `json:"tproxy_port,omitempty"`
	AllowLAN            bool   `json:"allow_lan,omitempty"`
	HTTPSAddress        string `json:"https_address,omitempty"`
	CertificatePath     string `json:"certificate_path,omitempty"`
	PrivateKeyPath      string `json:"private_key_path,omitempty"`
	AllowedOrigins      string `json:"allowed_origins,omitempty"`
	AllowPrivateNetwork bool   `json:"allow_private_network,omitempty"`
	UnixSocket          string `json:"unix_socket,omitempty"`
	RoutingMark         uint32 `json:"routing_mark,omitempty"`
	DoHPath             string `json:"doh_path,omitempty"`
}

func (o ListenerOptions) SupportsRedirect() bool { return o.CoreOS != "windows" }
func (o ListenerOptions) SupportsTProxy() bool   { return o.CoreOS != "windows" && o.CoreOS != "darwin" }

// SupportedListeners returns only settings the installed core can open.
// Keep the original options when writing a generation to preserve hidden values.
func (o ListenerOptions) SupportedListeners() ListenerOptions {
	if !o.SupportsRedirect() {
		o.RedirPort = 0
	}
	if !o.SupportsTProxy() {
		o.TProxyPort = 0
	}
	return o
}

var controllerTLSFields = []string{"certificate", "private-key", "client-auth-type", "client-auth-cert", "ech-key"}

func (o ListenerOptions) apply(cfg map[string]any, controller, secret string, mixedPort int, proxyUsers []string) {
	cfg["external-controller"], cfg["secret"] = controller, secret
	cfg["external-controller-tls"], cfg["external-controller-unix"] = o.HTTPSAddress, o.UnixSocket
	cfg["external-controller-routing-mark"], cfg["external-doh-server"] = o.RoutingMark, o.DoHPath
	cfg["mixed-port"], cfg["port"], cfg["socks-port"], cfg["redir-port"], cfg["tproxy-port"] = mixedPort, o.HTTPPort, o.SOCKSPort, o.RedirPort, o.TProxyPort
	cfg["allow-lan"], cfg["bind-address"] = o.AllowLAN, "127.0.0.1"
	if o.AllowLAN {
		cfg["bind-address"] = "*"
	}
	// An explicitly managed list applies to every client of the HTTP(S),
	// SOCKS and mixed listeners, including loopback clients.
	if proxyUsers != nil {
		cfg["authentication"] = proxyUsers
		cfg["skip-auth-prefixes"] = []string{}
	} else if !o.AllowLAN {
		cfg["authentication"] = []string{}
	}
	cfg["routing-mark"] = 0
	for _, key := range []string{"external-controller-pipe", "external-ui", "external-ui-name", "external-ui-url", "ss-config", "vmess-config", "interface-name"} {
		cfg[key] = ""
	}
	origins := []string{}
	for _, origin := range strings.Split(o.AllowedOrigins, "\n") {
		if value := strings.TrimSpace(origin); value != "" {
			origins = append(origins, value)
		}
	}
	cfg["external-controller-cors"] = map[string]any{"allow-origins": origins, "allow-private-network": o.AllowPrivateNetwork}
	tls, _ := cfg["tls"].(map[string]any)
	for _, key := range controllerTLSFields {
		delete(tls, key)
	}
	if o.CertificatePath != "" || o.PrivateKeyPath != "" {
		if tls == nil {
			tls = map[string]any{}
		}
		tls["certificate"], tls["private-key"] = o.CertificatePath, o.PrivateKeyPath
	}
	if len(tls) == 0 {
		delete(cfg, "tls")
	} else {
		cfg["tls"] = tls
	}
}

// ResolveController pins an explicitly configured local interface or hostname to
// a numeric address. Later API calls never resolve that hostname or send the
// installation credential to a new address after a DNS change.
func ResolveController(ctx context.Context, address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || !validPort(port) {
		return "", diagnostic.New("diagnostic.settings.controllerAddress", "controller requires a local address and a port between 1024 and 65535")
	}
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		host = "127.0.0.1"
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err == nil {
		for _, entry := range ips {
			if entry.Zone == "" && (entry.IP.IsUnspecified() || isLocalIP(entry.IP)) {
				return net.JoinHostPort(entry.IP.String(), port), nil
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "", diagnostic.New("diagnostic.settings.controllerLocal", "controller address must belong to this machine")
}

func isLocalIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	interfaces, _ := net.InterfaceAddrs()
	for _, addr := range interfaces {
		local, ok := addr.(*net.IPNet)
		if ok && local.IP.Equal(ip) {
			return true
		}
	}
	return false
}

func validPort(raw string) bool {
	port, err := strconv.Atoi(raw)
	return err == nil && raw == strconv.Itoa(port) && port >= 1024 && port <= 65535
}

// ControllerTarget converts wildcard bindings into a concrete local dial address.
func ControllerTarget(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !validPort(port) {
		return "", diagnostic.New("diagnostic.api.controllerURL", "invalid local controller URL")
	}
	if ip.IsUnspecified() {
		host = "::1"
		if ip.To4() != nil {
			host = "127.0.0.1"
		}
	} else if !isLocalIP(ip) {
		return "", diagnostic.New("diagnostic.settings.controllerLocal", "controller address must belong to this machine")
	}
	return net.JoinHostPort(host, port), nil
}

type ListenerEndpoint struct{ Network, Address string }

func (o ListenerOptions) ProxyListenAddress(port int) string {
	host := "127.0.0.1"
	if o.AllowLAN {
		host = ""
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// Endpoints lists the sockets Mihomo opens, including datagram listeners.
func (o ListenerOptions) Endpoints(controller string, mixedPort int) []ListenerEndpoint {
	o = o.SupportedListeners()
	out := []ListenerEndpoint{{"tcp", controller}}
	if o.HTTPSAddress != "" {
		out = append(out, ListenerEndpoint{"tcp", o.HTTPSAddress})
	}
	for _, proxy := range []struct {
		port int
		udp  bool
	}{{mixedPort, true}, {o.HTTPPort, false}, {o.SOCKSPort, true}, {o.RedirPort, false}, {o.TProxyPort, true}} {
		if proxy.port == 0 {
			continue
		}
		address := o.ProxyListenAddress(proxy.port)
		out = append(out, ListenerEndpoint{"tcp", address})
		if proxy.udp {
			out = append(out, ListenerEndpoint{"udp", address})
		}
	}
	return out
}

func (ep ListenerEndpoint) Conflicts(other ListenerEndpoint) bool {
	if ep.Network != other.Network {
		return false
	}
	a, ap, _ := net.SplitHostPort(ep.Address)
	b, bp, _ := net.SplitHostPort(other.Address)
	return ap == bp && (a == b || a == "" || b == "" || net.ParseIP(a).IsUnspecified() || net.ParseIP(b).IsUnspecified())
}

func (o ListenerOptions) Validate(controller, secret string, mixedPort int) error {
	if secret == "" || strings.IndexFunc(secret, unicode.IsControl) >= 0 {
		return diagnostic.New("diagnostic.config.controllerSecret", "controller secret must be nonempty and contain no control characters")
	}
	for _, address := range []string{controller, o.HTTPSAddress} {
		if address == "" && controller != "" {
			continue
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !validPort(port) {
			return diagnostic.New("diagnostic.settings.controllerAddress", "controller requires a local address and a port between 1024 and 65535")
		}
	}
	o = o.SupportedListeners()
	for _, port := range []int{mixedPort, o.HTTPPort, o.SOCKSPort, o.RedirPort, o.TProxyPort} {
		if port < 0 || port > 65535 {
			return diagnostic.New("diagnostic.settings.proxyPortRange", "proxy ports must be between 0 and 65535")
		}
	}
	endpoints := o.Endpoints(controller, mixedPort)
	for i, ep := range endpoints {
		for _, other := range endpoints[:i] {
			if ep.Conflicts(other) {
				return diagnostic.New("diagnostic.config.portConflict", "two managed listeners use the same address and port")
			}
		}
	}
	for _, path := range []string{o.CertificatePath, o.PrivateKeyPath, o.UnixSocket} {
		if strings.IndexFunc(path, unicode.IsControl) >= 0 || strings.Contains(strings.ToUpper(path), "-----BEGIN") {
			return diagnostic.New("diagnostic.settings.listenerPath", "enter a file path without control characters or inline credentials")
		}
	}
	if o.HTTPSAddress != "" && (o.CertificatePath == "" || o.PrivateKeyPath == "") {
		return diagnostic.New("diagnostic.settings.controllerTLS", "HTTPS requires both a certificate path and a private key path")
	}
	if path := o.DoHPath; path != "" && (!strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\\") || strings.IndexFunc(path, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0) {
		return diagnostic.New("diagnostic.settings.dohPath", "DoH requires an absolute URL path without spaces, query or fragment")
	}
	for _, origin := range strings.Split(o.AllowedOrigins, "\n") {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || origin == "null" {
			continue
		}
		sample := strings.Replace(origin, "*", "1", 1)
		if strings.HasPrefix(origin, "*://") {
			sample = "https" + origin[1:]
		}
		u, err := url.Parse(sample)
		if err != nil || strings.Count(origin, "*") > 1 || u.Scheme == "" || u.Hostname() == "" || u.User != nil || u.Path != "" || strings.ContainsAny(sample, "?#\\") {
			return diagnostic.New("diagnostic.settings.corsOrigin", "CORS requires one valid origin per line")
		}
	}
	return nil
}
