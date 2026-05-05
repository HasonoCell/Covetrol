//go:build linux

package container

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"covet/internal/cgroups"
)

const initEnv = "COVET_STAGE"
const initStage = "init"

func Run(command []string, resources cgroups.ResourceConfig) error {
	if os.Getenv(initEnv) == initStage {
		// 子进程任务
		return runContainerInit(command)
	}

	// 项目编译为二进制文件后可能会放在诸如 /usr/local/bin 这样的目录当中
	// 所以要先获取获取可执行文件的绝对路径或者说名称
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}

	// 容器引擎作为父进程，在创建子进程前完成准备工作
	cmd := exec.Command(self, append([]string{"run"}, command...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// 通过环境变量区分父子进程，子进程再次进入 container.Run() 时，
	// 发现这个环境变量已经存在，就不再继续创建下一层子进程，而是直接进入初始化逻辑
	cmd.Env = append(os.Environ(), initEnv+"="+initStage)
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
	// 为什么要这么做？这样父进程能拿到 pid，把子进程放进 cgroup 后再等待退出，真正实现限制
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start container process: %w", err)
	}

	var manager *cgroups.Manager
	if resources.MemoryLimit != "" || resources.CPUWeight > 0 {
		manager = cgroups.NewManager(fmt.Sprintf("covet-%d", time.Now().UnixNano()))
		if err := manager.Apply(cmd.Process.Pid, resources); err != nil {
			// 如果 cgroups 配置时出现错误，
			_ = cmd.Process.Kill()    // 发送子进程 kill 信号，子进程变为 zombie
			_, _ = cmd.Process.Wait() // 父进程 wait 等待 kernel 回收子进程
			return err
		}
		defer manager.Destroy()
	}

	return cmd.Wait()
}

func runContainerInit(command []string) error {
	// 子进程即容器内进程。前面 namespace flags 创建好了 namespace
	// 现在调用一系列 syscall 去初始化配置 namespace
	if err := syscall.Sethostname([]byte("covet")); err != nil {
		return fmt.Errorf("set hostname: %w", err)
	}

	if err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("make mount propagation private: %w", err)
	}

	if err := syscall.Mount("proc", "/proc", "proc", uintptr(0), ""); err != nil {
		return fmt.Errorf("mount /proc: %w", err)
	}
	defer syscall.Unmount("/proc", 0)

	path, err := exec.LookPath(command[0])
	if err != nil {
		return fmt.Errorf("find command %q: %w", command[0], err)
	}

	// 通过 exec 将原本通过 go 代码创建的子进程彻底替换为目标进程
	return syscall.Exec(path, command, os.Environ())
}
