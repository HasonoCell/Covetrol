#!/usr/bin/env sh
set -eu

if [ "$(uname -s)" != "Linux" ]; then
  echo "smoke_test_proxy.sh only supports Linux" >&2
  exit 1
fi

if [ "${EUID:-$(id -u)}" -ne 0 ]; then
  echo "smoke_test_proxy.sh must run as root" >&2
  exit 1
fi

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$ROOT_DIR"

GO_BIN=${GO_BIN:-}
ROOTFS_PATH=${ROOTFS_PATH:-}
IMAGE_NAME=${IMAGE_NAME:-busybox}
RUNTIME_DIR=./covet
RS_FILE=${1:-./covet-controller/examples/echo-rs.yaml}
SERVICE_FILE=${2:-./covet-proxy/examples/echo-service.yaml}
RS_NAME=echo-rs
SERVICE_NAME=echo-svc
PROXY_PID=""

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
  if [ -n "$PROXY_PID" ]; then
    kill "$PROXY_PID" >/dev/null 2>&1
    wait "$PROXY_PID" >/dev/null 2>&1
  fi
  ./covet-proxy-bin delete service "$SERVICE_NAME" >/dev/null 2>&1
  ./covet-controller-bin delete rs "$RS_NAME" >/dev/null 2>&1
}

trap cleanup EXIT INT TERM

echo "building covet..."
"$GO_BIN" build -o covet-bin ./covet/cmd/covet

echo "building covelet..."
"$GO_BIN" build -o covelet-bin ./covelet/cmd/covelet

echo "building covet-controller..."
"$GO_BIN" build -o covet-controller-bin ./covet-controller/cmd/covet-controller

echo "building covet-proxy..."
"$GO_BIN" build -o covet-proxy-bin ./covet-proxy/cmd/covet-proxy

if [ ! -f "$RUNTIME_DIR/.covet/images/$IMAGE_NAME.tar" ] && [ ! -f "$RUNTIME_DIR/.covet/images/$IMAGE_NAME/layer.tar" ]; then
  if [ -z "$ROOTFS_PATH" ]; then
    echo "image \"$IMAGE_NAME\" not found under $RUNTIME_DIR/.covet/images; set ROOTFS_PATH=/path/to/rootfs so the smoke script can pack it first" >&2
    exit 1
  fi
  echo "packing image $IMAGE_NAME from $ROOTFS_PATH ..."
  (cd "$RUNTIME_DIR" && ../covet-bin pack "$ROOTFS_PATH" "$IMAGE_NAME")
fi

echo "applying replica set from $RS_FILE ..."
./covet-controller-bin apply -f "$RS_FILE"

echo "applying service from $SERVICE_FILE ..."
./covet-proxy-bin apply -f "$SERVICE_FILE"

echo "starting proxy..."
./covet-proxy-bin serve "$SERVICE_NAME" >/tmp/covet-proxy.log 2>&1 &
PROXY_PID=$!

sleep 1

if command -v wget >/dev/null 2>&1; then
  output=$(wget -qO- http://127.0.0.1:18080)
else
  output=$(busybox wget -qO- http://127.0.0.1:18080)
fi

printf '%s\n' "$output"

if [ "$output" != "echo-ok" ]; then
  echo "unexpected proxy response: $output" >&2
  exit 1
fi

echo "proxy smoke test passed"
