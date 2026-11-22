#!/usr/bin/env bash
# Generate simultaneous download and upload traffic through a proxy (curl 7.71+).
# Example: PROXY=http://127.0.0.1:7899 CYCLES=1 ./test.sh
set -eu

# Honor the HTTPS/SOCKS settings exported by lazyclash env.
PROXY=${PROXY:-${https_proxy:-${HTTPS_PROXY:-${all_proxy:-${ALL_PROXY:-http://127.0.0.1:7897}}}}}
BASE_URL=${BASE_URL:-https://speed.cloudflare.com}
CYCLES=${CYCLES:-4}

if ! [[ $CYCLES =~ ^[1-9][0-9]*$ ]]; then
  echo 'CYCLES must be a positive integer.' >&2
  exit 2
fi

umask 077
TMP_DIR=$(mktemp -d)
DL_PID=''
UL_PID=''

cleanup() {
  local pid
  for pid in "$DL_PID" "$UL_PID"; do
    if [[ -n $pid ]]; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  rm -rf -- "$TMP_DIR"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Retrying the POST is safe: the speed-test endpoint only consumes dummy bytes.
curl_args=(
  --disable --fail --silent --show-error
  --proxy "$PROXY" --noproxy ''
  --connect-timeout 15 --max-time 60
  --retry 1 --retry-all-errors --retry-delay 1
  --output /dev/null
)

report() {
  local direction=$1 pid=$2 expected=$3
  local status=0 http actual seconds
  wait "$pid" || status=$?
  read -r http actual seconds < "$TMP_DIR/$direction.stats" || true
  if [[ $status == 0 && $http == 2[0-9][0-9] && $actual == "$expected" ]]; then
    printf '  %s OK (HTTP %s, %s bytes, %ss)\n' "$direction" "$http" "$actual" "$seconds"
  else
    printf '  %s FAILED (curl %s, HTTP %s, %s/%s bytes)\n' \
      "$direction" "$status" "${http:-000}" "${actual:-0}" "$expected" >&2
    cat "$TMP_DIR/$direction.err" >&2
    return 1
  fi
}

stage() {
  local name=$1 bytes=$2 rate=$3
  printf '\n[%s] Download + Upload | %s/s each\n' "$name" "$rate"
  head -c "$bytes" /dev/zero > "$TMP_DIR/upload.bin"

  curl "${curl_args[@]}" --limit-rate "$rate" \
    --write-out '%{http_code} %{size_download} %{time_total}\n' \
    "${BASE_URL%/}/__down?bytes=$bytes" > "$TMP_DIR/Download.stats" 2> "$TMP_DIR/Download.err" &
  DL_PID=$!
  curl "${curl_args[@]}" --limit-rate "$rate" \
    --header 'Content-Type: application/octet-stream' --data-binary "@$TMP_DIR/upload.bin" \
    --write-out '%{http_code} %{size_upload} %{time_total}\n' \
    "${BASE_URL%/}/__up" > "$TMP_DIR/Upload.stats" 2> "$TMP_DIR/Upload.err" &
  UL_PID=$!

  report Download "$DL_PID" "$bytes" || failures=$((failures + 1))
  DL_PID=''
  report Upload "$UL_PID" "$bytes" || failures=$((failures + 1))
  UL_PID=''
}

proxy_host=${PROXY#*://}
proxy_host=${proxy_host%%/*}
printf 'Proxy: %s\nCycles: %s (Ctrl+C to stop)\n' "${proxy_host##*@}" "$CYCLES"
failures=0
for ((i=1; i<=CYCLES; i++)); do
  printf '\n========== Cycle %s/%s ==========\n' "$i" "$CYCLES"
  stage LOW     524288 128K
  stage MEDIUM 2097152 512K
  stage HIGH   4194304 1M
  if (( i < CYCLES )); then
    printf '\n[IDLE] 3s\n'
    sleep 3
  fi
done

if (( failures )); then
  printf '\nTraffic test failed: %d transfer(s) failed.\n' "$failures" >&2
  exit 1
fi
printf '\nTraffic test completed successfully.\n'
