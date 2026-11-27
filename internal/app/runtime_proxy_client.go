package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/nelvko/lazyclash/internal/mihomo"
)

// runtimeProxyClient connects exclusively through the verified Mihomo listener.
// Callers close idle connections after their request.
func (a *App) runtimeProxyClient(ctx context.Context) (*http.Client, error) {
	var client mihomo.Client
	var m manifest
	err := a.locked(ctx, func() error {
		var state RuntimeSnapshot
		var err error
		state, client, _, err = a.inspectRuntimeLocked(ctx)
		if err != nil {
			return err
		}
		if state.State != "ready" {
			return runtimeError(state)
		}
		m, _, err = a.load()
		return err
	})
	if err != nil {
		return nil, err
	}
	proxy, err := runtimeProxyURL(ctx, m, client)
	if err != nil {
		return nil, err
	}
	// Binding the source to the proxy's numeric address also ensures that a
	// removed LAN address cannot send proxy credentials to a remote host.
	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(proxy.Hostname())}}
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), DialContext: dialer.DialContext}
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}, nil
}

func runtimeProxyURL(ctx context.Context, m manifest, client mihomo.Client) (*url.URL, error) {
	scheme, key, port := "http", "mixed-port", m.Port
	if port == 0 {
		key, port = "port", m.Listeners.HTTPPort
	}
	if port == 0 {
		scheme, key, port = "socks5", "socks-port", m.Listeners.SOCKSPort
	}
	host := m.Listeners.ProxyDialHost()
	if port < 1 || port > 65535 || net.ParseIP(host) == nil {
		return nil, errors.New("no local HTTP or SOCKS proxy listener")
	}
	if err := client.ConfirmProxyPorts(ctx, map[string]int{key: port}); err != nil {
		return nil, err
	}
	proxy := &url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(port))}
	if len(m.proxyUsers) > 0 {
		user, password, _ := strings.Cut(m.proxyUsers[0], ":")
		proxy.User = url.UserPassword(user, password)
	}
	return proxy, nil
}
