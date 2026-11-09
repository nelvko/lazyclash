
#!/usr/bin/env bash

# Real proxy traffic generator
# macOS / Linux
# Requires: bash, curl, dd, mktemp

set -u

PROXY="${PROXY:-http://127.0.0.1:7899}"
BASE_URL="https://speed.cloudflare.com"

CYCLES="${CYCLES:-4}"
IDLE_SECONDS=3

umask 077
TMP_DIR=$(mktemp -d) || exit 1

DL_PID=""
UL_PID=""

cleanup() {
  [[ -n "$DL_PID" ]] && kill "$DL_PID" 2>/dev/null || true
  [[ -n "$UL_PID" ]] && kill "$UL_PID" 2>/dev/null || true

  [[ -n "$DL_PID" ]] && wait "$DL_PID" 2>/dev/null || true
  [[ -n "$UL_PID" ]] && wait "$UL_PID" 2>/dev/null || true

  rm -rf -- "$TMP_DIR"
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Generate temporary zero-filled payloads.
# 512 KiB, 2 MiB, 4 MiB
dd if=/dev/zero of="$TMP_DIR/low.bin" \
  bs=1024 count=512 2>/dev/null

dd if=/dev/zero of="$TMP_DIR/medium.bin" \
  bs=1024 count=2048 2>/dev/null

dd if=/dev/zero of="$TMP_DIR/high.bin" \
  bs=1024 count=4096 2>/dev/null

download() {
  local bytes="$1"
  local rate="$2"

  curl -fsS \
    --proxy "$PROXY" \
    --noproxy "" \
    --connect-timeout 5 \
    --max-time 15 \
    --limit-rate "$rate" \
    -o /dev/null \
    "$BASE_URL/__down?bytes=$bytes"
}

upload() {
  local file="$1"
  local rate="$2"

  curl -fsS \
    --proxy "$PROXY" \
    --noproxy "" \
    --connect-timeout 5 \
    --max-time 15 \
    --limit-rate "$rate" \
    -H "Content-Type: application/octet-stream" \
    --data-binary "@$file" \
    -o /dev/null \
    "$BASE_URL/__up"
}

stage() {
  local name="$1"
  local bytes="$2"
  local rate="$3"
  local file="$4"

  echo ""
  echo "[$name] Download + Upload | $rate each"

  download "$bytes" "$rate" &
  DL_PID=$!

  upload "$file" "$rate" &
  UL_PID=$!

  local failed=0

  wait "$DL_PID" || {
    echo "Download request failed"
    failed=1
  }

  wait "$UL_PID" || {
    echo "Upload request failed"
    failed=1
  }

  DL_PID=""
  UL_PID=""

  return "$failed"
}

echo "================================="
echo " Proxy Traffic Generator"
echo "================================="
echo "Proxy  : $PROXY"
echo "Cycles : $CYCLES"
echo "Mode   : Download + Upload"
echo "Stop   : Ctrl+C"
echo ""

for ((i=1; i<=CYCLES; i++)); do
  echo "========== Cycle $i/$CYCLES =========="

  stage "LOW" \
    524288 128K "$TMP_DIR/low.bin" || true

  stage "MEDIUM" \
    2097152 512K "$TMP_DIR/medium.bin" || true

  stage "HIGH" \
    4194304 1M "$TMP_DIR/high.bin" || true

  echo ""
  echo "[IDLE] ${IDLE_SECONDS}s"
  sleep "$IDLE_SECONDS"
done

echo ""
echo "Traffic test completed."

