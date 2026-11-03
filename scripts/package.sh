#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
version=${1:-dev}
[[ $version =~ ^[a-zA-Z0-9._-]+$ ]] || { printf 'Invalid package version\n' >&2; exit 2; }
mkdir -p bin/packages
stage=$(mktemp -d "${TMPDIR:-/tmp}/lazyclash-package.XXXXXX")
trap 'rm -rf -- "$stage"' EXIT
for arch in amd64 arm64; do
  name="lazyclash_${version}_linux_${arch}"
  mkdir "$stage/$name"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOAMD64=v1 GOARM64=v8.0 go build -buildvcs=false -trimpath -o "$stage/$name/lazyclash" .
  cp README.md "$stage/$name/"
  COPYFILE_DISABLE=1 tar -czf "bin/packages/$name.tar.gz" -C "$stage/$name" lazyclash README.md
done
python3 - "$version" <<'PY'
import hashlib
import pathlib
import sys
root = pathlib.Path('bin/packages')
paths = sorted(root.glob(f'lazyclash_{sys.argv[1]}_linux_*.tar.gz'))
(root / f'lazyclash_{sys.argv[1]}_SHA256SUMS').write_text(''.join(f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n' for p in paths))
print(f'Packaged {len(paths)} Linux archives with SHA256SUMS in {root}')
PY
