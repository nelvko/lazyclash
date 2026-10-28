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
    for file in pty-empty.txt pty-install-local.txt pty-installed.txt cleanup.txt; do
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
  CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -o "$host_stage/lazyclash" ./cmd/lazyclash
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
owned=no
local_owned=no
local_home=
local_unit=
cleanup_linux() {
  result=$?
  trap - EXIT
  cleanup_failed=0
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
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
import sys
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        if self.path.startswith("/invalid"):
            self.wfile.write(b"proxies: [broken yaml\n")
        else:
            self.wfile.write(b'mode: rule\nproxies: []\nproxy-groups:\n - name: PROXY\n   type: select\n   proxies: [DIRECT, REJECT]\nrules: ["MATCH,PROXY"]\n')
    def log_message(self, *args): pass
server = HTTPServer(("127.0.0.1", 0), Handler)
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
lp service restart
lp service stop
lp service status
lp profile apply default
lp service start
lp doctor
lp service stop
printf 'Real Mihomo + systemd + PTY end-to-end scenarios passed\n'
