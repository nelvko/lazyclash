#!/usr/bin/env bash
# Opt-in real Mihomo + systemd + controlling-PTY checks. Never run by go test.
set -euo pipefail
if [[ ${1:-} != --linux ]]; then
  if [[ $# != 1 && $# != 3 ]]; then
    printf 'Usage: %s ORBSTACK_VM [--proxy-mode none|environment]\n' "$0" >&2
    exit 2
  fi
  vm=$1
  proxy_mode=none
  if [[ $# == 3 ]]; then
    [[ $2 == --proxy-mode && ($3 == none || $3 == environment) ]] || { printf 'Expected --proxy-mode none|environment\n' >&2; exit 2; }
    proxy_mode=$3
  fi
  root=$(cd "$(dirname "$0")/.." && pwd)
  cd "$root"
  arch=$(orb -m "$vm" uname -m)
  case "$arch" in
    aarch64|arm64) goarch=arm64 ;;
    x86_64) goarch=amd64 ;;
    *) printf 'Unsupported VM architecture: %s\n' "$arch" >&2; exit 1 ;;
  esac
  host_stage=$(mktemp -d "${TMPDIR:-/tmp}/lazyclash-e2e.XXXXXX")
  linux_stage=
  mkdir -p .test-artifacts
  cleanup_host() {
    result=$?
    trap - EXIT
    if [[ -n $linux_stage ]]; then
    for file in pty-empty.txt pty-install-local.txt pty-installed.txt pty-runtime.txt pty-rules.txt pty-logs.txt cleanup.txt; do
      if orb -m "$vm" test -f "$linux_stage/$file"; then
        orb -m "$vm" cat "$linux_stage/$file" > ".test-artifacts/$file" || result=1
      fi
    done
    if orb -m "$vm" test -f "$linux_stage/.resources-started" && ! orb -m "$vm" test -f "$linux_stage/.cleanup-complete"; then
      printf 'Linux cleanup needs inspection; preserving temporary files in VM %s: %s\n' "$vm" "$linux_stage" >&2
      result=1
    else
      orb -m "$vm" rm -rf -- "$linux_stage" || result=1
    fi
    fi
    rm -rf -- "$host_stage"
    exit "$result"
  }
  trap cleanup_host EXIT
  linux_stage=$(orb -m "$vm" mktemp -d /tmp/lazyclash-e2e.XXXXXX)
  CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -o "$host_stage/lazyclash" .
  cp scripts/e2e.sh scripts/pty-smoke.py "$host_stage/"
  COPYFILE_DISABLE=1 tar --no-xattrs -C "$host_stage" -cf - lazyclash e2e.sh pty-smoke.py | orb -m "$vm" tar -xf - -C "$linux_stage"
  # Captured output deliberately excludes raw configuration, journal and URLs.
  if orb -m "$vm" bash "$linux_stage/e2e.sh" --linux "$linux_stage" "$proxy_mode" > .test-artifacts/e2e.log 2>&1; then
    sed '/^download: [0-9]/d' .test-artifacts/e2e.log
  else
    result=$?
    sed '/^download: [0-9]/d' .test-artifacts/e2e.log >&2
    exit "$result"
  fi
  exit 0
fi

work=$2
proxy_mode=${3:-none}
cd "$work"
test "$(uname -s)" = Linux
test "$(id -u)" != 0
command -v python3 >/dev/null
command -v systemctl >/dev/null
systemctl --user show --property=SystemState --value
service_name="lazyclash-e2e-$(python3 -c 'import uuid; print(uuid.uuid4().hex[:12])')"
home="$work/home"
app="$work/lazyclash"
unit_dir="${XDG_CONFIG_HOME:-$(getent passwd "$(id -u)" | cut -d: -f6)/.config}/systemd/user"
unit="$unit_dir/$service_name.service"
fixture_pid=
traffic_pid=
owned=no
local_owned=no
local_home=
local_unit=
cleanup_linux() {
  result=$?
  trap - EXIT
  cleanup_failed=0
  if [[ -n "$traffic_pid" ]]; then
    kill "$traffic_pid" 2>/dev/null || true
    wait "$traffic_pid" 2>/dev/null || true
  fi
  # Preflight proves absence, then the unit must refer to this run's private
  # temporary installation. Never touch a pre-existing or replaced unit.
  if [[ $owned == yes && -f "$unit" && ! -L "$unit" ]] && grep -Fq "$home/installation/mihomo" "$unit"; then
    systemctl --user stop "$service_name.service" || cleanup_failed=1
    systemctl --user show "$service_name.service" --property=LoadState,ActiveState,SubState,Result,ExecMainStatus > "$work/cleanup.txt" || true
    state=$(systemctl --user show "$service_name.service" --property=ActiveState --value) || cleanup_failed=1
    if [[ $state == inactive || $state == failed ]]; then
      rm -- "$unit" || cleanup_failed=1
      systemctl --user daemon-reload || cleanup_failed=1
      systemctl --user reset-failed "$service_name.service" 2>/dev/null || true
      state=$(systemctl --user show "$service_name.service" --property=LoadState --value) || cleanup_failed=1
      [[ $state == not-found ]] || cleanup_failed=1
      printf 'Dedicated unit removed; manager LoadState=%s\n' "$state" >> "$work/cleanup.txt"
    else
      cleanup_failed=1
    fi
  elif [[ $owned == yes ]]; then
    [[ $(systemctl --user show "$service_name.service" --property=LoadState --value) == not-found ]] || cleanup_failed=1
  fi
  if [[ $local_owned == yes && -f "$local_unit" && ! -L "$local_unit" ]] && grep -Fq "$local_home/installation/mihomo" "$local_unit"; then
    systemctl --user stop "$service_name-local.service" || cleanup_failed=1
    state=$(systemctl --user show "$service_name-local.service" --property=ActiveState --value) || cleanup_failed=1
    if [[ $state == inactive || $state == failed ]]; then
      rm -- "$local_unit" || cleanup_failed=1
      systemctl --user daemon-reload || cleanup_failed=1
      systemctl --user reset-failed "$service_name-local.service" 2>/dev/null || true
    else
      cleanup_failed=1
    fi
  elif [[ $local_owned == yes ]]; then
    [[ $(systemctl --user show "$service_name-local.service" --property=LoadState --value) == not-found ]] || cleanup_failed=1
  fi
  if [[ -n "$fixture_pid" ]]; then
    kill "$fixture_pid" 2>/dev/null || true
    wait "$fixture_pid" 2>/dev/null || true
  fi
  # Check listener ownership through process termination, never free a conflict
  # by stopping somebody else's service.
  python3 - <<'PY' >> "$work/cleanup.txt" || cleanup_failed=1
import socket
for port in (9097, 7897):
    s = socket.socket()
    # Match Go's TCP listener: TIME_WAIT from a closed fixture is harmless,
    # while an existing listener still prevents this bind (no SO_REUSEPORT).
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind(("127.0.0.1", port))
    s.close()
print("Both dedicated test listeners are released")
PY
  if [[ $cleanup_failed == 0 ]]; then
    touch "$work/.cleanup-complete"
  else
    result=1
  fi
  printf 'Cleanup resources released=%s\n' "$((1 - cleanup_failed))" >> "$work/cleanup.txt"
  printf 'Cleanup exit=%s\n' "$result" >> "$work/cleanup.txt"
  exit "$result"
}

# Check all conflicts before registering a cleanup which touches test resources.
[[ ! -e "$unit" && ! -L "$unit" ]]
[[ $(systemctl --user show "$service_name.service" --property=LoadState --value) == not-found ]]
python3 - <<'PY'
import socket
for port in (9097, 7897):
    s = socket.socket()
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    try:
        s.bind(("127.0.0.1", port))
    except OSError:
        raise SystemExit("Test port conflict: %d; no process was stopped" % port)
    finally:
        s.close()
print("Port and unit conflict preflight passed")
PY
trap cleanup_linux EXIT
touch "$work/.resources-started"
lp() { "$app" --home "$home" --service-name "$service_name" "$@"; }
python3 "$work/pty-smoke.py" --binary "$app" --home "$home" --service-name "$service_name" --mode empty --output "$work/pty-empty.txt"
[[ ! -e "$home/installation" && ! -e "$unit" && ! -L "$unit" ]]
printf 'Install official pinned Mihomo release (version/checksum verified by application)\n'
printf 'Explicit download connection mode: %s\n' "$proxy_mode"
owned=yes
lp install --core-version v1.19.32 --proxy-mode "$proxy_mode"
lp status > "$work/status.json"
python3 - "$work/status.json" <<'PY'
import json, sys
s = json.load(open(sys.argv[1]))
assert s["Installed"] and s["Applied"] == "default", s
assert s["Version"] == "v1.19.32", s["Version"]
assert "preimport" in s["UnvalidatedProfiles"], s
print("Pinned Mihomo installed with valid default configuration")
PY
# A second --bin install uses a separate unit while both services are stopped.
# Its definition is removed immediately, without an uninstall product feature.
local_name="$service_name-local"
local_home="$work/local-home"
local_unit="$unit_dir/$local_name.service"
[[ ! -e "$local_unit" && ! -L "$local_unit" ]]
[[ $(systemctl --user show "$local_name.service" --property=LoadState --value) == not-found ]]
local_owned=yes
python3 "$work/pty-smoke.py" --binary "$app" --home "$local_home" --service-name "$local_name" --mode install-local --mihomo-bin "$home/installation/mihomo" --output "$work/pty-install-local.txt"
"$app" --home "$local_home" --service-name "$local_name" install --bin "$home/installation/mihomo" > "$work/local-result.json"
python3 - "$work/local-result.json" <<'PY'
import json, sys
assert json.load(open(sys.argv[1]))["Operation"] == "use existing"
print("TUI local installation and CLI same-version reuse passed")
PY
if [[ -f "$local_unit" && ! -L "$local_unit" ]] && grep -Fq "$local_home/installation/mihomo" "$local_unit"; then
  rm -- "$local_unit"
  systemctl --user daemon-reload
  local_owned=no
else
  printf 'Local installation ownership check failed\n' >&2
  exit 1
fi
[[ $(systemctl --user show "$local_name.service" --property=LoadState --value) == not-found ]]
printf 'Local --bin installation passed and dedicated unit cleaned\n'

lp service start
lp nodes > "$work/nodes.json"
python3 - "$work/nodes.json" <<'PY'
import json, sys
groups = json.load(open(sys.argv[1]))
assert any(g["name"] == "PROXY" and "DIRECT" in g["all"] for g in groups), groups
print("Real default API exposes PROXY and DIRECT/REJECT")
PY
python3 "$work/pty-smoke.py" --binary "$app" --home "$home" --service-name "$service_name" --mode installed --output "$work/pty-installed.txt"

# Update the installed path while the old inode remains an actual running
# Mihomo process. Installation must leave its PID, API version and config alone.
old_pid=$(systemctl --user show "$service_name.service" --property=MainPID --value)
config_hash=$(python3 - "$home/installation/current/config.yaml" <<'PY'
import hashlib, sys
print(hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest())
PY
)
lp install --core-version v1.19.33 --proxy-mode "$proxy_mode" > "$work/update-result.json"
[[ $(systemctl --user show "$service_name.service" --property=MainPID --value) == "$old_pid" ]]
lp status > "$work/update-status.json"
python3 - "$work/update-result.json" "$work/update-status.json" "$home/installation/current/config.yaml" "$config_hash" <<'PY'
import hashlib, json, sys
r=json.load(open(sys.argv[1])); s=json.load(open(sys.argv[2]))
assert r["Operation"] == "update" and r["InstalledVersion"] == "v1.19.33" and r["RunningVersion"] == "v1.19.32" and r["RestartRequired"], r
assert s["Version"] == "v1.19.33" and s["RunningVersion"] == "v1.19.32" and s["Service"]["Active"], s
assert hashlib.sha256(open(sys.argv[3], "rb").read()).hexdigest() == sys.argv[4]
print("Real update preserved running PID, old API version and complete config bytes")
PY
lp install --bin "$home/installation/mihomo" > "$work/reuse-result.json"
lp install --bin "$home/installation/mihomo" --reinstall > "$work/reinstall-result.json"
[[ $(systemctl --user show "$service_name.service" --property=MainPID --value) == "$old_pid" ]]
python3 - "$work/reuse-result.json" "$work/reinstall-result.json" <<'PY'
import json, sys
assert json.load(open(sys.argv[1]))["Operation"] == "use existing"
r=json.load(open(sys.argv[2])); assert r["Operation"] == "reinstall" and r["RestartRequired"], r
print("Same-version reuse and explicit reinstall preserved the running process")
PY

# Local HTTP fixture exercises subscriptions without private data or live URLs.
cat > "$work/fixture.py" <<'PY'
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import sys, time
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        if self.path.startswith("/invalid"):
            self.wfile.write(b"proxies: [broken yaml\n")
        else:
            self.wfile.write(b'mode: rule\nlog-level: info\nproxies: []\nproxy-groups:\n - name: PROXY\n   type: select\n   proxies: [DIRECT, REJECT]\nrules:\n - DOMAIN-SUFFIX,example.invalid,PROXY\n - DOMAIN,needle.example.invalid,REJECT\n - MATCH,PROXY\n')
    def do_POST(self):
        if self.path != "/stream":
            self.send_error(404)
            return
        length = int(self.headers.get("Content-Length", "0"))
        if not 0 < length <= 1024 * 1024 or len(self.rfile.read(length)) != length:
            self.send_error(400)
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Content-Length", str(8192 * 1200))
        self.end_headers()
        try:
            for _ in range(1200):
                self.wfile.write(b"x" * 8192)
                self.wfile.flush()
                time.sleep(0.1)
        except (BrokenPipeError, ConnectionResetError):
            pass
    def log_message(self, *args): pass
server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
Path(sys.argv[1]).write_text(str(server.server_port))
server.serve_forever()
PY
python3 "$work/fixture.py" "$work/fixture-port" &
fixture_pid=$!
for ((attempt=0; attempt<50; attempt++)); do
  [[ -f "$work/fixture-port" ]] && break
  sleep 0.1
done
fixture_port=$(cat "$work/fixture-port")
lp profile add fixture "http://127.0.0.1:$fixture_port/config?token=fixture-private-token"
before=$(readlink "$home/installation/current")
if lp profile add invalid "http://127.0.0.1:$fixture_port/invalid?token=fixture-private-token" > "$work/invalid.log" 2>&1; then
  printf 'Invalid YAML was accepted\n' >&2
  exit 1
fi
[[ ! -e "$home/profiles/invalid.yaml" ]]
[[ $(readlink "$home/installation/current") == "$before" ]]
if grep -Fq fixture-private-token "$work/invalid.log"; then
  printf 'Subscription credential appeared in diagnostics\n' >&2
  exit 1
fi
lp profile apply fixture
lp select PROXY REJECT
lp nodes > "$work/nodes.json"
python3 - "$work/nodes.json" <<'PY'
import json, sys
assert any(g["name"] == "PROXY" and g["now"] == "REJECT" for g in json.load(open(sys.argv[1])))
print("Subscription validate/save/apply and real API node selection passed")
PY
lp select PROXY DIRECT

# Read the core's actual ordered rules, then exercise the read-only page in a
# controlling terminal. The domains are reserved fixtures and are never fetched.
lp rules > "$work/rules.json"
python3 - "$work/rules.json" <<'PY'
import json, sys
rules = json.load(open(sys.argv[1]))
assert [(r["index"], r["type"], r["payload"], r["proxy"]) for r in rules] == [
    (0, "DomainSuffix", "example.invalid", "PROXY"),
    (1, "Domain", "needle.example.invalid", "REJECT"),
    (2, "Match", "", "PROXY"),
], "effective rules lost matching order, original indexes or empty MATCH payload"
assert all(r["size"] == -1 for r in rules), "non-group rule size was changed"
print("Actual ordered rules and MATCH payload verified through CLI")
PY
rules_config_hash=$(python3 - "$home/installation/current/config.yaml" <<'PY'
import hashlib, sys
print(hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest())
PY
)
python3 "$work/pty-smoke.py" --binary "$app" --home "$home" --service-name "$service_name" --mode rules --output "$work/pty-rules.txt"
python3 - "$home/installation/current/config.yaml" "$rules_config_hash" <<'PY'
import hashlib, sys
assert hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest() == sys.argv[2], "read-only rule inspection changed configuration"
print("Rule inspection left configuration bytes unchanged")
PY

# Collect real live logs through the shared CLI use case. The selected fixture
# profile enables info events only for this private installation; the product's
# default log level and service stdout policy are left unchanged.
python3 - "$app" "$home" "$service_name" "$fixture_port" <<'PY'
from http.client import HTTPConnection
import json, subprocess, sys, time
command = [sys.argv[1], "--home", sys.argv[2], "--service-name", sys.argv[3],
           "logs", "--lines", "3", "--duration", "4s"]
process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
try:
    for _ in range(12):
        if process.poll() is not None:
            break
        connection = HTTPConnection("127.0.0.1", 7897, timeout=5)
        try:
            connection.request("GET", "http://127.0.0.1:%s/log-event" % sys.argv[4])
            response = connection.getresponse()
            assert response.status == 200, "dedicated log traffic fixture failed"
            response.read()
        finally:
            connection.close()
        time.sleep(.15)
    output, error = process.communicate(timeout=10)
    assert process.returncode == 0, "live-log CLI observation failed"
    entries = json.loads(output)
    assert 0 < len(entries) <= 3, "bounded live-log observation was empty or exceeded its limit"
    assert any("[TCP]" in entry["message"] for entry in entries), "actual Mihomo traffic event was not captured"
    assert all(entry["time"] and entry["level"] and len(entry["message"].encode()) < 4200 for entry in entries)
    assert b"fixture-private-token" not in output, "subscription credential appeared in logs"
    print("Real live-log CLI collected bounded, timestamped Mihomo events")
finally:
    if process.poll() is None:
        process.kill()
        process.communicate()
PY
python3 "$work/pty-smoke.py" --binary "$app" --home "$home" --service-name "$service_name" --mode logs --fixture-port "$fixture_port" --output "$work/pty-logs.txt"
python3 - "$home/installation/current/config.yaml" "$rules_config_hash" <<'PY'
import hashlib, sys
assert hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest() == sys.argv[2], "read-only log viewing changed configuration"
print("Log inspection left configuration bytes unchanged")
PY

# Generate actual proxy traffic against our own loopback fixture. Two active
# connections prove that both CLI and TUI close only their requested UUID.
lp stats > "$work/stats-before.json"
cat > "$work/traffic.py" <<'PY'
from http.client import HTTPConnection, IncompleteRead
from pathlib import Path
import json, sys, threading, time

ready = [threading.Event(), threading.Event()]
ports = [None, None]
errors = []
def transfer(index):
    client = HTTPConnection("127.0.0.1", 7897, timeout=10)
    try:
        client.connect()
        ports[index] = str(client.sock.getsockname()[1])
        # Explicit absolute-form HTTP through Mihomo; environment proxy and
        # localhost bypass settings cannot silently turn this into a direct test.
        client.request("POST", "http://127.0.0.1:%s/stream" % sys.argv[1], b"u" * (256 * 1024))
        response = client.getresponse()
        if response.status != 200 or not response.read(32768):
            raise RuntimeError("stream fixture did not return proxy data")
        ready[index].set()
        while response.read(8192):
            pass
    except (IncompleteRead, ConnectionResetError, BrokenPipeError):
        if not ready[index].is_set():
            errors.append("connection failed before transferring fixture bytes")
    except Exception:
        errors.append("proxy traffic fixture failed")
    finally:
        client.close()
        ready[index].set()

threads = [threading.Thread(target=transfer, args=(i,), daemon=True) for i in range(2)]
for thread in threads: thread.start()
for event in ready:
    if not event.wait(20):
        raise SystemExit("proxy traffic fixture did not become ready")
if errors or any(port is None for port in ports):
    raise SystemExit("proxy traffic fixture could not establish both connections")
path = Path(sys.argv[2])
temporary = path.with_suffix(".tmp")
temporary.write_text(json.dumps(ports))
temporary.replace(path)
for thread in threads: thread.join(120)
if any(thread.is_alive() for thread in threads) or errors:
    raise SystemExit("proxy traffic fixture did not finish cleanly")
PY
python3 "$work/traffic.py" "$fixture_port" "$work/traffic-ready.json" &
traffic_pid=$!
for ((attempt=0; attempt<200; attempt++)); do
  [[ -f "$work/traffic-ready.json" ]] && break
  kill -0 "$traffic_pid" 2>/dev/null || { printf 'Proxy traffic fixture exited early\n' >&2; exit 1; }
  sleep 0.1
done
[[ -f "$work/traffic-ready.json" ]]
lp connections > "$work/connections-before.json"
lp stats > "$work/stats-live.json"
python3 - "$work/traffic-ready.json" "$work/connections-before.json" "$work/stats-before.json" "$work/stats-live.json" "$work/connection-ids" <<'PY'
import json, sys
ports = json.load(open(sys.argv[1]))
connections = json.load(open(sys.argv[2]))
before = json.load(open(sys.argv[3]))
live = json.load(open(sys.argv[4]))
assert live["State"] == "ready" and live["Memory"] > 0, "runtime sample lacks a running core and real RSS"
assert live["Traffic"]["down"] > 0, "real proxy transfer produced no sampled download rate"
assert live["Connections"]["uploadTotal"] > before["Connections"]["uploadTotal"], "uploaded bytes did not increase the core counter"
assert live["Connections"]["downloadTotal"] > before["Connections"]["downloadTotal"], "downloaded bytes did not increase the core counter"
ids = []
for port in ports:
    matches = [c["id"] for c in connections["connections"] if c["metadata"]["sourcePort"] == port]
    assert len(matches) == 1, "fixture connection was not uniquely visible through the real API"
    ids.append(matches[0])
with open(sys.argv[5], "w") as output:
    output.write("\n".join(ids) + "\n")
print("Real runtime API reports positive traffic rates, cumulative bytes and RSS")
PY
mapfile -t connection_ids < "$work/connection-ids"
lp connections close "${connection_ids[0]}"
lp connections > "$work/connections-after-cli.json"
python3 - "$work/connections-after-cli.json" "${connection_ids[0]}" "${connection_ids[1]}" <<'PY'
import json, sys
ids = {c["id"] for c in json.load(open(sys.argv[1]))["connections"]}
assert sys.argv[2] not in ids and sys.argv[3] in ids, "CLI did not close exactly the selected connection"
print("CLI closed exactly one requested connection")
PY
python3 "$work/pty-smoke.py" --binary "$app" --home "$home" --service-name "$service_name" --mode runtime --connection-id "${connection_ids[1]}" --output "$work/pty-runtime.txt"
wait "$traffic_pid"
traffic_pid=
lp stats > "$work/stats-restarted.json"
python3 - "$work/stats-restarted.json" <<'PY'
import json, sys
sample = json.load(open(sys.argv[1]))
assert sample["State"] == "ready" and sample["Memory"] > 0
assert sample["Connections"]["uploadTotal"] == 0 and sample["Connections"]["downloadTotal"] == 0, "core restart did not reset its counters"
print("Core restart resets in-memory cumulative counters and monitoring reconnects")
PY
lp service restart
lp service stop
lp service status
lp profile apply default
lp service start
lp rules > "$work/default-rules.json"
python3 - "$work/default-rules.json" <<'PY'
import json, sys
rules = json.load(open(sys.argv[1]))
assert len(rules) == 1 and rules[0]["index"] == 0 and rules[0]["type"] == "Match" and rules[0]["payload"] == "" and rules[0]["proxy"] == "PROXY", "default configuration did not expose its actual MATCH rule"
print("Built-in configuration exposes rules without a subscription")
PY
lp doctor
lp service stop
printf 'Real Mihomo + systemd + PTY end-to-end scenarios passed\n'
