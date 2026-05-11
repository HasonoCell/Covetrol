//go:build linux

package container

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"covetrol/covet/internal/cgroups"
	"covetrol/covet/internal/meta"
	"covetrol/covet/internal/mount"
	"covetrol/covet/internal/network"
	"covetrol/covet/internal/rootfs"
	"covetrol/covet/internal/store"
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
	// 准备该容器的 network
	if err := prepareRuntimeNetwork(&ctx); err != nil {
		return meta.Container{}, err
	}

	containerMeta.RootFS = ctx.MergedRootFS
	// 构造创建子进程的命令
	cmd, runtimeSync, cleanupIO, err := newChildCommand(ctx)
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

	// 需要目标进程 pid 才能把 peer 已经那个进程的 net namespace
	if err := setupRuntimeNetwork(&ctx, &containerMeta, cmd.Process.Pid); err != nil {
		_ = runtimeSync.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = rootfs.Cleanup(containerMeta)
		return meta.Container{}, err
	}
	// 父进程 setup host network 成功后发信号给子进程
	if err := signalRuntimeReady(runtimeSync); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = network.Teardown(ctx.Network)
		_ = rootfs.Cleanup(containerMeta)
		return meta.Container{}, err
	}
	// 保存元信息
	if err := store.SaveMetadata(containerMeta); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = network.Teardown(ctx.Network)
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
	if err := network.Teardown(ctx.Network); err != nil {
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

// 生成容器网络的有关配置
func prepareRuntimeNetwork(ctx *RuntimeContext) error {
	// 如果是共享 netns 模式，复用 netns 配置
	if ctx.Request.ShareNetWith != "" {
		// 加载复用配置的容器元信息
		sharedMeta, err := store.LoadMetadata(ctx.Request.ShareNetWith)
		if err != nil {
			return fmt.Errorf("load share-net-with container %q: %w", ctx.Request.ShareNetWith, err)
		}
		sharedMeta, err = store.RefreshMetadata(sharedMeta)
		if err != nil {
			return err
		}
		// 得到要共享 netns 的 pid
		ctx.ShareNetPID = sharedMeta.PID
		ctx.Network = network.Config{
			BridgeName:    sharedMeta.Bridge,
			BridgeIP:      "10.200.0.1",
			BridgeCIDR:    "10.200.0.1/24",
			ContainerIP:   sharedMeta.IPAddress,
			ContainerCIDR: sharedMeta.IPAddress + "/24",
			HostVethName:  sharedMeta.HostVeth,
			GuestVethName: sharedMeta.GuestVeth,
			PeerVethName:  sharedMeta.PeerVeth,
		}
		return nil
	}

	// 如果不是共享模式，创建新配置
	cfg, err := network.NewConfig(ctx.ContainerID)
	if err != nil {
		return err
	}
	ctx.Network = cfg
	return nil
}

// 操作 host 侧网络
func setupRuntimeNetwork(ctx *RuntimeContext, containerMeta *meta.Container, pid int) error {
	if ctx.Request.ShareNetWith != "" {
		containerMeta.IPAddress = ctx.Network.ContainerIP
		containerMeta.Bridge = ctx.Network.BridgeName
		containerMeta.GuestVeth = ctx.Network.GuestVethName
		containerMeta.PeerVeth = ctx.Network.PeerVethName
		return nil
	}
	if ctx.Network.ContainerIP == "" {
		return fmt.Errorf("runtime network config is not prepared")
	}
	cfg := ctx.Network
	if err := network.SetupHost(cfg, pid); err != nil {
		return err
	}
	containerMeta.IPAddress = cfg.ContainerIP
	containerMeta.Bridge = cfg.BridgeName
	containerMeta.HostVeth = cfg.HostVethName
	containerMeta.GuestVeth = cfg.GuestVethName
	containerMeta.PeerVeth = cfg.PeerVethName
	return nil
}

// 准备好创建容器内子进程的命令，返回命令和该进程的 io 清理函数
func newChildCommand(ctx RuntimeContext) (*exec.Cmd, io.Closer, func(), error) {
	// 项目编译为二进制文件后可能会放在诸如 /usr/local/bin 这样的目录当中
	// 所以要先获取获取可执行文件的绝对路径或者说名称
	self, err := os.Executable()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve current executable: %w", err)
	}

	childArgs := []string{"run", ctx.Request.Image}
	childArgs = append(childArgs, ctx.Request.Command...)
	cmd := exec.Command(self, childArgs...)
	if err := configureProcessIO(cmd, ctx); err != nil {
		return nil, nil, nil, err
	}

	// 父进程准备子进程命令时，还会创建一个管道 pipe，有什么用？主要是针对网络
	// 子进程 cmd.Start() 以后会立刻跑 runContainerInit()，但父进程这时还在忙 SetupHost
	// 如果子进程跑得太快，它会先执行 SetupContainer 配置容器内网络
	// 可这时 peer 还没被挪进 netns，子进程可能提前退出
	// 父进程再 LinkSetNsPid(...) 时就发现 PID 已死，操作失败
	//  所以现在父进程创建一个 pipe，子进程 init 一开始先 waitForRuntimeReady()
	// 父进程 SetupHost 成功后再 signalRuntimeReady()
	// 这样子进程就先等父进程把 veth/bridge/netns 处理好
	// 父进程放行，子进程再继续配置容器内 eth0
	syncReader, syncWriter, err := os.Pipe() // 父进程拿写端，子进程拿读端
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create init sync pipe: %w", err)
	}

	// 把 syncReader 放进 cmd.ExtraFiles，这样子进程启动后，会额外继承一个文件描述符
	// 我们知道 Unix 里一个进程一开始拥有的 fd 0/1/2 分别是 stdin/stdout/stderr
	// 所以 ExtraFiles 会从 3 开始编号，这里算出了这个 fd 编号，并塞进环境变量
	// COVET_INIT_SYNC_FD=<某个数字>，这样子进程就知道自己该去读哪个 fd
	cmd.ExtraFiles = append(cmd.ExtraFiles, syncReader)
	syncFD := 3 + len(cmd.ExtraFiles) - 1
	// 通过环境变量区分父子进程，子进程再次进入 Run() 时，
	// 发现这个环境变量已经存在，就不再继续创建下一层子进程，而是直接进入初始化逻辑
	cmd.Env = append(os.Environ(), initEnv+"="+initStage, rootfsEnv+"="+ctx.MergedRootFS)
	cmd.Env = append(cmd.Env, initSyncFDEnv+"="+strconv.Itoa(syncFD))
	if len(ctx.Request.Mounts) > 0 {
		// 父进程将请求参数中的 mounts 序列化为 json 存在环境变量中，从而使子进程启动后可读
		mountsJSON, err := json.Marshal(ctx.Request.Mounts)
		if err != nil {
			_ = syncReader.Close()
			_ = syncWriter.Close()
			return nil, nil, nil, fmt.Errorf("marshal bind mounts: %w", err)
		}
		cmd.Env = append(cmd.Env, mountsEnv+"="+string(mountsJSON))
	}
	if ctx.Network.ContainerIP != "" {
		// 同样在环境变量中存储 network config json
		networkJSON, err := json.Marshal(ctx.Network)
		if err != nil {
			_ = syncReader.Close()
			_ = syncWriter.Close()
			return nil, nil, nil, fmt.Errorf("marshal network config: %w", err)
		}
		cmd.Env = append(cmd.Env, networkEnv+"="+string(networkJSON))
	}
	// 如果是共享模式，将 id 通过环境变量传下去
	if ctx.ShareNetPID > 0 {
		cmd.Env = append(cmd.Env, shareNetPIDEnv+"="+strconv.Itoa(ctx.ShareNetPID))
	}
	// 修改 SystemProcessAttributes，用于在创建新系统进程时，指定特定的操作系统属性
	cloneFlags := uintptr(syscall.CLONE_NEWUTS |
		syscall.CLONE_NEWPID |
		syscall.CLONE_NEWNS |
		syscall.CLONE_NEWIPC)
	// 新容器如果是共享模式就不再创建新的 netns 了
	if ctx.Request.ShareNetWith == "" {
		cloneFlags |= syscall.CLONE_NEWNET
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:   cloneFlags,
		Unshareflags: syscall.CLONE_NEWNS,
	}

	// 返回子进程启动命令和 pipe 写端
	return cmd, syncWriter, func() {
		_ = syncReader.Close()
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

// 通过 syncWriter 往另一端发信号
func signalRuntimeReady(syncWriter io.Closer) error {
	file, ok := syncWriter.(*os.File)
	if !ok {
		return fmt.Errorf("invalid init sync writer type %T", syncWriter)
	}
	if _, err := file.Write([]byte{1}); err != nil {
		_ = file.Close()
		return fmt.Errorf("signal container init after runtime setup: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close init sync writer: %w", err)
	}
	return nil
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
		IPAddress:   ctx.Network.ContainerIP,
		Bridge:      ctx.Network.BridgeName,
		HostVeth:    ctx.Network.HostVethName,
		GuestVeth:   ctx.Network.GuestVethName,
		PeerVeth:    ctx.Network.PeerVethName,
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
