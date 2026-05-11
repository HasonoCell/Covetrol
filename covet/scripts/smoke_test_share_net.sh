#!/usr/bin/env sh
set -eu

if [ "$(uname -s)" != "Linux" ]; then
  echo "smoke_test_share_net.sh only supports Linux" >&2
  exit 1
fi

if [ "${EUID:-$(id -u)}" -ne 0 ]; then
  echo "smoke_test_share_net.sh must run as root" >&2
  exit 1
fi

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$ROOT_DIR"

IMAGE_NAME=${1:-busybox}
GO_BIN=${GO_BIN:-}
ROOTFS_PATH=${ROOTFS_PATH:-}
RUNTIME_DIR=./covet

if [ -z "$GO_BIN" ]; then
  if command -v go >/dev/null 2>&1; then
    GO_BIN=$(command -v go)
  else
    for candidate in /usr/local/go/bin/go /usr/bin/go /bin/go; do
      if [ -x "$candidate" ]; then
        GO_BIN=$candidate
        break
      fi
    done
  fi
fi

if [ -z "$GO_BIN" ] || [ ! -x "$GO_BIN" ]; then
  echo "go binary not found; set GO_BIN=/path/to/go or add go to root PATH" >&2
  exit 1
fi

infra_id=""
app_id=""

cleanup() {
  set +e
  if [ -n "$app_id" ]; then
    (cd "$RUNTIME_DIR" && ../covet-bin stop "$app_id" >/dev/null 2>&1)
    (cd "$RUNTIME_DIR" && ../covet-bin rm "$app_id" >/dev/null 2>&1)
  fi
  if [ -n "$infra_id" ]; then
    (cd "$RUNTIME_DIR" && ../covet-bin stop "$infra_id" >/dev/null 2>&1)
    (cd "$RUNTIME_DIR" && ../covet-bin rm "$infra_id" >/dev/null 2>&1)
  fi
}

trap cleanup EXIT INT TERM

extract_ip() {
  container_id=$1
  metadata_path="$RUNTIME_DIR/.covet/containers/$container_id/metadata.json"
  sed -n 's/.*"ip_address"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$metadata_path"
}

echo "building covet..."
"$GO_BIN" build -o covet-bin ./covet/cmd/covet

if [ ! -f "$RUNTIME_DIR/.covet/images/$IMAGE_NAME.tar" ] && [ ! -f "$RUNTIME_DIR/.covet/images/$IMAGE_NAME/layer.tar" ]; then
  if [ -z "$ROOTFS_PATH" ]; then
    echo "image \"$IMAGE_NAME\" not found under $RUNTIME_DIR/.covet/images; set ROOTFS_PATH=/path/to/rootfs so the smoke script can pack it first" >&2
    exit 1
  fi
  echo "packing image $IMAGE_NAME from $ROOTFS_PATH ..."
  (cd "$RUNTIME_DIR" && ../covet-bin pack "$ROOTFS_PATH" "$IMAGE_NAME")
fi

echo "starting infra container..."
infra_id=$(cd "$RUNTIME_DIR" && ../covet-bin run -d "$IMAGE_NAME" /bin/busybox sleep 600)
echo "infra container: $infra_id"

echo "starting app container in shared netns..."
app_id=$(cd "$RUNTIME_DIR" && ../covet-bin run -d --share-net-with "$infra_id" "$IMAGE_NAME" /bin/busybox sleep 600)
echo "app container: $app_id"

infra_ip=$(extract_ip "$infra_id")
app_ip=$(extract_ip "$app_id")

echo "infra ip: $infra_ip"
echo "app ip: $app_ip"

if [ -z "$infra_ip" ] || [ -z "$app_ip" ]; then
  echo "failed to resolve container IPs from metadata" >&2
  exit 1
fi

if [ "$infra_ip" != "$app_ip" ]; then
  echo "shared netns validation failed: infra ip ($infra_ip) != app ip ($app_ip)" >&2
  exit 1
fi

echo "covet ps:"
(cd "$RUNTIME_DIR" && ../covet-bin ps)

echo "share-net-with smoke test passed"
