package shellintegration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func stateManager(t *testing.T, owner Identity) *Manager {
	t.Helper()
	t.Setenv(SessionVariable, "")
	t.Setenv(LoadedVariable, "")
	t.Setenv("LAZYCLASH_SHELL_PID", "")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{HomeDir: root, ConfigDir: filepath.Join(root, "config", "lazyclash", "shell"), StateDir: filepath.Join(root, "cache", "lazyclash", "shell")}
	m.caller = func() (Identity, error) { return owner, nil }
	m.attachCaller = func(shell string, pid int) (Identity, error) {
		if shell != owner.Shell || pid != owner.PID {
			return Identity{}, errors.New("wrong source owner")
		}
		return owner, nil
	}
	m.alive = func(Identity) bool { return true }
	return m
}

func testOwner(pid int) Identity {
	return Identity{UID: os.Getuid(), PID: pid, Start: "123", BootID: "test-boot", TTY: uint64(pid), Shell: "bash", PIDNamespace: "test-ns"}
}

func ackState(t *testing.T, m *Manager, id string, request Request, failed bool) {
	t.Helper()
	code := 0
	if failed {
		code = 1
	}
	data := fmt.Appendf(nil, "1 %d %d %d %d %d\n", request.Revision, bit(request.Enabled), request.Port, bit(request.Detach), code)
	if err := atomicFile(filepath.Join(m.sessionDir(id), "ack"), data); err != nil {
		t.Fatal(err)
	}
}

func loadedState(t *testing.T, id string, port int) {
	t.Helper()
	t.Setenv(SessionVariable, id)
	t.Setenv(LoadedVariable, "1")
	for _, name := range []string{"http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY"} {
		t.Setenv(name, fmt.Sprintf("http://127.0.0.1:%d", port))
	}
	for _, name := range []string{"all_proxy", "ALL_PROXY"} {
		t.Setenv(name, fmt.Sprintf("socks5h://127.0.0.1:%d", port))
	}
	t.Setenv("no_proxy", "original,localhost,127.0.0.1,::1")
	t.Setenv("NO_PROXY", "original,localhost,127.0.0.1,::1")
}

func TestPendingFirstSourceUsesLatestIntentAndRequiresApplicationACK(t *testing.T) {
	owner := testOwner(100)
	m := stateManager(t, owner)
	if err := m.Install("bash"); err != nil {
		t.Fatal(err)
	}
	endpoint := Endpoint{Namespace: "instance-one", Host: "127.0.0.1", Port: 7890}
	if err := m.Set(true, endpoint); err != nil {
		t.Fatal(err)
	}
	status, err := m.Current()
	if err != nil || status.State != "pending" || status.Applied || !status.Desired {
		t.Fatalf("pending = %+v, %v", status, err)
	}
	notice, err := m.TakeNotice()
	if err != nil || notice == "" {
		t.Fatalf("first notice %q %v", notice, err)
	}
	if notice, err = m.TakeNotice(); err != nil || notice != "" {
		t.Fatalf("duplicate notice %q %v", notice, err)
	}
	if err = m.Set(false, endpoint); err != nil {
		t.Fatal(err)
	}
	endpoint.Port = 8899
	if err = m.Set(true, endpoint); err != nil {
		t.Fatal(err)
	}
	if notice, err = m.TakeNotice(); err != nil || notice != "" {
		t.Fatalf("updated intent repeated notice %q %v", notice, err)
	}
	attachment, err := m.Attach("bash", owner.PID)
	if err != nil {
		t.Fatal(err)
	}
	request, err := m.readRequest(attachment.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if request.Port != 8899 || !request.Enabled || request.Revision != 3 {
		t.Fatalf("first source lost latest intent: %+v", request)
	}
	loadedState(t, attachment.SessionID, 8899)
	status, err = m.Current()
	if err != nil || status.State != "enabling" || status.Applied {
		t.Fatalf("unacknowledged status %+v %v", status, err)
	}
	ackState(t, m, attachment.SessionID, request, true)
	if err = m.FinalizeAttach(attachment.SessionID); err == nil {
		t.Fatal("failed synchronization consumed pending request")
	}
	if _, err = m.readPending(owner); err != nil {
		t.Fatal("pending intent lost after failed application", err)
	}
	ackState(t, m, attachment.SessionID, request, false)
	if err = m.FinalizeAttach(attachment.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.readPending(owner); !os.IsNotExist(err) {
		t.Fatalf("pending not migrated: %v", err)
	}
	status, err = m.Current()
	if err != nil || status.State != "on" || !status.Applied || !status.HookConfirmed {
		t.Fatalf("applied %+v %v", status, err)
	}
	if err = m.Set(false, Endpoint{Host: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	status, err = m.Current()
	if err != nil || status.State != "disabling" || !status.Applied {
		t.Fatalf("pending restore %+v %v", status, err)
	}
	request, err = m.readRequest(attachment.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	ackState(t, m, attachment.SessionID, request, false)
	status, err = m.Current()
	if err != nil || status.State != "off" || status.Applied {
		t.Fatalf("restored %+v %v", status, err)
	}
}

func TestSessionsAndPendingAreIsolatedEvenWithInheritedMarker(t *testing.T) {
	one, two := testOwner(100), testOwner(200)
	m := stateManager(t, one)
	if err := m.Install("bash"); err != nil {
		t.Fatal(err)
	}
	n := *m
	n.caller = func() (Identity, error) { return two, nil }
	n.attachCaller = func(string, int) (Identity, error) { return two, nil }
	endpoint := Endpoint{Namespace: "instance", Host: "127.0.0.1", Port: 7890}
	if err := m.Set(true, endpoint); err != nil {
		t.Fatal(err)
	}
	first, err := m.Attach("bash", one.PID)
	if err != nil {
		t.Fatal(err)
	}
	loadedState(t, first.SessionID, 7890)
	firstRequest, err := m.readRequest(first.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	ackState(t, m, first.SessionID, firstRequest, false)
	if err = n.Set(false, endpoint); err != nil {
		t.Fatal(err)
	}
	status, err := n.Current()
	if err != nil || status.Loaded || !status.Pending || status.Desired {
		t.Fatalf("inherited marker reused: %+v %v", status, err)
	}
	after, err := m.readRequest(first.SessionID)
	if err != nil || after != firstRequest {
		t.Fatalf("second terminal changed first: %+v %v", after, err)
	}
	second, err := n.Attach("bash", two.PID)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID == second.SessionID {
		t.Fatal("sessions share identifier")
	}
	secondRequest, err := n.readRequest(second.SessionID)
	if err != nil || secondRequest.Enabled {
		t.Fatalf("second terminal enabled %+v %v", secondRequest, err)
	}
	if err = m.Set(false, endpoint); err != nil {
		t.Fatal(err)
	}
	unchanged, err := n.readRequest(second.SessionID)
	if err != nil || unchanged != secondRequest {
		t.Fatalf("first terminal changed second: %+v %v", unchanged, err)
	}
	endpoint.Port = 8899
	if err = m.PublishEndpoint(endpoint); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{first.SessionID, second.SessionID} {
		raw, err := privateRead(filepath.Join(m.sessionDir(id), "endpoint"))
		if err != nil {
			t.Fatal(err)
		}
		ep, err := parseEndpoint(raw)
		if err != nil || ep.Port != 8899 {
			t.Fatalf("port update %q %v", raw, err)
		}
	}
	unchanged, err = n.readRequest(second.SessionID)
	if err != nil || unchanged.Enabled {
		t.Fatalf("port update enabled terminal two: %+v %v", unchanged, err)
	}
}

func TestPortPublicationRequiresNewACKAndDetectsExternalEnvironmentChanges(t *testing.T) {
	owner := testOwner(100)
	m := stateManager(t, owner)
	if err := m.Install("bash"); err != nil {
		t.Fatal(err)
	}
	ep := Endpoint{Namespace: "instance", Host: "127.0.0.1", Port: 7890}
	if err := m.Set(true, ep); err != nil {
		t.Fatal(err)
	}
	attachment, err := m.Attach("bash", owner.PID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.readRequest(attachment.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	ackState(t, m, attachment.SessionID, r, false)
	loadedState(t, attachment.SessionID, 7890)
	ep.Port = 8899
	if err = m.PublishEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	s, err := m.Current()
	if err != nil || s.State != "enabling" || s.MixedPort != 8899 {
		t.Fatalf("stale port ACK %+v %v", s, err)
	}
	r.Port = 8899
	ackState(t, m, attachment.SessionID, r, false)
	loadedState(t, attachment.SessionID, 8899)
	s, err = m.Current()
	if err != nil || s.State != "on" {
		t.Fatalf("new port ACK %+v %v", s, err)
	}
	t.Setenv("HTTP_PROXY", "user-changed")
	s, err = m.Current()
	if err != nil || s.State != "error" || s.Applied {
		t.Fatalf("external change claimed active %+v %v", s, err)
	}
}

func TestCorruptStateDoesNotCreateDisableRequestOrConsumeIntent(t *testing.T) {
	owner := testOwner(100)
	m := stateManager(t, owner)
	if err := m.Install("bash"); err != nil {
		t.Fatal(err)
	}
	ep := Endpoint{Host: "127.0.0.1", Port: 7890}
	if err := m.Set(true, ep); err != nil {
		t.Fatal(err)
	}
	a, err := m.Attach("bash", owner.PID)
	if err != nil {
		t.Fatal(err)
	}
	loadedState(t, a.SessionID, 7890)
	path := filepath.Join(a.SessionDir, "request")
	broken := []byte("not a valid request\n")
	if err = atomicFile(path, broken); err != nil {
		t.Fatal(err)
	}
	if err = m.Set(false, Endpoint{Host: "127.0.0.1"}); err == nil {
		t.Fatal("corrupt request treated as disabled")
	}
	after, err := privateRead(path)
	if err != nil || string(after) != string(broken) {
		t.Fatalf("corrupt state overwritten %q %v", after, err)
	}
	if _, err = m.readPending(owner); err != nil {
		t.Fatal("failure consumed pending", err)
	}
}

func TestUninstallWaitsForCurrentShellRestorationACK(t *testing.T) {
	owner := testOwner(100)
	m := stateManager(t, owner)
	if err := m.Install("bash"); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(true, Endpoint{Host: "127.0.0.1", Port: 7890}); err != nil {
		t.Fatal(err)
	}
	a, err := m.Attach("bash", owner.PID)
	if err != nil {
		t.Fatal(err)
	}
	loadedState(t, a.SessionID, 7890)
	id, wait, err := m.RequestDetach()
	if err != nil || !wait || id != a.SessionID {
		t.Fatalf("detach %q %v %v", id, wait, err)
	}
	if ready, err := m.UninstallReady(id); err != nil || ready {
		t.Fatalf("premature cleanup %v %v", ready, err)
	}
	if err = m.NoteUninstallFailure(id, errors.New("restoration timeout")); err != nil {
		t.Fatal(err)
	}
	if status, err := m.Current(); err != nil || !strings.Contains(status.Detail, "restoration timeout") {
		t.Fatalf("deferred cleanup reason missing %+v %v", status, err)
	}
	if err = m.Set(true, Endpoint{Host: "127.0.0.1", Port: 7890}); err == nil {
		t.Fatal("proxy request canceled uninstall restoration")
	}
	retried, waiting, err := m.RequestDetach()
	if err != nil || !waiting || retried != id {
		t.Fatalf("uninstall retry lost ticket %s %v %v", retried, waiting, err)
	}
	r, err := m.readRequest(id)
	if err != nil {
		t.Fatal(err)
	}
	ackState(t, m, id, r, true)
	if ready, err := m.UninstallReady(id); err != nil || ready {
		t.Fatalf("failed restoration cleanup %v %v", ready, err)
	}
	ackState(t, m, id, r, false)
	if ready, err := m.UninstallReady(id); err != nil || !ready {
		t.Fatalf("successful restoration not ready %v %v", ready, err)
	}
	t.Setenv(SessionVariable, "")
	t.Setenv(LoadedVariable, "")
	if status, err := m.Current(); err != nil || !strings.Contains(status.Detail, "restoration timeout") {
		t.Fatalf("ticket disappeared after marker cleanup %+v %v", status, err)
	}
	if err = m.FinalizeUninstall(); err != nil {
		t.Fatal(err)
	}
	if err = m.FinishDetachedSession(id); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(m.sessionDir(id)); !os.IsNotExist(err) {
		t.Fatalf("detached session retained %v", err)
	}
	if _, err = os.Stat(m.uninstallTicketPath(id)); !os.IsNotExist(err) {
		t.Fatalf("uninstall ticket retained %v", err)
	}
}

func TestLoadedOtherTerminalKeepsWorkingAfterUninstall(t *testing.T) {
	one, two := testOwner(100), testOwner(200)
	m := stateManager(t, one)
	if err := m.Install("bash"); err != nil {
		t.Fatal(err)
	}
	n := *m
	n.caller = func() (Identity, error) { return two, nil }
	n.attachCaller = func(string, int) (Identity, error) { return two, nil }
	ep := Endpoint{Namespace: "instance", Host: "127.0.0.1", Port: 7890}
	if err := m.Set(true, ep); err != nil {
		t.Fatal(err)
	}
	a, err := m.Attach("bash", one.PID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.readRequest(a.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	ackState(t, m, a.SessionID, r, false)
	if err = m.FinalizeAttach(a.SessionID); err != nil {
		t.Fatal(err)
	}
	b, err := n.Attach("bash", two.PID)
	if err != nil {
		t.Fatal(err)
	}
	loadedState(t, b.SessionID, 7890)
	if err = n.Set(true, ep); err != nil {
		t.Fatal(err)
	}
	loadedState(t, a.SessionID, 7890)
	id, wait, err := m.RequestDetach()
	if err != nil || !wait {
		t.Fatalf("detach %v %v", wait, err)
	}
	r, err = m.readRequest(id)
	if err != nil {
		t.Fatal(err)
	}
	ackState(t, m, id, r, false)
	if err = m.FinalizeUninstall(); err != nil {
		t.Fatal(err)
	}
	if err = m.FinishDetachedSession(id); err != nil {
		t.Fatal(err)
	}
	loadedState(t, b.SessionID, 7890)
	if err = n.Set(false, Endpoint{Host: "127.0.0.1"}); err != nil {
		t.Fatal("other live terminal cannot disable after uninstall", err)
	}
	if err = n.Set(true, ep); err != nil {
		t.Fatal("other live terminal cannot re-enable after uninstall", err)
	}
	if _, err = n.readRequest(b.SessionID); err != nil {
		t.Fatal("other terminal state removed", err)
	}
}

func TestGCRetainsLiveSessionsAndExpiresDeadOrOldPending(t *testing.T) {
	owner := testOwner(100)
	m := stateManager(t, owner)
	if err := m.ensureState(); err != nil {
		t.Fatal(err)
	}
	p := pendingRecord{Owner: owner, Enabled: true, Endpoint: Endpoint{Host: "127.0.0.1", Port: 7890}, Revision: 1, UpdatedAt: time.Now().Add(-pendingTTL - time.Second)}
	if err := jsonFile(m.pendingPath(owner), p); err != nil {
		t.Fatal(err)
	}
	a, err := m.Attach("bash", owner.PID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.readPending(owner); !os.IsNotExist(err) {
		t.Fatalf("expired pending kept: %v", err)
	}
	var record ownerRecord
	if err = readJSON(filepath.Join(a.SessionDir, "owner.json"), &record); err != nil {
		t.Fatal(err)
	}
	record.UpdatedAt = time.Now().Add(-365 * 24 * time.Hour)
	if err = jsonFile(filepath.Join(a.SessionDir, "owner.json"), record); err != nil {
		t.Fatal(err)
	}
	m.gcLocked()
	if _, err = os.Stat(a.SessionDir); err != nil {
		t.Fatal("live session expired by age", err)
	}
	m.alive = func(Identity) bool { return false }
	m.gcLocked()
	if _, err = os.Stat(a.SessionDir); !os.IsNotExist(err) {
		t.Fatalf("dead session kept: %v", err)
	}
}

func TestStateRejectsSymlinkAndWorldReadableFilesWithoutWriting(t *testing.T) {
	owner := testOwner(100)
	m := stateManager(t, owner)
	outside := filepath.Join(m.HomeDir, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(m.StateDir), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, m.StateDir); err != nil {
		t.Fatal(err)
	}
	if err := m.ensureState(); err == nil {
		t.Fatal("symlink state directory accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("symlink target modified %v %v", entries, err)
	}
	path := filepath.Join(outside, "state")
	if err = os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = privateRead(path); err == nil {
		t.Fatal("world-readable state accepted")
	}
	if err = atomicFile(path, []byte("y")); err == nil {
		t.Fatal("unsafe existing state replaced")
	}
}

func TestProtocolRejectsMalformedRecords(t *testing.T) {
	for _, value := range []string{"", "1 0 1 127.0.0.1 7890 0", "1 1 1 evil 7890 0", "1 1 1 127.0.0.1 0 0", "1 1 2 127.0.0.1 7890 0", "1 1 1 127.0.0.1 7890 0 extra", "1 1 1 127.0.0.1 65536 0"} {
		if _, err := parseRequest([]byte(value)); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	for _, value := range []string{"", "1 0 1 7890 0 0", "1 1 1 65536 0 0", "1 1 1 7890 3 0", "1 1 1 7890 0 256"} {
		if _, err := parseACK([]byte(value)); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	if _, err := parseRequest(requestBytes(Request{Revision: 1, Host: "127.0.0.1"})); err != nil {
		t.Fatal(err)
	}
}

func TestNewAndUnsupportedCurrentDoNotCreateStorage(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	m, err := New()
	if err != nil {
		t.Fatal(err)
	}
	m.caller = func() (Identity, error) { return Identity{}, errors.New("no verified terminal") }
	s, err := m.Current()
	if err != nil || s.State != "unsupported" || !strings.Contains(s.Detail, "verified") {
		t.Fatalf("unsupported %+v %v", s, err)
	}
	for _, path := range []string{m.ConfigDir, m.StateDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("query created %s: %v", path, err)
		}
	}
}

func TestNewRejectsLinebreakInFramedIntegrationPaths(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, root+"/line\nbreak")
			if _, err := New(); err == nil {
				t.Fatal("newline path accepted by newline-framed attach protocol")
			}
		})
	}
}

func TestGCRemovesInterruptedSessionTemporariesAndPreservesUnknownFiles(t *testing.T) {
	owner := testOwner(100)
	m := stateManager(t, owner)
	a, err := m.Attach("bash", owner.PID)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ack.100.tmp", ".lazyclash-interrupted"} {
		if err = atomicFile(filepath.Join(a.SessionDir, name), []byte("temporary")); err != nil {
			t.Fatal(err)
		}
	}
	m.alive = func(Identity) bool { return false }
	m.gcLocked()
	if _, err = os.Stat(a.SessionDir); !os.IsNotExist(err) {
		t.Fatalf("interrupted temporaries prevented cleanup: %v", err)
	}
	m.alive = func(Identity) bool { return true }
	a, err = m.Attach("bash", owner.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err = atomicFile(filepath.Join(a.SessionDir, "user-note"), []byte("keep")); err != nil {
		t.Fatal(err)
	}
	m.alive = func(Identity) bool { return false }
	m.gcLocked()
	for _, name := range []string{"user-note", "owner.json", "request"} {
		if _, err = os.Stat(filepath.Join(a.SessionDir, name)); err != nil {
			t.Fatalf("unknown content caused partial cleanup: %s %v", name, err)
		}
	}
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	orphan := m.sessionDir(id)
	if err = secureDir(orphan); err != nil {
		t.Fatal(err)
	}
	if err = atomicFile(filepath.Join(orphan, ".lazyclash-interrupted"), []byte("temporary")); err != nil {
		t.Fatal(err)
	}
	m.gcLocked()
	if _, err = os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("generated orphan retained: %v", err)
	}
}
