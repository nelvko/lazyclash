package store

import (
	"path/filepath"
	"testing"
)

func TestSystemHomeIgnoresUserEnvironmentAndSeparatesServices(t *testing.T) {
	t.Setenv("LAZYCLASH_HOME", "/user/private-data")
	t.Setenv("XDG_DATA_HOME", "/user/xdg-data")
	for _, name := range []string{"lazyclash-mihomo", "lazyclash-isolated"} {
		got, err := homeForService("", name, "linux", 0)
		if err != nil || got != filepath.Join("/var/lib/lazyclash", name) {
			t.Fatalf("root home for %s = %q, %v", name, got, err)
		}
	}
	got, err := homeForService("/explicit/system-data", "lazyclash-isolated", "linux", 0)
	if err != nil || got != "/explicit/system-data" {
		t.Fatalf("explicit system home = %q, %v", got, err)
	}
	if _, err := homeForService("", "../foreign", "linux", 0); err == nil {
		t.Fatal("service name escaped the system namespace")
	}
}

func TestUserHomeKeepsEnvironmentAndExplicitOverride(t *testing.T) {
	t.Setenv("LAZYCLASH_HOME", "/user/private-data")
	got, err := homeForService("", "lazyclash-mihomo", "linux", 1000)
	if err != nil || got != "/user/private-data" {
		t.Fatalf("user environment home = %q, %v", got, err)
	}
	got, err = homeForService("/explicit/user-data", "lazyclash-mihomo", "linux", 1000)
	if err != nil || got != "/explicit/user-data" {
		t.Fatalf("explicit user home = %q, %v", got, err)
	}
	if _, err := homeForService("relative", "lazyclash-mihomo", "linux", 0); err == nil {
		t.Fatal("relative system home accepted")
	}
}
