//go:build linux

package mihomo

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFileCapabilityRejectedBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	bin := filepath.Join(dir, "mihomo")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntouch '"+marker+"'\necho 'Mihomo Meta v1.19.33 linux amd64'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	// VFS_CAP_REVISION_2 plus EFFECTIVE, with CAP_NET_ADMIN in permitted.
	capability := make([]byte, 20)
	binary.LittleEndian.PutUint32(capability[0:4], 0x02000001)
	binary.LittleEndian.PutUint32(capability[4:8], 1<<12)
	if err := unix.Setxattr(bin, "security.capability", capability, 0); err != nil {
		t.Skipf("requires a filesystem and test user permitted to set file capabilities: %v", err)
	}
	if _, err := BinaryVersion(t.Context(), bin); err == nil || !strings.Contains(err.Error(), "file capabilities") {
		t.Fatalf("capability-bearing version command accepted: %v", err)
	}
	if err := Validate(t.Context(), bin, filepath.Join(dir, "config.yaml"), dir); err == nil || !strings.Contains(err.Error(), "file capabilities") {
		t.Fatalf("capability-bearing validation accepted: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("capability-bearing executable ran before rejection")
	}
}
