#!/usr/bin/env bash
# Exercise a packaged Linux executable in an existing dedicated OrbStack VM.
set -euo pipefail
if [[ $# -lt 3 || $# -gt 4 ]]; then
  printf 'Usage: %s ORBSTACK_VM PACKAGED_LAZYCLASH VM_MIHOMO_EXECUTABLE [--proxy-only|--ui-tun-only]\n' "$0" >&2
  exit 2
fi
vm=$1
artifact=$2
mihomo=$3
[[ $mihomo == /* ]] || { printf 'Expected an absolute VM Mihomo path\n' >&2; exit 2; }
shift 3
scenario_flag=
for argument in "$@"; do
  if [[ $argument == --proxy-only || $argument == --ui-tun-only ]]; then
    scenario_flag=$argument
  else
    printf 'Expected an absolute VM Mihomo path or a scope scenario flag\n' >&2
    exit 2
  fi
done
root=$(cd "$(dirname "$0")/.." && pwd)
[[ -f "$artifact" && ! -L "$artifact" ]]
work=$(orb -m "$vm" mktemp -d /tmp/lazyclash-scopes.XXXXXX)
mkdir -p "$root/.test-artifacts"
log_file="$root/.test-artifacts/service-scopes-e2e.log"
if [[ $scenario_flag == --proxy-only ]]; then
  log_file="$root/.test-artifacts/service-scopes-proxy-e2e.log"
elif [[ $scenario_flag == --ui-tun-only ]]; then
  log_file="$root/.test-artifacts/service-scopes-ui-e2e.log"
fi
cleanup_host() {
  result=$?
  trap - EXIT
  for dimensions in 120x36 80x24; do
    if orb -m "$vm" test -f "$work/pty-root-tun-$dimensions.txt"; then
      orb -m "$vm" cat "$work/pty-root-tun-$dimensions.txt" > "$root/.test-artifacts/pty-root-tun-$dimensions.txt" || result=1
    fi
  done
  if orb -m "$vm" test -f "$work/cleanup-complete" || ! orb -m "$vm" test -f "$work/resources-started"; then
    orb -m "$vm" rm -rf -- "$work" || result=1
  else
    printf 'Scope test resources need inspection in VM %s: %s\n' "$vm" "$work" >&2
    result=1
  fi
  exit "$result"
}
trap cleanup_host EXIT
COPYFILE_DISABLE=1 tar --no-xattrs -C "$(dirname "$artifact")" -cf - "$(basename "$artifact")" | orb -m "$vm" tar -xf - -C "$work"
if [[ $(basename "$artifact") != lazyclash ]]; then
  orb -m "$vm" mv "$work/$(basename "$artifact")" "$work/lazyclash"
fi
COPYFILE_DISABLE=1 tar --no-xattrs -C "$root/scripts" -cf - service-scopes-e2e.py pty-smoke.py | orb -m "$vm" tar -xf - -C "$work"
# Copy only the explicitly selected executable, never profiles or credentials.
orb -m "$vm" sudo -n cp -- "$mihomo" "$work/mihomo"
orb -m "$vm" sudo -n chmod 0755 "$work/mihomo"
if [[ -n $scenario_flag ]]; then
  set -- "$scenario_flag"
else
  set --
fi
if orb -m "$vm" python3 "$work/service-scopes-e2e.py" --work "$work" "$@" > "$log_file" 2>&1; then
  cat "$log_file"
else
  result=$?
  cat "$log_file" >&2
  exit "$result"
fi
