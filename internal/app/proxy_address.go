package app

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// ProxyAddress formats a client URI. Wildcard hosts are templates because a
// bind address alone cannot identify the interface a remote client will use.
func (s RuntimeSettings) ProxyAddress(userinfo string) string {
	port, scheme := s.MixedPort, "http"
	if port == 0 {
		port = s.HTTPPort
	}
	if port == 0 {
		port, scheme = s.SOCKSPort, "socks5"
	}
	if port < 1 || port > 65535 {
		return ""
	}
	host := "127.0.0.1"
	if s.AllowLAN {
		host = strings.Trim(s.ConfiguredBindAddress(), "[]")
		if host == "*" {
			host = "<LAN IP>"
		} else {
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return ""
			}
			ip = ip.Unmap()
			switch {
			case ip.Is4() && ip.IsUnspecified():
				host = "<LAN IPv4>"
			case ip.IsUnspecified():
				host = "[<LAN IPv6>]"
			case ip.IsMulticast(), ip.Is6() && ip.IsLinkLocalUnicast(), ip.Is4() && ip.As4()[0] >= 240:
				return ""
			default:
				host = ip.String()
			}
		}
	}
	if userinfo != "" {
		userinfo += "@"
	}
	return scheme + "://" + userinfo + net.JoinHostPort(host, strconv.Itoa(port))
}

// GetProxyAddress resolves a password only for an explicit copy action.
// The generation guard also covers addresses, ports and account changes.
func (a *App) GetProxyAddress(ctx context.Context, original ProxyAuthSettings, username string) (string, error) {
	var address string
	err := a.locked(ctx, func() error {
		m, generation, err := a.load()
		if err != nil {
			return err
		}
		if original.revision == generation {
			for _, record := range m.proxyUsers {
				name, password, found := strings.Cut(record, ":")
				if found && name == username {
					address = manifestSettings(m).ProxyAddress(url.UserPassword(name, password).String())
					if address != "" {
						return nil
					}
					break
				}
			}
		}
		return diagnostic.New("diagnostic.settings.proxyAuthStale", "proxy settings changed while editing; reopen the editor")
	})
	return address, err
}
