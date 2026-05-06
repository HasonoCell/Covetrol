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
	case "list", "ps":
		return list()
	case "logs":
		return logs(args[1:])
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
	rootfs := flagset.String("rootfs", "", "container root filesystem path")
	detach := flagset.Bool("d", false, "run container in background")

	if err := flagset.Parse(args); err != nil {
		return fmt.Errorf("parse run flags: %w", err)
	}

	// 解析子命令中的无名参数（即最终要在容器中启动的进程）
	// 举个例子：covet run --mem 256m --cpu-weight 100 /bin/sh
	// mem 和 cpu 都是子命令参数被 flagset 收集，最后剩下的 /bin/sh 就是最终命令
	command := flagset.Args()
	if len(command) == 0 {
		return fmt.Errorf("run requires a command, for example: covet run /bin/sh")
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
		RootFS:  *rootfs,
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

func list() error {
	// 从本地状态目录里读取容器元数据并以表格形式输出
	metas, err := store.ListMetadata()
	if err != nil {
		return err
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "ID\tPID\tSTATUS\tCREATED\tCOMMAND")
	for _, meta := range metas {
		fmt.Fprintf(writer, "%s\t%d\t%s\t%s\t%s\n",
			meta.ID,
			meta.PID,
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

func usageError(prefix string) error {
	usage := "usage:\n  covet run [--rootfs /path/to/rootfs] [--mem 256m] [--cpu-weight 100] [-d] <command> [args...]\n  covet list\n  covet logs <container-id>\n\ncurrent commands:\n  run      start a process in isolated namespaces, optionally switch rootfs, and optionally attach cgroup v2 limits (linux only)\n  list     show persisted container metadata\n  logs     print the container log file"
	if prefix == "" {
		return errors.New(usage)
	}

	return fmt.Errorf("%s\n\n%s", prefix, usage)
}
