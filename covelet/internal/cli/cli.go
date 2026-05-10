package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"covetrol/covelet/internal/pod"
	"covetrol/covelet/internal/runtime"
	corev1 "covetrol/pkg/apis/core/v1"
	"gopkg.in/yaml.v3"
)

func Run(args []string) error {
	if len(args) == 0 {
		return usageError("")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	service := pod.NewService(runtime.NewCovetCLI(
		filepath.Join(cwd, "covet-bin"),
		filepath.Join(cwd, "covet"),
	))
	switch args[0] {
	case "run":
		return run(service, args[1:])
	case "get":
		return get(service, args[1:])
	case "list":
		return list(service, args[1:])
	case "delete":
		return deletePod(service, args[1:])
	case "help", "-h", "--help":
		return usageError("")
	default:
		return usageError(fmt.Sprintf("unknown command %q", args[0]))
	}
}

func run(service *pod.Service, args []string) error {
	flagset := flag.NewFlagSet("run", flag.ContinueOnError)
	flagset.SetOutput(os.Stderr)
	filePath := flagset.String("f", "", "pod spec file")
	if err := flagset.Parse(args); err != nil {
		return fmt.Errorf("parse run flags: %w", err)
	}
	if *filePath == "" {
		return fmt.Errorf("usage: covelet run -f <pod.yaml>")
	}

	podSpec, err := loadPod(*filePath)
	if err != nil {
		return err
	}
	return service.Run(podSpec)
}

func get(service *pod.Service, args []string) error {
	if len(args) != 2 || args[0] != "pod" {
		return fmt.Errorf("usage: covelet get pod <name>")
	}
	podObj, err := service.Get(args[1])
	if err != nil {
		return err
	}
	return printJSON(podObj)
}

func list(service *pod.Service, args []string) error {
	if len(args) != 1 || args[0] != "pods" {
		return fmt.Errorf("usage: covelet list pods")
	}
	pods, err := service.List()
	if err != nil {
		return err
	}
	return printJSON(pods)
}

func deletePod(service *pod.Service, args []string) error {
	if len(args) != 2 || args[0] != "pod" {
		return fmt.Errorf("usage: covelet delete pod <name>")
	}
	return service.Delete(args[1])
}

// 将配置文件解析为 Pod 结构体
func loadPod(path string) (corev1.Pod, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return corev1.Pod{}, fmt.Errorf("read pod file %q: %w", path, err)
	}

	var podObj corev1.Pod
	if err := yaml.Unmarshal(data, &podObj); err == nil {
		return podObj, nil
	}
	if err := json.Unmarshal(data, &podObj); err != nil {
		return corev1.Pod{}, fmt.Errorf("decode pod file %q as yaml or json: %w", path, err)
	}
	return podObj, nil
}

func printJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(data, '\n'))
	return err
}

func usageError(prefix string) error {
	usage := "usage:\n  covelet run -f <pod.yaml>\n  covelet get pod <name>\n  covelet list pods\n  covelet delete pod <name>"
	if prefix == "" {
		return errors.New(usage)
	}
	return fmt.Errorf("%s\n\n%s", prefix, usage)
}
