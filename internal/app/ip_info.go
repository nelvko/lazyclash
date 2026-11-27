package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/nelvko/lazyclash/internal/mihomo"
)

type IPInfo struct {
	IP, Country, Region, City        string
	ISP, Organization, ASN, Timezone string
}

const ipInfoEndpoint = "https://ipwho.is/?fields=success,ip,country,region,city,connection.isp,connection.org,connection.asn,timezone.id"

// LookupIPInfo observes the exit used by the current Mihomo rules for ipwho.is.
func (a *App) LookupIPInfo(ctx context.Context) (IPInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
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
		return IPInfo{}, err
	}
	proxy, err := ipInfoProxy(ctx, m, client)
	if err != nil {
		return IPInfo{}, err
	}
	// Binding the source to the proxy's numeric address also ensures that a
	// removed LAN address cannot send proxy credentials to a remote host.
	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(proxy.Hostname())}}
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), DialContext: dialer.DialContext}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return fetchIPInfo(ctx, httpClient)
}

func ipInfoProxy(ctx context.Context, m manifest, client mihomo.Client) (*url.URL, error) {
	scheme, key, port := "http", "mixed-port", m.Port
	if port == 0 {
		key, port = "port", m.Listeners.HTTPPort
	}
	if port == 0 {
		scheme, key, port = "socks5", "socks-port", m.Listeners.SOCKSPort
	}
	host := m.Listeners.ProxyDialHost()
	if port < 1 || port > 65535 || net.ParseIP(host) == nil {
		return nil, errors.New("no local HTTP or SOCKS proxy listener for IP lookup")
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

func fetchIPInfo(ctx context.Context, client *http.Client) (IPInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ipInfoEndpoint, nil)
	if err != nil {
		return IPInfo{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return IPInfo{}, ctx.Err()
		}
		return IPInfo{}, errors.New("IP lookup through Mihomo failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return IPInfo{}, errors.New("IP lookup service unavailable")
	}
	const limit = 16 << 10
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if ctx.Err() != nil {
		return IPInfo{}, ctx.Err()
	}
	var result struct {
		Success                   bool
		IP, Country, Region, City string
		Connection                struct {
			ASN      uint32
			ISP, Org string
		}
		Timezone struct{ ID string }
	}
	if err != nil || len(raw) > limit || json.Unmarshal(raw, &result) != nil || !result.Success {
		return IPInfo{}, errors.New("invalid IP lookup response")
	}
	ip, err := netip.ParseAddr(result.IP)
	if err != nil || ip.Zone() != "" {
		return IPInfo{}, errors.New("invalid IP lookup address")
	}
	field := func(s string) string {
		if strings.IndexFunc(s, unicode.IsControl) >= 0 {
			return ""
		}
		return strings.TrimSpace(s)
	}
	info := IPInfo{IP: ip.String(), Country: field(result.Country), Region: field(result.Region), City: field(result.City),
		ISP: field(result.Connection.ISP), Organization: field(result.Connection.Org), Timezone: field(result.Timezone.ID)}
	if result.Connection.ASN > 0 {
		info.ASN = "AS" + strconv.FormatUint(uint64(result.Connection.ASN), 10)
	}
	return info, nil
}
