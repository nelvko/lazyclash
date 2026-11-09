#!/usr/bin/env bash
# Open an OrbStack Linux shell where lazyclash builds on macOS before each run.
set -euo pipefail

usage() {
  printf 'Usage: %s [ORBSTACK_VM]\n' "$0"
  printf 'Without an argument, select the single lazyclash or lazyclash-* VM.\n'
  printf 'Enter the VM; each lazyclash invocation builds the latest code on macOS.\n'
}
if [[ ${1:-} == --help || ${1:-} == -h ]]; then
  usage
  exit 0
fi
if [[ $# -gt 1 ]]; then
  usage >&2
  exit 2
fi
if [[ ! -t 0 || ! -t 1 ]]; then
  printf 'Run this script in an interactive terminal.\n' >&2
  exit 1
fi
command -v orb >/dev/null || { printf 'OrbStack CLI is required.\n' >&2; exit 1; }
command -v go >/dev/null || { printf 'Go is required on macOS.\n' >&2; exit 1; }

root=$(cd "$(dirname "$0")/.." && pwd)
vm=${1:-${LAZYCLASH_VM:-}}
machines=$(orb list --quiet)
if [[ -z $vm ]]; then
  matches=0
  while IFS= read -r candidate; do
    case "$candidate" in
      lazyclash|lazyclash-*) vm=$candidate; matches=$((matches + 1)) ;;
    esac
  done <<< "$machines"
  if [[ $matches != 1 ]]; then
    printf 'Expected one dedicated lazyclash VM; choose one with %s VM_NAME.\n' "$0" >&2
    orb list >&2
    exit 1
  fi
fi
found=no
while IFS= read -r candidate; do
  [[ $candidate != "$vm" ]] || found=yes
done <<< "$machines"
if [[ $found != yes ]]; then
  printf 'OrbStack VM does not exist: %s\n' "$vm" >&2
  exit 1
fi

orb -m "$vm" sh -ec '
  test "$(uname -s)" = Linux
  if test "$(id -u)" = 0; then
    printf "The VM default user must be a normal user, not root.\n" >&2
    exit 1
  fi
  command -v getent >/dev/null
  command -v mac >/dev/null
  systemctl --user show --property=SystemState --value >/dev/null
'
machine_arch=$(orb -m "$vm" uname -m)
case "$machine_arch" in
  aarch64|arm64) goarch=arm64 ;;
  x86_64) goarch=amd64 ;;
  *) printf 'Unsupported VM architecture: %s\n' "$machine_arch" >&2; exit 1 ;;
esac

output_dir="$root/bin/linux-$goarch"
launcher_dir="$output_dir/dev"
mkdir -p "$launcher_dir"
cat > "$launcher_dir/lazyclash" <<'LAUNCHER'
#!/bin/sh
set -eu

output_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
root=$(CDPATH= cd -- "$output_dir/../.." && pwd)
goarch=${output_dir##*/linux-}
mac bash -s -- "$root" "$goarch" <<'BUILD'
set -euo pipefail
root=$1
goarch=$2
output_dir="$root/bin/linux-$goarch"
stage=$(mktemp "$output_dir/.lazyclash-build.XXXXXX")
trap 'rm -f -- "$stage"' EXIT
printf 'Building lazyclash for Linux/%s…\n' "$goarch" >&2
(cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -o "$stage" .)
chmod 755 "$stage"
mv -f -- "$stage" "$output_dir/lazyclash"
BUILD

exec "$output_dir/lazyclash" "$@"
LAUNCHER
chmod 755 "$launcher_dir/lazyclash"

printf 'Entering %s. Type lazyclash to build and run the latest code; exit to return to macOS.\n' "$vm"
# Only this shell receives the launcher directory in PATH. Normal lazyclash data
# and service defaults apply; no shell startup files or services are modified.
exec orb -m "$vm" -p -w "$root" sh -ec '
  PATH="$1:$PATH"
  export PATH
  user_shell=$(getent passwd "$(id -u)" | cut -d: -f7)
  test -n "$user_shell" && test -x "$user_shell"
  exec "$user_shell" -i
' sh "$launcher_dir"
