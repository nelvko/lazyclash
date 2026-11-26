# 工程质量审计复现附录 · 2026-11-26

以下三段命令复现主报告中的三个高优先级问题及一项 Go 数据竞争。使用主报告标注的源码基线，在仓库根目录执行。Go overlay 只改变本次测试的编译输入，测试中的端口、数据目录和服务对象均为隔离资源。命令中的预期现象对应审计当日版本。

[返回审计报告](engineering-quality-audit-2026-11-26.md)

## F01　DNS 策略顺序

将 `LAZYCLASH_TEST_MIHOMO` 设置为本机可执行的 Mihomo 绝对路径，在仓库任意子目录复制运行。脚本通过 Go overlay 调用项目实际的 `mihomo.Build`，依次启动两个临时核心进程。两个本机 UDP 服务器对同一测试域名返回不同地址，无需公网 DNS。审计版本预期输出 `original ['192.0.2.10']` 和 `built ['192.0.2.20']`。修复后的两次应均为 `192.0.2.10`。

```sh
python3 - <<'PY'
import json, os, pathlib, socket, subprocess, tempfile, threading, time, urllib.request

core = pathlib.Path(os.environ["LAZYCLASH_TEST_MIHOMO"]).resolve(strict=True)
repo = pathlib.Path(subprocess.check_output(["go", "env", "GOMOD"], text=True).strip()).resolve().parent

def dns_fixture(ip):
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.bind(("127.0.0.1", 0))
    def serve():
        while True:
            data, peer = sock.recvfrom(4096)
            end = 12
            while data[end]:
                end += data[end] + 1
            question = data[12:end + 5]
            header = data[:2] + bytes.fromhex("81800001000100000000")
            answer = bytes.fromhex("c00c000100010000000a0004") + socket.inet_aton(ip)
            sock.sendto(header + question + answer, peer)
    threading.Thread(target=serve, daemon=True).start()
    return sock.getsockname()[1]

z, a = dns_fixture("192.0.2.10"), dns_fixture("192.0.2.20")
with tempfile.TemporaryDirectory() as temp:
    root = pathlib.Path(temp).resolve()
    with socket.socket() as api:
        api.bind(("127.0.0.1", 0))
        controller = "127.0.0.1:" + str(api.getsockname()[1])
    raw = f"""mode: rule
external-controller: {controller}
secret: fixture
log-level: silent
rules: ["MATCH,DIRECT"]
rule-providers:
  z-first: {{type: inline, behavior: domain, payload: [fixture.example]}}
  a-second: {{type: inline, behavior: domain, payload: [fixture.example]}}
dns:
  enable: true
  use-hosts: false
  use-system-hosts: false
  nameserver: [udp://127.0.0.1:{z}]
  nameserver-policy:
    rule-set:z-first: [udp://127.0.0.1:{z}]
    rule-set:a-second: [udp://127.0.0.1:{a}]
"""
    (root / "main.go").write_text("""package main
import ("io"; "os"; "github.com/nelvko/lazyclash/internal/mihomo")
func main() {
    raw, err := io.ReadAll(os.Stdin)
    if err != nil { panic(err) }
    built, err := mihomo.Build(mihomo.BuildOptions{Base: raw, Controller: os.Args[1], Secret: "fixture", Mode: "rule"})
    if err != nil { panic(err) }
    if _, err = os.Stdout.Write(built); err != nil { panic(err) }
}
""")
    overlay = root / "overlay.json"
    overlay.write_text(json.dumps({"Replace": {str(repo / "main.go"): str(root / "main.go")}}))
    built = subprocess.check_output(["go", "run", "-overlay", str(overlay), ".", controller],
                                    cwd=repo, input=raw.encode())
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    for name, content in [("original", raw.encode()), ("built", built)]:
        home = root / name
        home.mkdir()
        (home / "config.yaml").write_bytes(content)
        proc = subprocess.Popen([str(core), "-d", str(home), "-f", str(home / "config.yaml")],
                                stdout=subprocess.DEVNULL)
        try:
            req = urllib.request.Request("http://" + controller + "/dns/query?name=fixture.example&type=A",
                                         headers={"Authorization": "Bearer fixture"})
            for _ in range(60):
                try:
                    with client.open(req, timeout=1) as resp:
                        print(name, [row["data"] for row in json.load(resp)["Answer"]])
                    break
                except OSError:
                    if proc.poll() is not None:
                        raise RuntimeError("测试内核提前退出")
                    time.sleep(0.1)
            else:
                raise TimeoutError("未取得本机 DNS 回答")
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=3)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()
PY
```

## F02　CLI 单字段更新

在 Go 1.26+ 仓库根目录或其子目录运行下面这段 shell 命令。它通过 Go overlay 加载一个临时测试，使用真实 Cobra 命令、应用层事务和临时安装目录。服务替身始终处于停止状态。审计版本会输出控制端口从 B 的新值退回 A 的旧值。

```sh
(
set -eu
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
root=$(dirname "$(go env GOMOD)")
cat > "$scratch/repro_test.go" <<'GO'
package cli

import (
	"context"
	"io"
	"net"
	"strconv"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
)

type interleavedRead struct {
	*app.App
	afterRead func()
}

func (a interleavedRead) GetRuntimeSettings(ctx context.Context) (app.RuntimeSettings, error) {
	value, err := a.App.GetRuntimeSettings(ctx)
	if err == nil {
		a.afterRead()
	}
	return value, err
}

func TestAuditPortOverwrite(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := logsBackend(t)
	a.Service = &installService{}
	ctx := t.Context()
	initial, err := a.GetRuntimeSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(base, next app.RuntimeSettings) {
		plan, err := a.PrepareRuntimeSettingsEdit(ctx, base, next)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.ApplyRuntimeSettings(ctx, plan); err != nil {
			t.Fatal(err)
		}
	}
	var ports []int
	var listeners []io.Closer
	for attempts := 0; attempts < 32 && len(ports) < 4; attempts++ {
		tcp, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		udp, err := net.ListenPacket("udp4", tcp.Addr().String())
		if err != nil {
			tcp.Close()
			continue
		}
		listeners = append(listeners, tcp, udp)
		ports = append(ports, tcp.Addr().(*net.TCPAddr).Port)
	}
	for _, l := range listeners {
		l.Close()
	}
	if len(ports) != 4 {
		t.Fatal("four free fixture ports required")
	}
	base := initial
	base.MixedPort, base.ControllerPort = ports[0], ports[1]
	apply(initial, base)
	other := base
	other.ControllerPort = ports[2]
	cmd := runtimeSettingsCommand(func() runtimeSettingsBackend {
		return interleavedRead{a, func() { apply(base, other) }}
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"ports", "--mixed-port", strconv.Itoa(ports[3])})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}
	saved, err := a.GetRuntimeSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("controller before B=%d, after B=%d, after A=%d", ports[1], ports[2], saved.ControllerPort)
	if saved.ControllerPort != ports[1] || saved.MixedPort != ports[3] {
		t.Fatal("未复现旧值覆盖")
	}
}
GO
python3 - "$root" "$scratch" <<'PY'
import json, pathlib, sys
repo, tmp = [pathlib.Path(s).resolve() for s in sys.argv[1:]]
(tmp / "overlay.json").write_text(json.dumps({"Replace": {
    str(repo / "internal/cli/audit_repro_test.go"): str(tmp / "repro_test.go")
}}))
PY
go -C "$root" test -overlay "$scratch/overlay.json" -run '^TestAuditPortOverwrite$' -v -count=1 ./internal/cli
)
```

## F03 与 F05　恢复日志和并发检查更新

在与报告一致的源码根目录运行下方 Bash 代码，需要 Go、Python 3 和可用的 Go 竞态检测器。测试复用仓库的 `installedApp`、`updateFixture` 和临时安装目录。Go overlay 增加一个测试文件，并在编译时为 `store.Replace` 增加一个可控的 I/O 错误。命令结束后删除临时文件。

```bash
(
set -eu
audit_dir=$(mktemp -d)
trap 'rm -rf "$audit_dir"' EXIT
cat > "$audit_dir/audit_test.go" <<'GO'
package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/service"
	"golang.org/x/sys/unix"
)

func TestAuditJournalPublishedBeforeSyncError(t *testing.T) {
	a, _ := installedApp(t)
	journal := filepath.Join(a.Home, "apply-pending.json")
	t.Setenv("LAZYCLASH_AUDIT_SYNC_FAILURE", journal)
	if err := a.SetMode(t.Context(), "global"); !errors.Is(err, unix.EIO) {
		t.Fatalf("SetMode returned %v", err)
	}
	if _, err := os.Stat(journal); err != nil {
		t.Fatal("the journal should have been published", err)
	}
	t.Setenv("LAZYCLASH_AUDIT_SYNC_FAILURE", "")
	_, err := a.Snapshot(t.Context())
	if err == nil || !strings.Contains(err.Error(), "recovery generation changed") {
		t.Fatalf("Snapshot returned %v", err)
	}
	t.Log(err)
}

func TestAuditConcurrentUpdateChecks(t *testing.T) {
	a, _ := updateFixture(t, "v1.19.0")
	a.Service = &service.Systemd{Name: a.Name, Scope: service.User, UnitDir: t.TempDir()}
	gate := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			<-gate
			for range 3 {
				if _, err := a.CheckUpdate(t.Context(), "", downloadnet.Config{}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	close(gate)
	wg.Wait()
}
GO
python3 - "$audit_dir" <<'PY'
import json
import sys
from pathlib import Path

repo = Path.cwd().resolve()
work = Path(sys.argv[1]).resolve()
store = repo / "internal/store/store.go"
source = store.read_text()
needle = "return SyncDir(filepath.Dir(path))"
pos = source.index(needle, source.index("func Replace("))
assert pos < source.index("func Remove(")
inject = 'if p := os.Getenv("LAZYCLASH_AUDIT_SYNC_FAILURE"); p != "" && p == path { return unix.EIO }\n\t'
(work / "store.go").write_text(source[:pos] + inject + source[pos:])
tests = {str(repo / "internal/app/quality_audit_test.go"): str(work / "audit_test.go")}
for name, replacements in (("read", tests), ("sync", {**tests, str(store): str(work / "store.go")})):
    (work / (name + ".json")).write_text(json.dumps({"Replace": replacements}))
PY
go test -count=1 -v -overlay "$audit_dir/sync.json" \
    -run '^TestAuditJournalPublishedBeforeSyncError$' ./internal/app
if go test -count=1 -race -overlay "$audit_dir/read.json" \
    -run '^TestAuditConcurrentUpdateChecks$' ./internal/app > "$audit_dir/race.log" 2>&1
then
    echo '本次没有检测到竞争'
    exit 1
fi
grep -F 'WARNING: DATA RACE' "$audit_dir/race.log"
grep -F -m 2 '/internal/app/tun.go:35' "$audit_dir/race.log"
)
```

第一项直接模拟日志链接已创建、父目录同步返回 `EIO` 的分支。审计基线上测试通过，并打印 `recovery generation changed; preserving files and journal for manual inspection`。第二项只向构建添加测试，用一个已安装的 `App` 同时查新。预期 Go 竞态检测器以失败状态退出，两次写均指向 `internal/app/tun.go:35`。两项均不连接宿主 systemd 或外部订阅。
