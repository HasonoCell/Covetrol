#!/usr/bin/env sh
set -eu

if [ "$(uname -s)" != "Linux" ]; then
  echo "smoke_test_network.sh only supports Linux" >&2
  exit 1
fi

if [ "${EUID:-$(id -u)}" -ne 0 ]; then
  echo "smoke_test_network.sh must run as root" >&2
  exit 1
fi

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
  echo "usage: $0 <covet-bin> <image-name> [ping-count]" >&2
  exit 1
fi

COVET_BIN=$1
IMAGE_NAME=$2
PING_COUNT=${3:-3}

if [ ! -x "$COVET_BIN" ]; then
  echo "covet binary not found or not executable: $COVET_BIN" >&2
  exit 1
fi

cid1=""
cid2=""

cleanup() {
  set +e
  if [ -n "$cid1" ]; then
    "$COVET_BIN" stop "$cid1" >/dev/null 2>&1
    "$COVET_BIN" rm "$cid1" >/dev/null 2>&1
  fi
  if [ -n "$cid2" ]; then
    "$COVET_BIN" stop "$cid2" >/dev/null 2>&1
    "$COVET_BIN" rm "$cid2" >/dev/null 2>&1
  fi
}

trap cleanup EXIT INT TERM

extract_ip() {
  container_id=$1
  metadata_path=".covet/containers/$container_id/metadata.json"
  if [ ! -f "$metadata_path" ]; then
    echo "metadata not found for container $container_id: $metadata_path" >&2
    return 1
  fi
  sed -n 's/.*"ip_address"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$metadata_path"
}

echo "starting container 1..."
cid1=$("$COVET_BIN" run -d "$IMAGE_NAME" /bin/busybox sleep 600)
echo "container 1: $cid1"

echo "starting container 2..."
cid2=$("$COVET_BIN" run -d "$IMAGE_NAME" /bin/busybox sleep 600)
echo "container 2: $cid2"

ip1=$(extract_ip "$cid1")
ip2=$(extract_ip "$cid2")

if [ -z "$ip1" ] || [ -z "$ip2" ]; then
  echo "failed to resolve container IPs from metadata" >&2
  exit 1
fi

echo "container 1 ip: $ip1"
echo "container 2 ip: $ip2"

echo "ping $ip2 from $cid1..."
"$COVET_BIN" exec "$cid1" /bin/busybox ping -c "$PING_COUNT" "$ip2"

echo "ping $ip1 from $cid2..."
"$COVET_BIN" exec "$cid2" /bin/busybox ping -c "$PING_COUNT" "$ip1"

echo "network smoke test passed"
