#!/usr/bin/env sh
set -eu

if [ "$(uname -s)" != "Linux" ]; then
  echo "smoke_test_shared_pod_net.sh only supports Linux" >&2
  exit 1
fi

if [ "${EUID:-$(id -u)}" -ne 0 ]; then
  echo "smoke_test_shared_pod_net.sh must run as root" >&2
  exit 1
fi

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$ROOT_DIR"

POD_FILE=${1:-./covelet/examples/localhost-pod.yaml}
POD_NAME=localhost-pod
GO_BIN=${GO_BIN:-}
IMAGE_NAME=${IMAGE_NAME:-busybox}
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

cleanup() {
  set +e
  ./covelet-bin delete pod "$POD_NAME" >/dev/null 2>&1
}

trap cleanup EXIT INT TERM

echo "building covet..."
"$GO_BIN" build -o covet-bin ./covet/cmd/covet

echo "building covelet..."
"$GO_BIN" build -o covelet-bin ./covelet/cmd/covelet

if [ ! -f "$RUNTIME_DIR/.covet/images/$IMAGE_NAME.tar" ] && [ ! -f "$RUNTIME_DIR/.covet/images/$IMAGE_NAME/layer.tar" ]; then
  if [ -z "$ROOTFS_PATH" ]; then
    echo "image \"$IMAGE_NAME\" not found under $RUNTIME_DIR/.covet/images; set ROOTFS_PATH=/path/to/rootfs so the smoke script can pack it first" >&2
    exit 1
  fi
  echo "packing image $IMAGE_NAME from $ROOTFS_PATH ..."
  (cd "$RUNTIME_DIR" && ../covet-bin pack "$ROOTFS_PATH" "$IMAGE_NAME")
fi

echo "running pod from $POD_FILE ..."
./covelet-bin run -f "$POD_FILE"

echo "getting pod status..."
./covelet-bin get pod "$POD_NAME"

web_id=$(sed -n 's/.*"container_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' .covetrol/pods/"$POD_NAME"/containers.json | sed -n '2p')
debug_id=$(sed -n 's/.*"container_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' .covetrol/pods/"$POD_NAME"/containers.json | sed -n '3p')

if [ -z "$web_id" ] || [ -z "$debug_id" ]; then
  echo "failed to resolve business container ids for pod $POD_NAME" >&2
  exit 1
fi

echo "web container: $web_id"
echo "debug container: $debug_id"

sleep 1

echo "probing localhost from debug container..."
output=$(cd "$RUNTIME_DIR" && ../covet-bin exec "$debug_id" /bin/busybox wget -qO- http://127.0.0.1:8080/index.html)
printf '%s\n' "$output"

echo "shared pod network smoke test passed"
