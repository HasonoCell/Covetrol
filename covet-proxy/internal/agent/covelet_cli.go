package agent

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	corev1 "covetrol/pkg/apis/core/v1"
)

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

func (c *CoveletCLI) run(args ...string) (string, error) {
	cmd := exec.Command(c.BinaryPath, args...)
	cmd.Dir = c.WorkingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run %s %s: %w: %s", filepath.Base(c.BinaryPath), strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
