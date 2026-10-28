#!/usr/bin/env bash
# Run the actual Go test binaries in an explicitly selected OrbStack Linux VM.
set -euo pipefail
if [[ $# != 1 ]]; then
  printf 'Usage: %s ORBSTACK_VM\n' "$0" >&2
  exit 2
fi
vm=$1
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
command -v orb >/dev/null
orb -m "$vm" sh -c 'test "$(uname -s)" = Linux; test "$(id -u)" != 0; command -v python3 >/dev/null'
machine_arch=$(orb -m "$vm" uname -m)
case "$machine_arch" in
  aarch64|arm64) goarch=arm64 ;;
  x86_64) goarch=amd64 ;;
  *) printf 'Unsupported VM architecture: %s\n' "$machine_arch" >&2; exit 1 ;;
esac
host_stage=$(mktemp -d "${TMPDIR:-/tmp}/lazyclash-linux.XXXXXX")
linux_stage=
mkdir -p .test-artifacts
exec > >(tee .test-artifacts/linux-tests.log) 2>&1
cleanup() {
  result=$?
  trap - EXIT
  if [[ -n $linux_stage ]]; then
    orb -m "$vm" rm -rf -- "$linux_stage" || result=1
  fi
  rm -rf -- "$host_stage"
  exit "$result"
}
trap cleanup EXIT
linux_stage=$(orb -m "$vm" mktemp -d /tmp/lazyclash-linux.XXXXXX)
index=0
go list ./... > "$host_stage/packages"
while IFS= read -r package <&3; do
  index=$((index + 1))
  testbin="$host_stage/$index.test"
  printf 'Build Linux/%s tests: %s\n' "$goarch" "$package"
  CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go test -c -o "$testbin" "$package"
  if [[ -f "$testbin" ]]; then
    orb -m "$vm" sh -c 'umask 077; cat > "$1"; chmod 700 "$1"' sh "$linux_stage/$index.test" < "$testbin"
    printf 'Run in Linux VM: %s\n' "$package"
    orb -m "$vm" "$linux_stage/$index.test" -test.v -test.timeout=3m </dev/null
  fi
done 3< "$host_stage/packages"
printf 'Actual Linux test execution passed (%s, %s).\n' "$vm" "$goarch"
