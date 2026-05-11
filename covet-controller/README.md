# covet-controller

`covet-controller` 是 `covetrol` 中的最小控制循环组件。当前版本支持通过本地 `ReplicaSet` yaml 驱动 `covelet` 创建、缩容和删除 Pod。

当前命令：

- `covet-controller apply -f <replicaset.yaml>`
- `covet-controller get rs <name>`
- `covet-controller reconcile <name>`
- `covet-controller delete rs <name>`

也可以直接跑 smoke test：

```bash
sudo ROOTFS_PATH=/tmp/covet-rootfs ./covet-controller/scripts/smoke_test_controller.sh
```
