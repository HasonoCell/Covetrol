package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"covet/internal/cgroups"
	"covet/internal/container"
)

// main 是项目主入口，cli 来分发不同的命令
func Run(args []string) error {
	if len(args) == 0 {
		return usageError("")
	}

	switch args[0] {
	case "run":
		return run(args[1:])
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

	cfg := cgroups.ResourceConfig{
		MemoryLimit: memoryLimit,
		CPUWeight:   *cpuWeight,
	}

	return container.Run(command, cfg)
}

func usageError(prefix string) error {
	usage := "usage:\n  covet run [--mem 256m] [--cpu-weight 100] <command> [args...]\n\nstage 2 commands:\n  run     start a process in isolated namespaces and optionally attach cgroup v2 limits (linux only)"
	if prefix == "" {
		return errors.New(usage)
	}

	return fmt.Errorf("%s\n\n%s", prefix, usage)
}
