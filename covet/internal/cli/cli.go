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

	"covetrol/covet/internal/cgroups"
	"covetrol/covet/internal/container"
	"covetrol/covet/internal/image"
	"covetrol/covet/internal/mount"
	"covetrol/covet/internal/store"
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
	case "start":
		return start(args[1:])
	case "stop":
		return stop(args[1:])
	case "rm":
		return remove(args[1:])
	case "exec":
		return exec(args[1:])
	case "unpack":
		return unpack(args[1:])
	case "pack":
		return pack(args[1:])
	case "images":
		return images()
	case "image":
		return imageCommand(args[1:])
	case "volumes":
		return volumes()
	case "volume":
		return volume(args[1:])
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
	shareNetWith := flagset.String("share-net-with", "", "join the network namespace of an existing container")
	var mounts mount.List
	flagset.Var(&mounts, "v", "mount in the form /host:/container[:ro] or volume:/container[:ro]")

	// 将解析结果放在前面定义的变量中
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
	// 如果 run 时没传 command，就采用默认 command
	if len(command) == 0 {
		var err error
		command, err = image.DefaultCommand(imageName)
		if err != nil {
			return err
		}
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

	req := container.RunRequest{
		Command:      command,
		Image:        imageName,
		Detach:       *detach,
		ShareNetWith: *shareNetWith,
		Resources: cgroups.ResourceConfig{
			MemoryLimit: memoryLimit,
			CPUWeight:   *cpuWeight,
		},
	}

	// 解析参数携带的 mount entry（target 和 source），携带到 req 中
	parsedMounts, err := mount.Parse(mounts)
	if err != nil {
		return err
	}
	req.Mounts = parsedMounts

	if err := container.ValidateRequest(req); err != nil {
		return err
	}

	// run 真正返回后，如果是后台模式，就把容器 ID 打给用户，后续 list/stop/rm 都靠它
	meta, err := container.Run(req)
	if err != nil {
		return err
	}

	if req.Detach {
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

func start(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: covet start <container-id>")
	}

	// 根据容器 ID 启动容器并更新元数据状态
	return container.Start(args[0])
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

func pack(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: covet pack <rootfs-path> <image-name>")
	}

	// 将一个 rootfs 目录打包为本地镜像仓库中的 tar 文件
	return image.Pack(args[0], args[1])
}

func unpack(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: covet unpack <image-name> <rootfs-path>")
	}

	// 从本地镜像 tar 恢复出一个新的 rootfs 目录
	return image.Unpack(args[0], args[1])
}

func images() error {
	// 列出当前本地镜像仓库里已有的镜像名
	imageNames, err := image.Images()
	if err != nil {
		return err
	}

	for _, imageName := range imageNames {
		fmt.Println(imageName)
	}
	return nil
}

func imageCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: covet image inspect <name>")
	}

	switch args[0] {
	case "inspect":
		return imageInspect(args[1:])
	default:
		return fmt.Errorf("unknown image subcommand %q", args[0])
	}
}

func imageInspect(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: covet image inspect <name>")
	}

	info, err := image.Inspect(args[0])
	if err != nil {
		return err
	}

	fmt.Printf("Name:\t%s\n", info.Name)
	fmt.Printf("Format:\t%s\n", info.Format)
	fmt.Printf("Path:\t%s\n", info.Path)
	if info.ManifestPath != "" {
		fmt.Printf("Manifest:\t%s\n", info.ManifestPath)
	}
	if info.ConfigPath != "" {
		fmt.Printf("Config:\t%s\n", info.ConfigPath)
	}
	if len(info.Cmd) == 0 {
		fmt.Printf("Cmd:\t-\n")
	} else {
		fmt.Printf("Cmd:\t%s\n", strings.Join(info.Cmd, " "))
	}
	if !info.CreatedAt.IsZero() {
		fmt.Printf("Created:\t%s\n", info.CreatedAt.Format(time.RFC3339))
	}
	if len(info.Layers) == 0 {
		fmt.Printf("Layers:\t-\n")
		return nil
	}
	fmt.Printf("Layers:\t%s\n", strings.Join(info.Layers, ", "))
	return nil
}

func volumes() error {
	volumeNames, err := mount.ListVolumes()
	if err != nil {
		return err
	}

	for _, name := range volumeNames {
		fmt.Println(name)
	}
	return nil
}

func volume(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: covet volume <inspect|rm> <name>")
	}

	switch args[0] {
	case "inspect":
		return volumeInspect(args[1:])
	case "rm":
		return volumeRemove(args[1:])
	default:
		return fmt.Errorf("unknown volume subcommand %q", args[0])
	}
}

func volumeInspect(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: covet volume inspect <name>")
	}

	name := args[0]
	info, err := mount.InspectVolume(name)
	if err != nil {
		return err
	}
	inUse, err := store.ContainersUsingVolume(name)
	if err != nil {
		return err
	}

	fmt.Printf("Name:\t%s\n", info.Name)
	fmt.Printf("Path:\t%s\n", info.Path)
	fmt.Printf("Exists:\t%t\n", info.Exists)
	fmt.Printf("ReferencedBy:\t")
	if len(inUse) == 0 {
		fmt.Println("-")
		return nil
	}
	containerIDs := make([]string, 0, len(inUse))
	for _, containerMeta := range inUse {
		containerIDs = append(containerIDs, containerMeta.ID)
	}
	fmt.Println(strings.Join(containerIDs, ", "))
	return nil
}

func volumeRemove(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: covet volume rm <name>")
	}

	name := args[0]
	inUse, err := store.ContainersUsingVolume(name)
	if err != nil {
		return err
	}
	if len(inUse) > 0 {
		containerIDs := make([]string, 0, len(inUse))
		for _, containerMeta := range inUse {
			containerIDs = append(containerIDs, containerMeta.ID)
		}
		return fmt.Errorf("volume %q is still referenced by containers: %s", name, strings.Join(containerIDs, ", "))
	}

	return mount.RemoveVolume(name)
}

func usageError(prefix string) error {
	usage := "usage:\n  covet run [--mem 256m] [--cpu-weight 100] [-d] [-v /host:/container[:ro]] [-v volume:/container[:ro]] <image-name> [command] [args...]\n  covet ps\n  covet logs <container-id>\n  covet stop <container-id>\n  covet rm <container-id>\n  covet exec <container-id> <command> [args...]\n  covet pack <rootfs-path> <image-name>\n  covet unpack <image-name> <rootfs-path>\n  covet images\n  covet image inspect <name>\n  covet volumes\n  covet volume inspect <name>\n  covet volume rm <name>\n\ncurrent commands:\n  run      start a process from a local image, then apply namespaces, mounts, and optional cgroup v2 limits (linux only)\n  ps       show persisted container metadata\n  logs     print the container log file\n  stop     stop a running container by id\n  rm       remove a stopped container state directory\n  exec     run a new command inside an existing container\n  pack     pack a rootfs directory into a local image tar\n  unpack   unpack a local image tar into a rootfs directory\n  images   list image tars stored under .covet/images\n  image    inspect image metadata\n  volumes  list named volumes stored under .covet/volumes\n  volume   inspect or remove a named volume"
	if prefix == "" {
		return errors.New(usage)
	}

	return fmt.Errorf("%s\n\n%s", prefix, usage)
}
