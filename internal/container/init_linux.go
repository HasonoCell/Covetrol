//go:build linux

package container

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"

	"covet/internal/mount"
	"covet/internal/rootfs"
)

const initEnv = "COVET_STAGE"
const initStage = "init"
const rootfsEnv = "COVET_ROOTFS"
const mountsEnv = "COVET_MOUNTS"

func runContainerInit(command []string, mergedRootFS, mountsJSON string) error {
	// 子进程即容器内进程。前面 namespace flags 创建好了 namespace
	// 现在调用一系列 syscall 去初始化配置 namespace
	if err := syscall.Sethostname([]byte("covet")); err != nil {
		return fmt.Errorf("set hostname: %w", err)
	}

	// 关闭文件系统的共享机制
	if err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("make mount propagation private: %w", err)
	}

	if mergedRootFS != "" {
		if err := rootfs.Pivot(mergedRootFS); err != nil {
			return err
		}
	}

	// 子进程要在设置好新的根路径 / 后才开始绑定挂载
	if mountsJSON != "" {
		var mounts []mount.Mount
		// 得到 mount 参数，开始 bind mount
		if err := json.Unmarshal([]byte(mountsJSON), &mounts); err != nil {
			return fmt.Errorf("decode bind mounts: %w", err)
		}
		if err := mount.Apply(mounts); err != nil {
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
	path, err := rootfs.ResolveCommandPath(command[0])
	if err != nil {
		return fmt.Errorf("find command %q: %w", command[0], err)
	}

	// 通过 exec 将原本通过 go 代码创建的子进程彻底替换为目标进程
	return syscall.Exec(path, command, os.Environ())
}
