package container

import "covet/internal/cgroups"

// RunRequest 只表示用户显式请求的运行参数
type RunRequest struct {
	Command   []string
	Image     string
	Detach    bool
	Resources cgroups.ResourceConfig
}

// RuntimeContext 表示父进程在真正启动容器前准备出的运行时上下文
type RuntimeContext struct {
	Request      RunRequest
	ContainerID  string
	MergedRootFS string
}
