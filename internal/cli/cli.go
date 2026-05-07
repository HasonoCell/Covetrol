package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"covet/internal/cgroups"
	"covet/internal/container"
	"covet/internal/image"
	"covet/internal/store"
)

// main 是项目主入口，cli 来分发不同的命令
func Run(args []string) error {
	if len(args) == 0 {
		return usageError("")
	}

	switch args[0] {
	case "run":
		return run(args[1:])
	case "ps":
		return ps()
	case "logs":
		return logs(args[1:])
	case "stop":
		return stop(args[1:])
	case "rm":
		return remove(args[1:])
	case "exec":
		return exec(args[1:])
	case "commit":
		return commit(args[1:])
	case "import":
		return _import(args[1:])
	case "images":
		return images()
	case "help", "-h", "--help":
		return usageError("")
	default:
		return usageError(fmt.Sprintf("unknown command %q", args[0]))
	}
}

func run(args []string) error {
	// 支持子命令多参数
	flagset := flag.NewFlagSet("run", flag.ContinueOnError)
	flagset.SetOutput(io.Discard)

	// 解析子命令参数
	memLimit := flagset.String("mem", "", "memory limit, for example 256m")
	cpuWeight := flagset.Int("cpu-weight", 0, "cgroup v2 cpu.weight value, 1-10000")
	detach := flagset.Bool("d", false, "run container in background")

	if err := flagset.Parse(args); err != nil {
		return fmt.Errorf("parse run flags: %w", err)
	}

	// run 的位置参数第一个是镜像名，后面的才是容器内要执行的命令
	// 比如：covet run busybox-base /bin/sh
	positional := flagset.Args()
	if len(positional) == 0 {
		return fmt.Errorf("run requires an image name, for example: covet run busybox-base /bin/sh")
	}
	imageName := positional[0]
	command := positional[1:]
	if len(command) == 0 {
		// 目前还没有镜像元数据里的默认 CMD，所以先约定省略命令时默认进入 /bin/sh。
		command = []string{"/bin/sh"}
	}

	// 将诸如 256m, 256mb 这样的 mem 参数转换为纯字节数的字符串格式
	memoryLimit, err := cgroups.ParseMemoryLimit(*memLimit)
	if err != nil {
		return err
	}

	// cpu 传入的就是纯数字，自然不用解析转换了
	if *cpuWeight < 0 || *cpuWeight > 10000 {
		return fmt.Errorf("--cpu-weight must be between 1 and 10000")
	}

	cfg := container.Config{
		Command: command,
		Image:   imageName,
		Detach:  *detach,
		Resources: cgroups.ResourceConfig{
			MemoryLimit: memoryLimit,
			CPUWeight:   *cpuWeight,
		},
	}

	if err := container.ValidateConfig(cfg); err != nil {
		return err
	}

	// run 真正返回后，如果是后台模式，就把容器 ID 打给用户，后续 list/stop/rm 都靠它
	meta, err := container.Run(cfg)
	if err != nil {
		return err
	}

	if cfg.Detach {
		fmt.Println(meta.ID)
	}

	return nil
}

func ps() error {
	// 从本地状态目录里读取容器元数据并以表格形式输出
	metas, err := store.ListMetadata()
	if err != nil {
		return err
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "ID\tPID\tIMAGE\tSTATUS\tCREATED\tCOMMAND")
	for _, meta := range metas {
		fmt.Fprintf(writer, "%s\t%d\t%s\t%s\t%s\t%s\n",
			meta.ID,
			meta.PID,
			meta.Image,
			meta.Status,
			meta.CreatedAt.Format(time.RFC3339),
			strings.Join(meta.Command, " "),
		)
	}
	return writer.Flush()
}

func logs(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: covet logs <container-id>")
	}

	// 直接读取容器日志文件并输出到 stdout
	data, err := store.ReadLog(args[0])
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(data)
	return err
}

func stop(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: covet stop <container-id>")
	}

	// 根据容器 ID 发送停止信号并更新元数据状态
	return container.Stop(args[0])
}

func remove(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: covet rm <container-id>")
	}

	// 只允许删除已经停止的容器状态目录
	return container.Remove(args[0])
}

func exec(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: covet exec <container-id> <command> [args...]")
	}

	// 进入目标容器进程的 namespace，再启动新的命令
	return container.Exec(args[0], args[1:])
}

func commit(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: covet commit <rootfs-path> <image-name>")
	}

	// 将一个 rootfs 目录打包为本地镜像仓库中的 tar 文件
	return image.Commit(args[0], args[1])
}

func _import(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: covet import <image-name> <rootfs-path>")
	}

	// 从本地镜像 tar 恢复出一个新的 rootfs 目录
	return image.Import(args[0], args[1])
}

func images() error {
	// 列出当前本地镜像仓库里已有的镜像名
	imageNames, err := image.List()
	if err != nil {
		return err
	}

	for _, imageName := range imageNames {
		fmt.Println(imageName)
	}
	return nil
}

func usageError(prefix string) error {
	usage := "usage:\n  covet run [--mem 256m] [--cpu-weight 100] [-d] <image-name> [command] [args...]\n  covet ps\n  covet logs <container-id>\n  covet stop <container-id>\n  covet rm <container-id>\n  covet exec <container-id> <command> [args...]\n  covet commit <rootfs-path> <image-name>\n  covet import <image-name> <rootfs-path>\n  covet images\n\ncurrent commands:\n  run      start a process from a local image, then apply namespaces and optional cgroup v2 limits (linux only)\n  ps       show persisted container metadata\n  logs     print the container log file\n  stop     stop a running container by id\n  rm       remove a stopped container state directory\n  exec     run a new command inside an existing container\n  commit   pack a rootfs directory into a local image tar\n  import   unpack a local image tar into a rootfs directory\n  images   list image tars stored under .covet/images"
	if prefix == "" {
		return errors.New(usage)
	}

	return fmt.Errorf("%s\n\n%s", prefix, usage)
}
