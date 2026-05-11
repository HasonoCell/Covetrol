//go:build linux

package container

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"syscall"

	"covetrol/covet/internal/mount"
	"covetrol/covet/internal/network"
	"covetrol/covet/internal/rootfs"
)

const initEnv = "COVET_STAGE"
const initStage = "init"
const rootfsEnv = "COVET_ROOTFS"
const mountsEnv = "COVET_MOUNTS"
const networkEnv = "COVET_NETWORK"
const initSyncFDEnv = "COVET_INIT_SYNC_FD"
const shareNetPIDEnv = "COVET_SHARE_NET_PID"

func runContainerInit(command []string, mergedRootFS, mountsJSON string) error {
	// 先等父进程完成所有配置后发信号，再初始化容器
	if err := waitForRuntimeReady(); err != nil {
		return err
	}
	// 首先加入要共享的 netns
	if err := joinSharedNetNamespace(); err != nil {
		return err
	}

	// 子进程即容器内进程。前面 namespace flags 创建好了 namespace
	// 现在调用一系列 syscall 去初始化配置 namespace
	if err := syscall.Sethostname([]byte("covet")); err != nil {
		return fmt.Errorf("set hostname: %w", err)
	}

	// 关闭文件系统的共享机制
	if err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("make mount propagation private: %w", err)
	}

	// 为什么要先 mount，再去 pivot 替换容器根路径？
	// mount 要求的 source 都应该是宿主机上的真实文件路径（不管是 /tmp/data 还是 /.../.covet/volumes/data）
	// 如果 pivot 再去 mount，容器内进程的 / 已经变成了新的 rootfs 的根路径了，无法再看见原来的宿主机真实文件路径
	// 也就是说，如果先 pivot 再 mount，即使能拿到正确的 target，但是已经丢失了原来的 source
	if mountsJSON != "" {
		var mounts []mount.Mount
		if err := json.Unmarshal([]byte(mountsJSON), &mounts); err != nil {
			return fmt.Errorf("decode bind mounts: %w", err)
		}
		if err := mount.Apply(mergedRootFS, mounts); err != nil {
			return err
		}
	}

	if mergedRootFS != "" {
		if err := rootfs.Pivot(mergedRootFS); err != nil {
			return err
		}
	}

	// 解析父进程通过 env 传下来的 network config，开始容器侧 network setup
	networkJSON := os.Getenv(networkEnv)
	if networkJSON != "" {
		var cfg network.Config
		if err := json.Unmarshal([]byte(networkJSON), &cfg); err != nil {
			return fmt.Errorf("decode network config: %w", err)
		}

		// 如果不是共享才 setup，否则前面 joinSharedNetNamespace 已经加入 netns 了
		if os.Getenv(shareNetPIDEnv) == "" {
			if err := network.SetupContainer(cfg); err != nil {
				return err
			}
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
	path, err := rootfs.ResolveCommandPath(command[0])
	if err != nil {
		return fmt.Errorf("find command %q: %w", command[0], err)
	}

	// 通过 exec 将原本通过 go 代码创建的子进程彻底替换为目标进程
	return syscall.Exec(path, command, os.Environ())
}

func joinSharedNetNamespace() error {
	sharePIDValue := os.Getenv(shareNetPIDEnv)
	if sharePIDValue == "" {
		return nil
	}
	sharePID, err := strconv.Atoi(sharePIDValue)
	if err != nil {
		return fmt.Errorf("parse %s=%q: %w", shareNetPIDEnv, sharePIDValue, err)
	}
	// 如何加入已有 netns？通过打开要共享的容器的 /proc/<pid>/ns/net 作为 file
	nsFile, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", sharePID))
	if err != nil {
		return fmt.Errorf("open target net namespace for pid %d: %w", sharePID, err)
	}
	defer nsFile.Close()
	// 再把该 file 的 fd 当作参数调用 setns
	if err := sys_setns(int(nsFile.Fd()), 0); err != nil {
		return err
	}
	return nil
}

func waitForRuntimeReady() error {
	// 拿到父进程传下来的 ExtraFiles fd
	fdValue := os.Getenv(initSyncFDEnv)
	if fdValue == "" {
		return nil
	}

	// fd 转成数字
	fd, err := strconv.Atoi(fdValue)
	if err != nil {
		return fmt.Errorf("parse %s=%q: %w", initSyncFDEnv, fdValue, err)
	}

	// 这里 fd 对应的本质是一个 pipe 读端，将这个 pipe 包装成一个 file
	file := os.NewFile(uintptr(fd), "covet-init-sync")
	if file == nil {
		return fmt.Errorf("open init sync fd %d", fd)
	}
	defer file.Close()

	buf := make([]byte, 1)
	// 关键！如果 pipe 写端一直不写数据，file.Read 会一直阻塞，这样就实现了等待
	_, err = file.Read(buf)
	if err != nil && err != io.EOF {
		return fmt.Errorf("wait for runtime network setup: %w", err)
	}
	return nil
}
