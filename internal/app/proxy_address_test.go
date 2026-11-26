package app

import (
	"net/url"
	"testing"

	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestProxyAddressUsesRegularListenerAndEffectiveHost(t *testing.T) {
	for _, tc := range []struct {
		name, bind, want   string
		lan                bool
		mixed, http, socks int
	}{
		{"loopback even with stored binding", "192.0.2.9", "http://user:pass@127.0.0.1:7897", false, 7897, 8080, 1080},
		{"wildcard template", "", "http://user:pass@<LAN IP>:8080", true, 0, 8080, 1080},
		{"IPv4 wildcard", "0.0.0.0", "socks5://user:pass@<LAN IPv4>:1080", true, 0, 0, 1080},
		{"IPv6 wildcard", "[::]", "http://user:pass@[<LAN IPv6>]:7897", true, 7897, 0, 0},
		{"specific IPv6", "[2001:db8::1]", "http://user:pass@[2001:db8::1]:7897", true, 7897, 0, 0},
		{"specific IPv4", "192.0.2.9", "http://user:pass@192.0.2.9:8080", true, 0, 8080, 0},
		{"transparent only", "", "", false, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := RuntimeSettings{MixedPort: tc.mixed, ListenerOptions: mihomo.ListenerOptions{BindAddress: tc.bind, AllowLAN: tc.lan, HTTPPort: tc.http, SOCKSPort: tc.socks, RedirPort: 1234}}
			if got := s.ProxyAddress("user:pass"); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCopyProxyAddressEncodesSelectedAccountAndRejectsStaleChoice(t *testing.T) {
	a, _ := installedApp(t)
	ctx := t.Context()
	ports, err := a.PrepareRuntimeSettings(ctx, availableSettings(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.ApplyRuntimeSettings(ctx, ports); err != nil {
		t.Fatal(err)
	}
	first, err := a.GetProxyAuth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	user, password := "g@st", "a:/%密"
	plan, err := a.PrepareProxyAuth(ctx, first, []ProxyAuthUser{{Username: user, Password: password}, {Username: "reader", Password: "example"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.ApplyRuntimeSettings(ctx, plan); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		current bool
		name    string
		want    bool
	}{{true, user, true}, {false, user, false}, {true, "absent", false}} {
		current := first
		if tc.current {
			current, err = a.GetProxyAuth(ctx)
			if err != nil {
				t.Fatal(err)
			}
		}
		address, err := a.GetProxyAddress(ctx, current, tc.name)
		if (err == nil) != tc.want {
			t.Fatal("unexpected copy availability")
		}
		if !tc.want {
			if address != "" {
				t.Fatal("failed copy returned credentials")
			}
			continue
		}
		parsed, err := url.Parse(address)
		if err != nil || parsed.User == nil || parsed.User.Username() != user || parsed.Hostname() != "127.0.0.1" {
			t.Fatal("wrong account or host in copied address")
		}
		if got, ok := parsed.User.Password(); !ok || got != password {
			t.Fatal("password was not URI-encoded correctly")
		}
	}
}

func TestTerminalEndpointUsesReachableLoopbackBindingAndSourceRules(t *testing.T) {
	s := RuntimeSettings{MixedPort: 7897, AccessRules: mihomo.AccessRules{LANAllowedIPs: mihomo.DefaultLANAllowedIPs}}
	s.BindAddress = "192.0.2.1"
	if s.TerminalProxyPort() != 7897 {
		t.Fatal("inactive binding suppressed local proxy")
	}
	s.AllowLAN = true
	if s.TerminalProxyPort() != 0 {
		t.Fatal("published an unbound loopback")
	}
	s.BindAddress = "*"
	s.LANDisallowedIPs = "127.0.0.0/8"
	if s.TerminalProxyPort() != 0 {
		t.Fatal("published a blocked loopback")
	}
	s.LANDisallowedIPs = ""
	s.LANAllowedIPs = ""
	if s.TerminalProxyPort() != 0 {
		t.Fatal("empty allow list exposed a loopback endpoint")
	}
}
