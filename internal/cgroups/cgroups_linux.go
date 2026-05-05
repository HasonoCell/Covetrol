//go:build linux

package cgroups

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ResourceConfig struct {
	MemoryLimit string
	CPUWeight   int
}

// 管理容器的 cgroups 写入路径
type Manager struct {
	path string
}

func NewManager(name string) *Manager {
	// 容器的 cgroups 配置文件写入路径
	return &Manager{path: filepath.Join("/sys/fs/cgroup", name)}
}

// 将参数应用到 cgroups 配置文件中
func (m *Manager) Apply(pid int, cfg ResourceConfig) error {
	if err := os.MkdirAll(m.path, 0o755); err != nil {
		return fmt.Errorf("create cgroup dir %q: %w", m.path, err)
	}

	if cfg.MemoryLimit != "" {
		if err := os.WriteFile(filepath.Join(m.path, "memory.max"), []byte(cfg.MemoryLimit), 0o644); err != nil {
			return fmt.Errorf("set memory.max: %w", err)
		}
	}

	if cfg.CPUWeight > 0 {
		if err := os.WriteFile(filepath.Join(m.path, "cpu.weight"), []byte(strconv.Itoa(cfg.CPUWeight)), 0o644); err != nil {
			return fmt.Errorf("set cpu.weight: %w", err)
		}
	}

	if err := os.WriteFile(filepath.Join(m.path, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return fmt.Errorf("write cgroup.procs: %w", err)
	}

	return nil
}

// 销毁容器时清除对应的 cgroups
func (m *Manager) Destroy() error {
	if err := os.Remove(m.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove cgroup dir %q: %w", m.path, err)
	}

	return nil
}

func ParseMemoryLimit(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "", nil
	}

	if value == "max" {
		return "max", nil
	}

	// 转换单位
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(value, "kb"):
		multiplier = 1024
		value = strings.TrimSuffix(value, "kb")
	case strings.HasSuffix(value, "k"):
		multiplier = 1024
		value = strings.TrimSuffix(value, "k")
	case strings.HasSuffix(value, "mb"):
		multiplier = 1024 * 1024
		value = strings.TrimSuffix(value, "mb")
	case strings.HasSuffix(value, "m"):
		multiplier = 1024 * 1024
		value = strings.TrimSuffix(value, "m")
	case strings.HasSuffix(value, "gb"):
		multiplier = 1024 * 1024 * 1024
		value = strings.TrimSuffix(value, "gb")
	case strings.HasSuffix(value, "g"):
		multiplier = 1024 * 1024 * 1024
		value = strings.TrimSuffix(value, "g")
	}

	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return "", fmt.Errorf("parse memory limit %q: %w", value, err)
	}
	if n <= 0 {
		return "", fmt.Errorf("memory limit must be greater than 0")
	}

	return strconv.FormatInt(n*multiplier, 10), nil
}
