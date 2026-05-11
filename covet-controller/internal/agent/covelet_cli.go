package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	corev1 "covetrol/pkg/apis/core/v1"
	"gopkg.in/yaml.v3"
)

// 为 covelet 实现 NodeAgent 接口
type CoveletCLI struct {
	BinaryPath string
	WorkingDir string
}

func NewCoveletCLI(binaryPath, workingDir string) *CoveletCLI {
	return &CoveletCLI{
		BinaryPath: binaryPath,
		WorkingDir: workingDir,
	}
}

// ApplyPod 的实现思路是，先把 pod 结构体序列化为 yaml，
// 将 yaml 写成一个临时文件，让 covelet 直接读这个 yaml 配置文件 run 起来
func (c *CoveletCLI) ApplyPod(pod corev1.Pod) error {
	tempFile, err := os.CreateTemp("", "covetrol-pod-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp pod file: %w", err)
	}
	// 删除临时文件
	defer os.Remove(tempFile.Name())

	data, err := yaml.Marshal(pod)
	if err != nil {
		tempFile.Close()
		return fmt.Errorf("marshal pod yaml: %w", err)
	}
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return fmt.Errorf("write temp pod file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp pod file: %w", err)
	}
	_, err = c.run("run", "-f", tempFile.Name())
	return err
}

func (c *CoveletCLI) GetPod(name string) (corev1.Pod, error) {
	output, err := c.run("get", "pod", name)
	if err != nil {
		return corev1.Pod{}, err
	}
	var pod corev1.Pod
	if err := json.Unmarshal([]byte(output), &pod); err != nil {
		return corev1.Pod{}, fmt.Errorf("unmarshal pod %q: %w", name, err)
	}
	return pod, nil
}

func (c *CoveletCLI) ListPods() ([]corev1.Pod, error) {
	output, err := c.run("list", "pods")
	if err != nil {
		return nil, err
	}
	var pods []corev1.Pod
	if err := json.Unmarshal([]byte(output), &pods); err != nil {
		return nil, fmt.Errorf("unmarshal pod list: %w", err)
	}
	return pods, nil
}

func (c *CoveletCLI) DeletePod(name string) error {
	_, err := c.run("delete", "pod", name)
	return err
}

func (c *CoveletCLI) run(args ...string) (string, error) {
	cmd := exec.Command(c.BinaryPath, args...)
	cmd.Dir = c.WorkingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run %s %s: %w: %s", filepath.Base(c.BinaryPath), strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
