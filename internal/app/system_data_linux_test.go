//go:build linux

package app

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/nelvko/lazyclash/internal/service"
)

func TestRootAppSelectsIndependentSystemDefaults(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to select system defaults")
	}
	t.Setenv("LAZYCLASH_HOME", "/tmp/inherited-user-home")
	t.Setenv("LAZYCLASH_SERVICE", "lazyclash-inherited-user-service")
	t.Setenv("XDG_DATA_HOME", "/tmp/inherited-user-data")
	for _, name := range []string{"", "lazyclash-explicit-scope"} {
		a, err := New("", name)
		if err != nil {
			t.Fatal(err)
		}
		wantName := name
		if wantName == "" {
			wantName = DefaultService
		}
		if a.Name != wantName || a.Home != filepath.Join("/var/lib/lazyclash", wantName) {
			t.Fatalf("name/home inherited user environment: %q / %q", a.Name, a.Home)
		}
		s, ok := a.Service.(*service.Systemd)
		if !ok || s.Scope != service.System || s.Lifecycle != filepath.Join(a.Home, "installation", "lazyclash") || s.Home != a.Home {
			t.Fatalf("incorrect system service configuration: %+v", a.Service)
		}
	}
}

func rootSystemDirectory(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root to verify system data ownership")
	}
	dir, err := os.MkdirTemp("/tmp", "lazyclash-system-data-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestSystemHomeRejectsWritableAndSymlinkAncestors(t *testing.T) {
	dir := rootSystemDirectory(t)
	home := filepath.Join(dir, "instance")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := trustedRootAncestors(home); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := trustedRootAncestors(home); err == nil {
		t.Fatal("accepted a writable non-sticky ancestor")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	if err := trustedRootAncestors(link); err == nil {
		t.Fatal("accepted a symbolic link as a system data directory")
	}
	if err := os.Chmod(home, 0777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if err := trustedRootAncestors(home); err == nil {
		t.Fatal("accepted a writable system home with the sticky bit")
	}
}

func TestSystemDataSeparatesRuntimeOwnershipFromProtectedFiles(t *testing.T) {
	dir := rootSystemDirectory(t)
	bin := filepath.Join(dir, "mihomo")
	if err := os.WriteFile(bin, []byte("binary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := setOwnership(bin, 0, 65534, 0750, false); err != nil {
		t.Fatal(err)
	}
	if err := trustedRootFile(bin); err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(dir, "runtime")
	if err := os.Mkdir(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	if err := setOwnership(runtime, 65534, 65534, 0700, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if stat := info.Sys().(*syscall.Stat_t); stat.Uid != 65534 || stat.Gid != 65534 || info.Mode().Perm() != 0700 {
		t.Fatalf("runtime ownership/mode = %v / %v", stat, info.Mode())
	}
	if err := os.Chmod(bin, 0770); err != nil {
		t.Fatal(err)
	}
	if err := trustedRootFile(bin); err == nil {
		t.Fatal("accepted a service-writable executable")
	}
}

func TestRootLifecycleIdentitySurvivesDamagedCurrent(t *testing.T) {
	dir := rootSystemDirectory(t)
	installation := filepath.Join(dir, "installation")
	if err := os.Mkdir(installation, 0700); err != nil {
		t.Fatal(err)
	}
	id := "0123456789abcdef01234567"
	path := filepath.Join(installation, "instance-id")
	if err := os.WriteFile(path, []byte(id), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{Home: dir}
	if actual, err := a.lifecycleIdentity(); err != nil || actual != id {
		t.Fatalf("stable cleanup identity: %q %v", actual, err)
	}
	if err := os.Chmod(path, 0660); err != nil {
		t.Fatal(err)
	}
	if _, err := a.lifecycleIdentity(); err == nil {
		t.Fatal("mutable cleanup identity accepted")
	}
}
func TestSystemHomeRejectsSandboxHiddenLocations(t *testing.T) {
	for _, path := range []string{"/tmp/root-data", "/var/tmp/root-data", "/root/lazyclash", "/home/alice/lazyclash", "/run/user/0/lazyclash"} {
		if err := systemHomeVisible(path); err == nil {
			t.Fatalf("hidden system home accepted: %s", path)
		}
	}
	if err := systemHomeVisible("/var/lib/lazyclash/example"); err != nil {
		t.Fatal(err)
	}
}
