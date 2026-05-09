# covetrol

`covetrol` 是一个学习性质的手写容器编排项目，目标是从一个自研的轻量容器运行时逐步往上构建出最小可用的节点代理、控制循环和网络组件。

当前仓库的组件规划如下：

- `covet/`
  当前已经完成的容器运行时，负责镜像、rootfs、容器生命周期、volume、bridge 网络等能力
- `covelet/`
  计划中的节点代理，负责把 Pod 规格翻译成对 `covet` 的调用
- `covet-controller/`
  计划中的最小控制循环组件
- `covet-cni/`
  计划中的网络组件

当前最完整的组件是 `covet`。它的使用方式、命令参考和 smoke test 请直接查看：

- [covet/README.md](./covet/README.md)

当前建议的开发顺序：

1. 保持 `covet` 稳定，作为底层容器运行时
2. 先实现 `covelet` 的最小 Pod 运行闭环
3. 再补 `covet-controller`
4. 最后再把网络进一步抽象到 `covet-cni`
