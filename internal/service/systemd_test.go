package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testManager(t *testing.T) (*Systemd, *[]string) {
	t.Helper()
	calls := []string{}
	s := &Systemd{Name: "lazyclash-test", UnitDir: t.TempDir()}
	s.run = func(_ context.Context, cmd string, args ...string) ([]byte, error) {
		calls = append(calls, cmd+" "+strings.Join(args, " "))
		if strings.Contains(strings.Join(args, " "), "--property=LoadState") {
			return []byte("not-found\n"), nil
		}
		return nil, nil
	}
	return s, &calls
}

func TestInspectReportsProcessIdentityAndRejectsInvalidPID(t *testing.T) {
	for _, pid := range []string{"4321", "0", "-1", "not-a-pid"} {
		t.Run(pid, func(t *testing.T) {
			s, _ := testManager(t)
			s.run = func(_ context.Context, command string, args ...string) ([]byte, error) {
				if command == "loginctl" {
					return []byte("yes\n"), nil
				}
				if !strings.Contains(strings.Join(args, " "), "MainPID") {
					t.Fatal("process identity was not requested")
				}
				return []byte("LoadState=loaded\nActiveState=active\nMainPID=" + pid + "\n"), nil
			}
			status, err := s.Inspect(t.Context())
			if pid == "-1" || pid == "not-a-pid" {
				if err == nil {
					t.Fatal("invalid process identity accepted")
				}
				return
			}
			if err != nil || !status.Active || !status.Linger || (pid == "4321" && status.PID != 4321) || (pid == "0" && status.PID != 0) {
				t.Fatalf("status=%+v err=%v", status, err)
			}
		})
	}
}

func TestInstallIsAtomicAndDoesNotEnable(t *testing.T) {
	s, calls := testManager(t)
	if err := s.Install(context.Background(), "/tmp/my bin/mihomo", "/tmp/config.yaml", "/tmp/data"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.UnitDir, s.Name+".service")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("unit permissions: %o", info.Mode().Perm())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `ExecStart=:"/tmp/my bin/mihomo" -d "/tmp/data" -f "/tmp/config.yaml"`) {
		t.Fatalf("invalid unit: %s", content)
	}
	want := []string{
		"systemctl --user show lazyclash-test.service --property=LoadState --value",
		"systemctl --user daemon-reload",
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("unexpected side effects: %v", *calls)
	}
	if err := s.Install(context.Background(), "/different", "/config", "/data"); err == nil {
		t.Fatal("second installation overwrote existing unit")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(content) {
		t.Fatalf("existing unit changed: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(s.UnitDir, ".lazyclash-unit-*"))
	if len(files) != 0 {
		t.Fatalf("staging files leaked: %v", files)
	}
}

func TestAvailableRejectsDanglingSymlinkAndVendorUnit(t *testing.T) {
	s, calls := testManager(t)
	path := filepath.Join(s.UnitDir, s.Name+".service")
	if err := os.Symlink("missing", path); err != nil {
		t.Fatal(err)
	}
	if err := s.Available(context.Background()); err == nil {
		t.Fatal("accepted dangling symlink")
	}
	if len(*calls) != 0 {
		t.Fatal("should reject existing path before querying manager")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s.run = func(context.Context, string, ...string) ([]byte, error) { return []byte("loaded\n"), nil }
	if err := s.Available(context.Background()); err == nil {
		t.Fatal("accepted existing vendor unit")
	}
}

func TestFailedReloadRollsBackUnit(t *testing.T) {
	s, _ := testManager(t)
	reloads := 0
	s.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[len(args)-1] == "daemon-reload" {
			reloads++
			return nil, errors.New("unavailable manager")
		}
		return []byte("not-found\n"), nil
	}
	if err := s.Install(context.Background(), "/mihomo", "/config", "/data"); err == nil {
		t.Fatal("expected reload failure")
	}
	if _, err := os.Lstat(filepath.Join(s.UnitDir, s.Name+".service")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unit left behind: %v", err)
	}
	if reloads != 2 {
		t.Fatalf("rollback should invalidate cache, reloads=%d", reloads)
	}
}

func TestRemoveProtectsForeignUnitAndSymlink(t *testing.T) {
	s, _ := testManager(t)
	path := filepath.Join(s.UnitDir, s.Name+".service")
	if err := os.WriteFile(path, []byte("[Service]\nExecStart=/existing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(context.Background()); err == nil {
		t.Fatal("removed foreign unit")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(s.UnitDir, "foreign")
	content, err := unitContent("/mihomo", "/config", "/data")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(context.Background()); err == nil {
		t.Fatal("removed symlink")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("symlink target was modified")
	}
}

func TestInspectNotFoundAndLinger(t *testing.T) {
	s, _ := testManager(t)
	s.run = func(_ context.Context, command string, args ...string) ([]byte, error) {
		if command == "loginctl" {
			return []byte("yes\n"), nil
		}
		return []byte("LoadState=not-found\nActiveState=inactive\n"), nil
	}
	status, err := s.Inspect(context.Background())
	if err != nil || status.Active || status.State != "not-found" || !status.Linger {
		t.Fatalf("status=%+v error=%v", status, err)
	}
}

func TestCancellationAndActionValidation(t *testing.T) {
	s, calls := testManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Install(ctx, "/mihomo", "/config", "/data"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if err := s.Action(context.Background(), "enable"); err == nil {
		t.Fatal("accepted unsupported action")
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected commands: %v", *calls)
	}
}

func TestSystemdQuotingAndPathValidation(t *testing.T) {
	quoted, err := quoteArg(`/tmp/a%t/$SECRET/"quoted"\bin`)
	if err != nil {
		t.Fatal(err)
	}
	if quoted != `"/tmp/a%%t/$SECRET/\"quoted\"\\bin"` {
		t.Fatalf("incorrect escaping: %s", quoted)
	}
	for _, path := range []string{"relative", "/tmp/new\nline", "/tmp/nul\x00"} {
		if _, err := quoteArg(path); err == nil {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
}

func TestCommandHidesStderrAndPreservesCancellation(t *testing.T) {
	s := &Systemd{}
	_, err := s.command(context.Background(), "sh", "-c", "printf 'secret-controller-token' >&2; exit 1")
	if err == nil || strings.Contains(err.Error(), "secret-controller-token") {
		t.Fatalf("subprocess output leaked: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = s.command(ctx, "sleep", "10")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline lost: %v", err)
	}
}

func TestVerifyRejectsAnotherInstallationModifiedUnitAndDropIns(t *testing.T) {
	for _, tc := range []struct {
		name     string
		content  string
		fragment string
		dropIns  string
		wantErr  bool
	}{
		{name: "owned"},
		{name: "another installation", content: "other", wantErr: true},
		{name: "edited unit", content: "edited", wantErr: true},
		{name: "different manager unit", fragment: "/other/lazyclash-test.service", wantErr: true},
		{name: "drop-in", dropIns: "/run/user/501/systemd/user/lazyclash-test.service.d/override.conf", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := testManager(t)
			path := filepath.Join(s.UnitDir, s.Name+".service")
			content, err := unitContent("/my/mihomo", "/my/config", "/my/data")
			if err != nil {
				t.Fatal(err)
			}
			switch tc.content {
			case "other":
				content, err = unitContent("/other/mihomo", "/other/config", "/other/data")
				if err != nil {
					t.Fatal(err)
				}
			case "edited":
				content += "\nEnvironment=MODIFIED=yes\n"
			}
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			fragment := tc.fragment
			if fragment == "" {
				fragment = path
			}
			s.run = func(context.Context, string, ...string) ([]byte, error) {
				return []byte("FragmentPath=" + fragment + "\nDropInPaths=" + tc.dropIns + "\n"), nil
			}
			err = s.Verify(context.Background(), "/my/mihomo", "/my/config", "/my/data")
			if (err != nil) != tc.wantErr {
				t.Fatalf("Verify error=%v wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				if err := s.RemoveOwned(context.Background(), "/my/mihomo", "/my/config", "/my/data"); err == nil {
					t.Fatal("RemoveOwned accepted foreign or modified unit")
				}
				if _, err := os.Stat(path); err != nil {
					t.Fatal("RemoveOwned removed protected unit")
				}
			}
		})
	}
}

func TestRemoveOwnedAllowsAbsentAndRemovesExactUnit(t *testing.T) {
	s, _ := testManager(t)
	if err := s.RemoveOwned(context.Background(), "/mihomo", "/config", "/data"); err != nil {
		t.Fatalf("absent unit should be harmless: %v", err)
	}
	path := filepath.Join(s.UnitDir, s.Name+".service")
	content, err := unitContent("/mihomo", "/config", "/data")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	s.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("FragmentPath=" + path + "\nDropInPaths=\nLoadState=loaded\nActiveState=inactive\n"), nil
	}
	if err := s.Remove(context.Background()); err == nil {
		t.Fatal("unrelated operation could remove unit")
	}
	if err := s.RemoveOwned(context.Background(), "/mihomo", "/config", "/data"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unit was not removed: %v", err)
	}
}

func TestRemoveOwnedPreservesRunningOrTransitioningService(t *testing.T) {
	for _, state := range []string{"active", "activating", "deactivating", "reloading", "unknown"} {
		t.Run(state, func(t *testing.T) {
			s, _ := testManager(t)
			path := filepath.Join(s.UnitDir, s.Name+".service")
			content, err := unitContent("/mihomo", "/config", "/data")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			s.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				return []byte("FragmentPath=" + path + "\nDropInPaths=\nLoadState=loaded\nActiveState=" + state + "\n"), nil
			}
			if err := s.RemoveOwned(context.Background(), "/mihomo", "/config", "/data"); err == nil {
				t.Fatal("removed service which could still own a process")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("running service definition was removed")
			}
		})
	}
}
