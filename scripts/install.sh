#!/bin/sh
# Install the unpacked Linux release for the current user.
set -eu
if [ "$(uname -s)" != Linux ] || [ -n "${SUDO_UID:-}" ]; then
    echo "Run this user installer directly in your Linux shell." >&2
    exit 1
fi
source_dir=$(CDPATH= cd -P -- "$(dirname -- "$0")" && pwd)
dest_dir=${HOME:?}/.local/bin
"$source_dir/lazyclash" --help >/dev/null
mkdir -p -- "$dest_dir"
stage=$(mktemp "$dest_dir/.lazyclash.XXXXXX")
trap 'rm -f -- "$stage"' EXIT
install -m 0755 -- "$source_dir/lazyclash" "$stage"
mv -fT -- "$stage" "$dest_dir/lazyclash"
printf 'Installed %s\n' "$dest_dir/lazyclash"
if [ ! -t 0 ]; then
    echo "No interactive shell detected. The terminal proxy switch can set up the hook on first use."
    exit 0
fi
# Keep the interactive shell as the binary's parent for verified shell detection.
exec "$dest_dir/lazyclash" shell install
