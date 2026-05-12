# covet-proxy

`covet-proxy` 是 `covetrol` 中的最小 Service / 转发组件。当前版本支持通过 selector 选择 Running Pod，并在宿主机本地监听一个端口，将流量 round-robin 转发到后端 Pod。

当前命令：

- `covet-proxy apply -f <service.yaml>`
- `covet-proxy get service <name>`
- `covet-proxy delete service <name>`
- `covet-proxy serve <name>`

也可以直接跑 smoke test：

```bash
sudo ROOTFS_PATH=/tmp/covet-rootfs ./covet-proxy/scripts/smoke_test_proxy.sh
```
