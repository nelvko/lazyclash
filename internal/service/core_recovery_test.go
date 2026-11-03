package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResetFailedOwnedClearsOnlyVerifiedUnitWithoutChangingLifecycle(t *testing.T) {
	for _, name := range []string{"current"} {
		t.Run(name, func(t *testing.T) {
			s, _ := testManager(t)
			path := filepath.Join(s.UnitDir, s.Name+".service")
			content, err := unitContent("/my/mihomo", "/my/config", "/my/data")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			var calls []string
			s.run = func(_ context.Context, command string, args ...string) ([]byte, error) {
				calls = append(calls, command+" "+strings.Join(args, " "))
				return []byte("FragmentPath=" + path + "\nDropInPaths=\nNeedDaemonReload=no\n"), nil
			}
			if err := s.ResetFailedOwned(t.Context(), "/my/mihomo", "/my/config", "/my/data"); err != nil {
				t.Fatal(err)
			}
			want := []string{
				"systemctl --user show lazyclash-test.service --property=FragmentPath,DropInPaths,NeedDaemonReload",
				"systemctl --user reset-failed lazyclash-test.service",
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("recovery changed scope, skipped ownership, or changed process state: %v", calls)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil || string(raw) != content || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
				t.Fatalf("reset-failed rewrote the service definition: %v", err)
			}
		})
	}
}

func TestResetFailedOwnedRejectsForeignModifiedAndUnverifiedUnits(t *testing.T) {
	for _, scenario := range []string{"foreign-paths", "modified-file", "symlink", "foreign-fragment", "drop-in", "pending-reload", "verify-failed"} {
		t.Run(scenario, func(t *testing.T) {
			s, _ := testManager(t)
			path := filepath.Join(s.UnitDir, s.Name+".service")
			content, err := unitContent("/my/mihomo", "/my/config", "/my/data")
			if err != nil {
				t.Fatal(err)
			}
			fragment, dropIns, reload := path, "", "no"
			switch scenario {
			case "foreign-paths":
				content, err = unitContent("/other/mihomo", "/other/config", "/other/data")
				if err != nil {
					t.Fatal(err)
				}
			case "modified-file":
				content += "\nEnvironment=USER_CHANGED=yes\n"
			case "foreign-fragment":
				fragment = "/other/lazyclash-test.service"
			case "drop-in":
				dropIns = "/other/lazyclash-test.service.d/override.conf"
			case "pending-reload":
				reload = "yes"
			}
			preservedPath := path
			if scenario == "symlink" {
				preservedPath = filepath.Join(s.UnitDir, "foreign.service")
				if err := os.Symlink(preservedPath, path); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(preservedPath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			verifyErr := errors.New("manager ownership query failed")
			resets := 0
			s.run = func(_ context.Context, command string, args ...string) ([]byte, error) {
				if len(args) > 1 && args[1] == "reset-failed" {
					resets++
				}
				if scenario == "verify-failed" {
					return nil, verifyErr
				}
				return []byte("FragmentPath=" + fragment + "\nDropInPaths=" + dropIns + "\nNeedDaemonReload=" + reload + "\n"), nil
			}
			err = s.ResetFailedOwned(t.Context(), "/my/mihomo", "/my/config", "/my/data")
			if err == nil || resets != 0 {
				t.Fatalf("unverified unit was reset: error=%v resets=%d", err, resets)
			}
			if scenario == "verify-failed" && !errors.Is(err, verifyErr) {
				t.Fatalf("verification failure cause lost: %v", err)
			}
			raw, readErr := os.ReadFile(preservedPath)
			if readErr != nil || string(raw) != content {
				t.Fatalf("protected unit was modified: %v", readErr)
			}
		})
	}
}

func TestResetFailedOwnedPropagatesResetFailureAndCancellation(t *testing.T) {
	for _, scenario := range []string{"reset-failed", "cancel-before-verification", "cancel-after-verification"} {
		t.Run(scenario, func(t *testing.T) {
			s, _ := testManager(t)
			path := filepath.Join(s.UnitDir, s.Name+".service")
			content, err := unitContent("/mihomo", "/config", "/data")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancel-before-verification" {
				cancel()
			}
			resetErr := errors.New("systemd reset-failed refused")
			shows, resets := 0, 0
			s.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if args[1] == "reset-failed" {
					resets++
					return nil, resetErr
				}
				shows++
				if scenario == "cancel-after-verification" {
					cancel()
				}
				return []byte("FragmentPath=" + path + "\nDropInPaths=\nNeedDaemonReload=no\n"), nil
			}
			err = s.ResetFailedOwned(ctx, "/mihomo", "/config", "/data")
			if scenario == "reset-failed" {
				if !errors.Is(err, resetErr) || shows != 1 || resets != 1 {
					t.Fatalf("reset error lost: error=%v shows=%d resets=%d", err, shows, resets)
				}
			} else if !errors.Is(err, context.Canceled) || resets != 0 || scenario == "cancel-before-verification" && shows != 0 {
				t.Fatalf("cancellation lost or reset submitted: error=%v shows=%d resets=%d", err, shows, resets)
			}
		})
	}
}
