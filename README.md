# covet

`covet` 是一个从零开始实现的学习型容器运行时项目，部分参考了《自己动手写 Docker 这本书》，提供了一个最小但完整的本地容器体验：可以基于本地镜像启动容器、限制资源、管理生命周期、挂载数据卷，并为容器提供独立网络和容器间通信能力。这个项目挺有意思的，了解了一些容器的底层实现知识～

## 环境要求

Linux，cgroup v2，以及一个可用的 `busybox` rootfs 或者你自己的 rootfs～

## 构建

```bash
go build -o covet ./cmd/covet
```

## 准备 rootfs

提供了一个辅助脚本来帮助准备 rootfs：

```bash
./scripts/prepare_busybox_rootfs.sh /tmp/covet-rootfs
```

这个脚本会：复制 busybox，创建 /bin/sh，准备最小目录结构，以及在需要时复制动态链接依赖。

Ubuntu 上建议安装：

```bash
sudo apt update
sudo apt install -y busybox-static
```

## 命令参考

- 打包本地镜像：`./covet pack /tmp/covet-rootfs busybox`
- 查看镜像列表：`./covet images`
- 查看镜像详情：`./covet image inspect busybox`
- 解包镜像：`./covet unpack busybox /tmp/unpacked-rootfs`
- 启动前台容器：`sudo ./covet run busybox`
- 启动后台容器：`sudo ./covet run -d busybox /bin/busybox sleep 600`
- 指定资源限制：`sudo ./covet run --mem 256m --cpu-weight 100 busybox /bin/sh`
- 查看容器列表：`./covet ps`
- 查看容器日志：`./covet logs <container-id>`
- 在容器内执行命令：`sudo ./covet exec <container-id> /bin/sh`
- 停止容器：`sudo ./covet stop <container-id>`
- 启动已存在容器：`sudo ./covet start <container-id>`
- 删除容器：`sudo ./covet rm <container-id>`
- bind mount：`sudo ./covet run -v /tmp/data:/data busybox /bin/sh`
- named volume：`sudo ./covet run -v mydata:/data busybox /bin/sh`
- 查看 volume 列表：`./covet volumes`
- 查看 volume 详情：`./covet volume inspect mydata`
- 删除 volume：`./covet volume rm mydata`
- 网络 smoke test：`sudo ./scripts/smoke_test_network.sh ./covet busybox`
