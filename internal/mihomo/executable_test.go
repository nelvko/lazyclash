package mihomo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivilegedExecutableRejectedBeforeAnyExecution(t *testing.T) {
	for _, mode := range []os.FileMode{os.ModeSetuid, os.ModeSetgid} {
		t.Run(mode.String(), func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "executed")
			bin := filepath.Join(dir, "mihomo")
			if err := os.WriteFile(bin, []byte("#!/bin/sh\ntouch '"+marker+"'\necho 'Mihomo Meta v1.19.33 linux amd64'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(bin, 0700|mode); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(bin)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode()&mode == 0 {
				t.Skip("filesystem does not retain the privilege mode")
			}
			if _, err := BinaryVersion(t.Context(), bin); err == nil {
				t.Fatal("privileged version check was executed")
			}
			if err := Validate(t.Context(), bin, filepath.Join(dir, "config.yaml"), dir); err == nil {
				t.Fatal("privileged validation was executed")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("rejected executable ran before privilege check")
			}
			link := filepath.Join(dir, "linked-mihomo")
			if err := os.Symlink(bin, link); err != nil {
				t.Fatal(err)
			}
			if err := SafeExecutable(link); err == nil {
				t.Fatal("privileged symlink target was accepted")
			}
		})
	}
}

func TestSafeExecutableRequiresOrdinaryExecutableFile(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mihomo")
	if err := os.WriteFile(bin, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, bin, filepath.Join(dir, "missing")} {
		if err := SafeExecutable(path); err == nil {
			t.Fatalf("accepted non-executable file %s", filepath.Base(path))
		}
	}
	if err := os.Chmod(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := SafeExecutable(bin); err != nil {
		t.Fatalf("ordinary executable rejected: %v", err)
	}
}
