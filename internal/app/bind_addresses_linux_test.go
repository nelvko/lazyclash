package app

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBindAddressOptionsRequireLocalRunningCore(t *testing.T) {
	a, svc := testApp(t)
	running := &proxyPIDService{fakeService: svc, pid: os.Getpid()}
	a.Service = running
	svc.active = true
	bin, _, _ := a.paths()
	if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, bin, []byte("different executable"))
	if options := a.BindAddressOptions(t.Context()); len(options) != 0 {
		t.Fatal("suggested addresses for a different executable")
	}
	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, bin); err != nil {
		t.Fatal(err)
	}
	seen := map[netip.Addr]bool{}
	for _, opt := range a.BindAddressOptions(t.Context()) {
		ip, err := netip.ParseAddr(strings.Trim(opt.Address, "[]"))
		if err != nil || seen[ip] || opt.Interface == "" || (ip.Is6() && opt.Address != "["+ip.String()+"]") {
			t.Fatalf("unexpected bind address option: %+v", opt)
		}
		seen[ip] = true
	}
	if !seen[netip.MustParseAddr("127.0.0.1")] {
		t.Fatal("missing loopback address for the verified local process")
	}
	svc.active = false
	if options := a.BindAddressOptions(t.Context()); len(options) != 0 {
		t.Fatal("suggested addresses for stopped service")
	}
}
