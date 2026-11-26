package app

import (
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"

	"github.com/nelvko/lazyclash/internal/mihomo"
)

type BindAddressOption struct {
	Address   string
	Interface string
}

// BindAddressOptions suggests addresses only when the managed main process is
// running the local installation's executable in our network namespace.
// An empty result leaves manual entry available.
func (a *App) BindAddressOptions(ctx context.Context) []BindAddressOption {
	if runtime.GOOS != "linux" || a.Service == nil {
		return nil
	}
	status, err := a.Service.Inspect(ctx)
	if err != nil || !status.Active || status.PID <= 0 {
		return nil
	}
	bin, _, _ := a.paths()
	proc := filepath.Join("/proc", strconv.Itoa(status.PID))
	if !sameFile(bin, filepath.Join(proc, "exe")) || !sameFile("/proc/self/ns/net", filepath.Join(proc, "ns/net")) {
		return nil
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	// Put addresses usable by LAN clients ahead of loopback addresses.
	slices.SortStableFunc(interfaces, func(a, b net.Interface) int {
		return int(a.Flags&net.FlagLoopback) - int(b.Flags&net.FlagLoopback)
	})
	var options []BindAddressOption
	seen := map[netip.Addr]bool{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			network, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip, _ := netip.AddrFromSlice(network.IP)
			ip = ip.Unmap()
			// A link-local IPv6 listener requires a zone, which bind-address
			// cannot represent. Wildcards are offered separately by the editor.
			if seen[ip] || !(ip.IsGlobalUnicast() || ip.IsLoopback() || ip.Is4() && ip.IsLinkLocalUnicast()) {
				continue
			}
			seen[ip] = true
			address, _ := mihomo.NormalizeBindAddress(ip.String())
			options = append(options, BindAddressOption{Address: address, Interface: iface.Name})
		}
	}
	return options
}

func sameFile(first, second string) bool {
	a, err := os.Stat(first)
	if err != nil {
		return false
	}
	b, err := os.Stat(second)
	return err == nil && os.SameFile(a, b)
}
