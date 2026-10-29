#!/usr/bin/env bash
# Build on macOS, then open a normal user shell in an OrbStack Linux VM.
set -euo pipefail

usage() {
  printf 'Usage: %s [ORBSTACK_VM]\n' "$0"
  printf 'Without an argument, select the single lazyclash or lazyclash-* VM.\n'
  printf 'Build lazyclash for its architecture, then enter the VM; type lazyclash to run.\n'
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
  systemctl --user show --property=SystemState --value >/dev/null
'
machine_arch=$(orb -m "$vm" uname -m)
case "$machine_arch" in
  aarch64|arm64) goarch=arm64 ;;
  x86_64) goarch=amd64 ;;
  *) printf 'Unsupported VM architecture: %s\n' "$machine_arch" >&2; exit 1 ;;
esac

output_dir="$root/bin/linux-$goarch"
mkdir -p "$output_dir"
stage=$(mktemp "$output_dir/.lazyclash-build.XXXXXX")
trap 'rm -f -- "$stage"' EXIT
printf 'Building lazyclash for Linux/%s…\n' "$goarch"
(cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -o "$stage" .)
chmod 755 "$stage"
mv -f -- "$stage" "$output_dir/lazyclash"
trap - EXIT

printf 'Entering %s. Type lazyclash to start; exit to return to macOS.\n' "$vm"
# Only this shell receives the build directory in PATH. Normal lazyclash data
# and service defaults apply; no shell startup files or services are modified.
exec orb -m "$vm" -p -w "$root" sh -ec '
  PATH="$1:$PATH"
  export PATH
  user_shell=$(getent passwd "$(id -u)" | cut -d: -f7)
  test -n "$user_shell" && test -x "$user_shell"
  exec "$user_shell" -i
' sh "$output_dir"
