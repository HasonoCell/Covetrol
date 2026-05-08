//go:build linux

package container

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"covet/internal/cgroups"
	"covet/internal/meta"
	"covet/internal/mount"
	"covet/internal/rootfs"
	"covet/internal/store"
)

// Run 返回一个 Container 结构体，用来后续管理生命周期
func Run(req RunRequest) (meta.Container, error) {
	if os.Getenv(initEnv) == initStage {
		// 走子进程任务
		return meta.Container{}, runContainerInit(req.Command, os.Getenv(rootfsEnv), os.Getenv(mountsEnv))
	}

	// 父进程（容器引擎）任务，先组装上下文
	ctx := RuntimeContext{
		Request:     req,
		ContainerID: newContainerID(),
	}
	containerMeta := newContainerMetadata(ctx)
	return startContainer(ctx, containerMeta)
}

// run 和 start 命令都可以复用此函数
func startContainer(ctx RuntimeContext, containerMeta meta.Container) (meta.Container, error) {
	// 创建该容器对应的 metadata store dir
	if err := os.MkdirAll(store.ContainerDir(ctx.ContainerID), 0o755); err != nil {
		return meta.Container{}, fmt.Errorf("create container dir: %w", err)
	}
	// 准备该容器的 rootfs
	if err := prepareRuntimeRootFS(&ctx); err != nil {
		return meta.Container{}, err
	}

	containerMeta.RootFS = ctx.MergedRootFS
	// 构造创建子进程的命令
	cmd, cleanupIO, err := newChildCommand(ctx)
	if err != nil {
		_ = rootfs.Cleanup(containerMeta)
		return meta.Container{}, err
	}
	defer cleanupIO()

	// Start 方法和 Run 不同的地方在于：父进程创建子进程，但先不 wait
	// 为什么要这么做？这样父进程能拿到 pid，把子进程放进 cgroup、落盘元数据后再等待退出
	if err := cmd.Start(); err != nil {
		_ = rootfs.Cleanup(containerMeta)
		return meta.Container{}, fmt.Errorf("start container process: %w", err)
	}

	containerMeta.PID = cmd.Process.Pid
	// 保存元信息
	if err := store.SaveMetadata(containerMeta); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = rootfs.Cleanup(containerMeta)
		return meta.Container{}, err
	}

	// 应用 cgroups
	cleanupResources, err := applyRuntimeResources(cmd.Process.Pid, ctx.Request.Resources)
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = rootfs.Cleanup(containerMeta)
		return meta.Container{}, err
	}
	defer cleanupResources()

	if ctx.Request.Detach {
		// 后台模式下父进程到这里就可以返回了，真正的容器进程继续在后台运行
		return containerMeta, nil
	}

	waitErr := cmd.Wait()
	containerMeta.Status = meta.StateStopped
	if err := rootfs.Cleanup(containerMeta); err != nil {
		return meta.Container{}, err
	}
	if err := store.SaveMetadata(containerMeta); err != nil {
		return meta.Container{}, err
	}
	if waitErr != nil {
		return meta.Container{}, waitErr
	}

	return containerMeta, nil
}

// 准备容器 rootfs
func prepareRuntimeRootFS(ctx *RuntimeContext) error {
	mergedRootFS, err := rootfs.PrepareOverlay(ctx.ContainerID, ctx.Request.Image, ctx.Request.Mounts)
	if err != nil {
		return err
	}
	absMergedRootFS, err := filepath.Abs(mergedRootFS)
	if err != nil {
		return fmt.Errorf("resolve merged rootfs path %q: %w", mergedRootFS, err)
	}
	ctx.MergedRootFS = absMergedRootFS
	return nil
}

// 准备好创建容器内子进程的命令，返回命令和该进程的 io 清理函数
func newChildCommand(ctx RuntimeContext) (*exec.Cmd, func(), error) {
	// 项目编译为二进制文件后可能会放在诸如 /usr/local/bin 这样的目录当中
	// 所以要先获取获取可执行文件的绝对路径或者说名称
	self, err := os.Executable()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve current executable: %w", err)
	}

	childArgs := []string{"run", ctx.Request.Image}
	childArgs = append(childArgs, ctx.Request.Command...)
	cmd := exec.Command(self, childArgs...)
	if err := configureProcessIO(cmd, ctx); err != nil {
		return nil, nil, err
	}
	// 通过环境变量区分父子进程，子进程再次进入 Run() 时，
	// 发现这个环境变量已经存在，就不再继续创建下一层子进程，而是直接进入初始化逻辑
	cmd.Env = append(os.Environ(), initEnv+"="+initStage, rootfsEnv+"="+ctx.MergedRootFS)
	if len(ctx.Request.Mounts) > 0 {
		// 父进程将请求参数中的 mounts 序列化为 json 存在环境变量中，从而使子进程启动后可读
		mountsJSON, err := json.Marshal(ctx.Request.Mounts)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal bind mounts: %w", err)
		}
		cmd.Env = append(cmd.Env, mountsEnv+"="+string(mountsJSON))
	}
	// 修改 SystemProcessAttributes，用于在创建新系统进程时，指定特定的操作系统属性
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWIPC |
			syscall.CLONE_NEWNET,
		Unshareflags: syscall.CLONE_NEWNS,
	}

	return cmd, func() {
		closeProcessIO(cmd)
	}, nil
}

// 设置容器进程的 IO
func configureProcessIO(cmd *exec.Cmd, ctx RuntimeContext) error {
	// 如果是 -d 后台模式运行，进程将信息输出到日志
	if ctx.Request.Detach {
		logFile, err := os.OpenFile(store.LogPath(ctx.ContainerID), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("open container log file: %w", err)
		}
		cmd.Stdin = nil
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		return nil
	}

	// 否则直接输出到 stdio
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return nil
}

// 关闭容器进程 IO
func closeProcessIO(cmd *exec.Cmd) {
	for _, stream := range []any{cmd.Stdout, cmd.Stderr} {
		if file, ok := stream.(*os.File); ok && file != os.Stdout && file != os.Stderr {
			_ = file.Close()
		}
	}
}

// 应用资源控制组
func applyRuntimeResources(pid int, resources cgroups.ResourceConfig) (func(), error) {
	if resources.MemoryLimit == "" && resources.CPUWeight == 0 {
		return func() {}, nil
	}

	manager := cgroups.NewManager(fmt.Sprintf("covet-%d", time.Now().UnixNano()))
	if err := manager.Apply(pid, resources); err != nil {
		return nil, err
	}
	return func() {
		_ = manager.Destroy()
	}, nil
}

// 生成容器元信息
func newContainerMetadata(ctx RuntimeContext) meta.Container {
	return meta.Container{
		ID:          ctx.ContainerID,
		Command:     append([]string(nil), ctx.Request.Command...),
		Image:       ctx.Request.Image,
		Mounts:      toMetadataMounts(ctx.Request.Mounts),
		MemoryLimit: ctx.Request.Resources.MemoryLimit,
		CPUWeight:   ctx.Request.Resources.CPUWeight,
		Status:      meta.StateRunning,
		CreatedAt:   time.Now().UTC(),
	}
}

// 创建一个 mount 信息副本到 metadata
func toMetadataMounts(mounts []mount.Mount) []mount.Mount {
	if len(mounts) == 0 {
		return nil
	}
	var cloned []mount.Mount
	out := append(cloned, mounts...)
	return out
}

// 生成随机容器 ID
func newContainerID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("covet-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
