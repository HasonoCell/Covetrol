# covelet

`covelet` 是 `covetrol` 中的最小节点代理实现。当前版本支持读取本地 Pod yaml/json，先启动一个 infra container，再让业务容器通过 `covet --share-net-with` 共享它的 network namespace，并在 `.covetrol/pods/` 下维护本地状态。

当前命令：

- `covelet run -f <pod.yaml>`
- `covelet get pod <name>`
- `covelet list pods`
- `covelet delete pod <name>`

示例：

```bash
go build -o covet-bin ./covet/cmd/covet
go build -o covelet-bin ./covelet/cmd/covelet
sudo ./covelet-bin run -f ./covelet/examples/busybox-pod.yaml
./covelet-bin get pod busybox-pod
./covelet-bin list pods
sudo ./covelet-bin delete pod busybox-pod
```

也可以直接跑 smoke test：

```bash
sudo ROOTFS_PATH=/tmp/covet-rootfs ./covelet/scripts/smoke_test_covelet.sh
```

如果本地已经有 `busybox` 镜像，也可以直接：

```bash
sudo ./covelet/scripts/smoke_test_covelet.sh
```

验证 Pod 内共享 localhost：

```bash
sudo ROOTFS_PATH=/tmp/covet-rootfs ./covelet/scripts/smoke_test_shared_pod_net.sh
```
