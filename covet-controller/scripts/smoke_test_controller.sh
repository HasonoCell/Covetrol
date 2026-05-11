#!/usr/bin/env sh
set -eu

if [ "$(uname -s)" != "Linux" ]; then
  echo "smoke_test_controller.sh only supports Linux" >&2
  exit 1
fi

if [ "${EUID:-$(id -u)}" -ne 0 ]; then
  echo "smoke_test_controller.sh must run as root" >&2
  exit 1
fi

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$ROOT_DIR"

GO_BIN=${GO_BIN:-}
ROOTFS_PATH=${ROOTFS_PATH:-}
IMAGE_NAME=${IMAGE_NAME:-busybox}
RUNTIME_DIR=./covet
RS_NAME=busybox-rs
RS_FILE_2=${1:-./covet-controller/examples/busybox-rs.yaml}
RS_FILE_1=${2:-./covet-controller/examples/busybox-rs-1.yaml}

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
  ./covet-controller-bin delete rs "$RS_NAME" >/dev/null 2>&1
}

trap cleanup EXIT INT TERM

echo "building covet..."
"$GO_BIN" build -o covet-bin ./covet/cmd/covet

echo "building covelet..."
"$GO_BIN" build -o covelet-bin ./covelet/cmd/covelet

echo "building covet-controller..."
"$GO_BIN" build -o covet-controller-bin ./covet-controller/cmd/covet-controller

if [ ! -f "$RUNTIME_DIR/.covet/images/$IMAGE_NAME.tar" ] && [ ! -f "$RUNTIME_DIR/.covet/images/$IMAGE_NAME/layer.tar" ]; then
  if [ -z "$ROOTFS_PATH" ]; then
    echo "image \"$IMAGE_NAME\" not found under $RUNTIME_DIR/.covet/images; set ROOTFS_PATH=/path/to/rootfs so the smoke script can pack it first" >&2
    exit 1
  fi
  echo "packing image $IMAGE_NAME from $ROOTFS_PATH ..."
  (cd "$RUNTIME_DIR" && ../covet-bin pack "$ROOTFS_PATH" "$IMAGE_NAME")
fi

echo "applying replicas=2 from $RS_FILE_2 ..."
./covet-controller-bin apply -f "$RS_FILE_2"

echo "getting replica set status..."
./covet-controller-bin get rs "$RS_NAME"

echo "listing pods after scale-up..."
./covelet-bin list pods

echo "re-applying replicas=1 from $RS_FILE_1 ..."
./covet-controller-bin apply -f "$RS_FILE_1"

echo "reconciling replica set..."
./covet-controller-bin reconcile "$RS_NAME"

echo "getting replica set status after scale-down..."
./covet-controller-bin get rs "$RS_NAME"

echo "listing pods after scale-down..."
./covelet-bin list pods

echo "deleting replica set..."
./covet-controller-bin delete rs "$RS_NAME"

echo "listing pods after delete..."
./covelet-bin list pods

echo "controller smoke test passed"
