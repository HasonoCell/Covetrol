//go:build linux

package container

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"covet/internal/cgroups"
	"covet/internal/meta"
	"covet/internal/store"
)

const initEnv = "COVET_STAGE"
const initStage = "init"
const rootfsEnv = "COVET_ROOTFS"

// Run 返回一个 Container 结构体，用来后续管理生命周期
func Run(cfg Config) (meta.Container, error) {
	if os.Getenv(initEnv) == initStage {
		// 子进程任务
		return meta.Container{}, runContainerInit(cfg.Command, os.Getenv(rootfsEnv))
	}

	// 项目编译为二进制文件后可能会放在诸如 /usr/local/bin 这样的目录当中
	// 所以要先获取获取可执行文件的绝对路径或者说名称
	self, err := os.Executable()
	if err != nil {
		return meta.Container{}, fmt.Errorf("resolve current executable: %w", err)
	}

	// 为当前这次容器运行创建一份最小元数据，后续 list/stop/rm/logs 都靠它
	containerMeta := meta.Container{
		ID:        newContainerID(),
		Command:   append([]string(nil), cfg.Command...),
		RootFS:    cfg.RootFS,
		Status:    meta.StateRunning,
		CreatedAt: time.Now().UTC(),
	}

	if err := os.MkdirAll(store.ContainerDir(containerMeta.ID), 0o755); err != nil {
		return meta.Container{}, fmt.Errorf("create container dir: %w", err)
	}

	// 容器引擎作为父进程，在创建子进程前完成准备工作
	cmd := exec.Command(self, append([]string{"run"}, cfg.Command...)...)
	if cfg.Detach {
		// -d 后台运行时把 stdout 和 stderr 都重定向到容器日志文件中
		logFile, err := os.OpenFile(store.LogPath(containerMeta.ID), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return meta.Container{}, fmt.Errorf("open container log file: %w", err)
		}
		defer logFile.Close()
		cmd.Stdin = nil
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	// 通过环境变量区分父子进程，子进程再次进入 container.Run() 时，
	// 发现这个环境变量已经存在，就不再继续创建下一层子进程，而是直接进入初始化逻辑
	cmd.Env = append(os.Environ(), initEnv+"="+initStage, rootfsEnv+"="+cfg.RootFS)
	// 修改 SystemProcessAttributes，用于在创建新系统进程时，指定特定的操作系统属性
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWIPC |
			syscall.CLONE_NEWNET,
		Unshareflags: syscall.CLONE_NEWNS,
	}

	// Start 方法和 Run 不同的地方在于：父进程创建子进程，但先不 wait
	// 为什么要这么做？这样父进程能拿到 pid，把子进程放进 cgroup、落盘元数据后再等待退出
	if err := cmd.Start(); err != nil {
		return meta.Container{}, fmt.Errorf("start container process: %w", err)
	}

	containerMeta.PID = cmd.Process.Pid
	if err := store.SaveMetadata(containerMeta); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return meta.Container{}, err
	}

	var manager *cgroups.Manager
	if cfg.Resources.MemoryLimit != "" || cfg.Resources.CPUWeight > 0 {
		manager = cgroups.NewManager(fmt.Sprintf("covet-%d", time.Now().UnixNano()))
		if err := manager.Apply(cmd.Process.Pid, cfg.Resources); err != nil {
			// 如果 cgroups 配置时出现错误，
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
			return meta.Container{}, err
		}
		defer manager.Destroy()
	}

	if cfg.Detach {
		// 后台模式下父进程到这里就可以返回了，真正的容器进程继续在后台运行
		return containerMeta, nil
	}

	waitErr := cmd.Wait()
	// 前台模式下父进程会阻塞等待容器退出，并在退出后把状态更新为 stopped
	containerMeta.Status = meta.StateStopped
	if err := store.SaveMetadata(containerMeta); err != nil {
		return meta.Container{}, err
	}
	if waitErr != nil {
		return meta.Container{}, waitErr
	}

	return containerMeta, nil
}

func runContainerInit(command []string, rootfs string) error {
	// 子进程即容器内进程。前面 namespace flags 创建好了 namespace
	// 现在调用一系列 syscall 去初始化配置 namespace
	if err := syscall.Sethostname([]byte("covet")); err != nil {
		return fmt.Errorf("set hostname: %w", err)
	}

	// 关闭文件系统的共享机制
	if err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("make mount propagation private: %w", err)
	}

	if rootfs != "" {
		if err := setupRootFS(rootfs); err != nil {
			return err
		}
	}

	if err := os.MkdirAll("/proc", 0o555); err != nil {
		return fmt.Errorf("ensure /proc exists: %w", err)
	}

	// pid namespace 不会在容器中自动挂载 /proc
	// 所以这里需要手动挂载，对于伪文件系统，挂载源（比如这里的 "proc"）更多只用来占位
	if err := syscall.Mount("proc", "/proc", "proc", uintptr(0), ""); err != nil {
		return fmt.Errorf("mount /proc: %w", err)
	}
	defer syscall.Unmount("/proc", 0)

	// 给 exec 找最终的程序路径
	path, err := resolveCommandPath(command[0], rootfs)
	if err != nil {
		return fmt.Errorf("find command %q: %w", command[0], err)
	}

	// 通过 exec 将原本通过 go 代码创建的子进程彻底替换为目标进程
	return syscall.Exec(path, command, os.Environ())
}

func setupRootFS(rootfs string) error {
	absRootFS, err := filepath.Abs(rootfs)
	if err != nil {
		return fmt.Errorf("resolve rootfs path %q: %w", rootfs, err)
	}

	// pivot_root 要求新的根目录必须是一个挂载点，所以这里将普通目录变为一个挂载点（MS_BIND）
	if err := syscall.Mount(absRootFS, absRootFS, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("bind mount rootfs %q: %w", absRootFS, err)
	}

	// 存放旧根目录
	oldRoot := filepath.Join(absRootFS, ".pivot_root")
	if err := os.MkdirAll(oldRoot, 0o700); err != nil {
		return fmt.Errorf("create old root dir %q: %w", oldRoot, err)
	}

	// 在容器中的 rootfs 下创建 /dev
	if err := os.MkdirAll(filepath.Join(absRootFS, "dev"), 0o755); err != nil {
		return fmt.Errorf("create /dev in rootfs %q: %w", absRootFS, err)
	}

	// 绑定挂载宿主机的 /dev 到容器 rootfs 的 /dev 中，确保容器至少有一个最小可用的 /dev
	// 绑定挂载本质就是让 vfs 拦截对 target 的操作（读写），重定向到 source 中去
	if err := syscall.Mount("/dev", filepath.Join(absRootFS, "dev"), "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("bind mount host /dev into rootfs %q: %w", absRootFS, err)
	}

	// 切换根目录
	if err := syscall.PivotRoot(absRootFS, oldRoot); err != nil {
		return fmt.Errorf("pivot root to %q: %w", absRootFS, err)
	}

	// 显式地切换当前程序的工作目录，确保指向新的根目录
	if err := syscall.Chdir("/"); err != nil {
		return fmt.Errorf("chdir to new root: %w", err)
	}

	oldRoot = "/.pivot_root"
	// 卸载旧根
	if err := syscall.Unmount(oldRoot, syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("unmount old root %q: %w", oldRoot, err)
	}

	// 移除临时目录
	if err := os.Remove(oldRoot); err != nil {
		return fmt.Errorf("remove old root dir %q: %w", oldRoot, err)
	}

	return nil
}

// 子进程（即容器进程）执行命令前的最终解析 + 检验
func resolveCommandPath(command, rootfs string) (string, error) {
	if strings.Contains(command, "/") {
		return command, nil
	}

	if rootfs == "" {
		return resolveCommandInRootFS("/", command, os.Getenv("PATH"))
	}

	return resolveCommandInRootFS("/", command, os.Getenv("PATH"))
}

// 生成随机容器 ID
func newContainerID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("covet-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
