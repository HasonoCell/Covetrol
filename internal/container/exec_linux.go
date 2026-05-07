//go:build linux

package container

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	"covet/internal/meta"
	"covet/internal/store"
)

// docker exec 这个命令当时学 docker 的时候还不太明白有啥作用，写完这个文件一下子就清晰了
// 比如我们 docker run -d nginx 启动一个 nginx 容器，那么容器主进程（pid=1）就是 nginx 服务进程
// 但是如果我们想查看一下容器中 nginx 的日志文件，而主进程的 nginx 服务也不能停，无法直接执行命令或者派生子进程，怎么办？
// 我们就可以通过 docker exec <container_id> <command> 直接新开一个和容器主进程具有相同父进程（即容器引擎）的新进程
// 然后在这个新进程中执行命令～

// 其实 docker exec <container_id> <command> 就是在一个已存在的容器中根据 command 新执行一个进程，
// 执行完就结束，不会影响容器内主进程。搞清这个，exec 命令的逻辑就很清楚了
// container_id -> pid -> 进入容器内 -> 执行 <command>
func Exec(containerID string, command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("exec requires a command")
	}

	containerMeta, err := store.LoadMetadata(containerID)
	if err != nil {
		return err
	}

	// 拿到 metadata
	containerMeta, err = store.RefreshMetadata(containerMeta)
	if err != nil {
		return err
	}
	if containerMeta.Status != meta.StateRunning || containerMeta.PID <= 0 {
		return fmt.Errorf("container %s is not running", containerID)
	}

	return execInContainer(containerMeta.PID, command)
}

func execInContainer(pid int, command []string) error {
	// 一个 goroutine 在 go 内部调度器的调度下可能会在多个 os 线程之间切换
	// LockOSThread 可以让一个 goroutine 锁死在一个 os 线程上
	// 为什么要这么做？因为 linux namespace 机制是基于线程级别的
	// 也就是说，不同的执行流（线程）有不同的资源视图（namespace，即 nsproxy 指针）
	runtime.LockOSThread()
	// 等到 exec 结束就可以解除锁死了
	defer runtime.UnlockOSThread()

	nsFiles, err := openNamespaceFiles(pid)
	if err != nil {
		return err
	}
	// 记得关文件～
	defer closeNamespaceFiles(nsFiles)

	// 先进入会立即影响当前线程的 namespace
	for _, nsFile := range nsFiles[:4] { // 这里包含了 ipc, uts, net, mnt

		// setns 系统调用会将当前线程加入到指定的 Namespace 中
		// 为什么需要 setns？原因就是我们通过 exec <command> 开的新进程和容器主进程不是父子关系（不是通过 fork / clone 创建的），
		// 所以不会继承容器主进程的 namespace，那么就和之前一样，我们也要通过 setns 重新限制新进程的 namespace

		if err := sys_setns(int(nsFile.Fd()), 0); err != nil {
			return err
		}
	}

	// setns 到 mount namespace 之后，还需要把当前进程的根目录切到目标容器根
	// 否则绝对路径仍然会从宿主进程当前 root 开始解析
	rootPath := fmt.Sprintf("/proc/%d/root", pid)
	// 所以需要手动 chroot（和前面的 pivot_root 注意区分）
	if err := syscall.Chroot(rootPath); err != nil {
		return fmt.Errorf("chroot to %s: %w", rootPath, err)
	}
	if err := syscall.Chdir("/"); err != nil {
		return fmt.Errorf("chdir to / after chroot: %w", err)
	}

	// PID namespace 的 setns 只会影响后续创建的子进程，所以要放在启动目标命令前
	if err := sys_setns(int(nsFiles[4].Fd()), 0); err != nil {
		return err
	}

	path, err := resolveCommandPath(command[0], "")
	if err != nil {
		return fmt.Errorf("find exec command %q: %w", command[0], err)
	}

	// 为新进程隔离好 namespace 后就可以愉快的执行了～
	cmd := exec.Command(path, command[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	// 直接 run，不用先 start 后手动 wait
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run exec command: %w", err)
	}

	return nil
}

// /proc/<pid>/ns 可以看见该进程的所有 namespace 文件
func openNamespaceFiles(pid int) ([]*os.File, error) {
	namespaceNames := []string{"ipc", "uts", "net", "mnt", "pid"}
	nsFiles := make([]*os.File, 0, len(namespaceNames))

	for _, nsName := range namespaceNames {
		path := fmt.Sprintf("/proc/%d/ns/%s", pid, nsName)
		file, err := os.Open(path)
		if err != nil {
			closeNamespaceFiles(nsFiles)
			return nil, fmt.Errorf("open namespace file %s: %w", path, err)
		}
		nsFiles = append(nsFiles, file)
	}
	return nsFiles, nil
}

func closeNamespaceFiles(files []*os.File) {
	for _, file := range files {
		_ = file.Close()
	}
}
