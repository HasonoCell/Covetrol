# covet

`covet` 是一个从零开始实现容器运行时的学习型项目。

## 构建

```bash
go build ./cmd/covet
```

## 准备最小 busybox rootfs

在 Linux 上，可以使用辅助脚本准备一个最小 rootfs：

```bash
./scripts/prepare_busybox_rootfs.sh /tmp/covet-rootfs
```

这个脚本会做以下事情：

- 将 `busybox` 复制到 `bin/busybox`
- 创建 `/bin/sh`，并让它链接到同目录下的 `busybox`
- 创建当前运行时需要的最小目录结构（/etc, /dev, /sys 等文件夹）
- 如果 `busybox` 是动态链接程序，则复制 `ldd` 输出的动态加载器和共享库
- 创建最小的 `/etc/passwd` 和 `/etc/group`

前置条件：

- Linux
- `busybox` 已在 `PATH` 中
- `ldd` 已在 `PATH` 中

如果你使用 Ubuntu，推荐安装静态版本：

```bash
sudo apt update
sudo apt install -y busybox-static
```

## 制作本地镜像

先把准备好的 rootfs 打包进本地镜像仓库：

```bash
./covet pack /tmp/covet-rootfs busybox-base
```

## 运行

在 Linux 上以 root 身份运行：

```bash
sudo ./covet run busybox-base /bin/sh
sudo ./covet run --mem 256m --cpu-weight 100 busybox-base /bin/sh
sudo ./covet run -v /tmp/data:/data:ro busybox-base /bin/sh
sudo ./covet run -v mydata:/data busybox-base /bin/sh
```

说明：

- `-v /host:/container[:ro]` 是 bind mount
- `-v mydata:/container[:ro]` 是 named volume，数据保存在 `.covet/volumes/mydata`

## Volume 管理

```bash
./covet volumes
./covet volume inspect mydata
./covet volume rm mydata
```
