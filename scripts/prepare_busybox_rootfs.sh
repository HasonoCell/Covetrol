#!/usr/bin/env sh
set -eu

if [ "$(uname -s)" != "Linux" ]; then
  echo "prepare_busybox_rootfs.sh only supports Linux" >&2
  exit 1
fi

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <rootfs-dir>" >&2
  exit 1
fi

if ! command -v busybox >/dev/null 2>&1; then
  echo "busybox not found in PATH" >&2
  exit 1
fi

if ! command -v ldd >/dev/null 2>&1; then
  echo "ldd not found in PATH" >&2
  exit 1
fi

ROOTFS=$1
BUSYBOX=$(command -v busybox)

mkdir -p "$ROOTFS"/bin "$ROOTFS"/dev "$ROOTFS"/etc "$ROOTFS"/proc "$ROOTFS"/sys "$ROOTFS"/tmp "$ROOTFS"/usr/bin
cp "$BUSYBOX" "$ROOTFS"/bin/busybox
ln -sf busybox "$ROOTFS"/bin/sh

# Copy the dynamic loader and shared libraries when busybox is dynamically linked.
if ldd "$BUSYBOX" 2>/dev/null | grep -q 'not a dynamic executable'; then
  :
else
  ldd "$BUSYBOX" | while IFS= read -r line; do
    set -- $line
    lib_path=""

    case "$line" in
      *'=> '* )
        if [ "$3" != "not" ]; then
          lib_path=$3
        fi
        ;;
      /* )
        lib_path=$1
        ;;
    esac

    if [ -n "$lib_path" ] && [ -f "$lib_path" ]; then
      target_dir="$ROOTFS$(dirname "$lib_path")"
      mkdir -p "$target_dir"
      cp "$lib_path" "$target_dir/"
    fi
  done
fi

cat > "$ROOTFS"/etc/passwd <<'PASSWD'
root:x:0:0:root:/root:/bin/sh
PASSWD

cat > "$ROOTFS"/etc/group <<'GROUP'
root:x:0:
GROUP

chmod 1777 "$ROOTFS"/tmp

echo "busybox rootfs prepared at: $ROOTFS"
echo "try: ./covet commit $ROOTFS busybox-base"
echo "or:  sudo ./covet run busybox-base /bin/sh"
