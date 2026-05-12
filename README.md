# covetrol

covetrol 是一个容器编排引擎，从一个自研的轻量容器运行时逐步往上构建出可用的节点代理、控制循环和服务转发组件。covet 是核心的容器引擎，covelet 作为一个 node 上的 pod 管理工具。covet-controller 提供基本的 replicas reconvile loop，covet-proxy 提供基本的 pod 网络能力。
