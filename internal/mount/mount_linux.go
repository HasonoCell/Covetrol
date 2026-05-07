//go:build linux

package mount

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// 遍历所有挂载项
func Apply(mounts []Mount) error {
	for _, mount := range mounts {
		if err := applyOne(mount); err != nil {
			return err
		}
	}
	return nil
}

func applyOne(m Mount) error {
	// m.Target 这里注意是容器内路径，如 /tmp/data
	target, err := safeTarget(m.Target)
	if err != nil {
		return err
	}
	if err := prepareTarget(target, m.SourceIsDir); err != nil {
		return err
	}
	// 绑定挂载
	if err := syscall.Mount(m.Source, target, "", syscall.MS_BIND, ""); err != nil {
		return fmt.Errorf("bind mount %q to %q: %w", m.Source, m.Target, err)
	}
	if !m.ReadOnly {
		return nil
	}
	// 如果绑定挂载希望是 ro 的，那么以 ro 的方式 remount（MS_REMOUNT）一次
	if err := syscall.Mount("", target, "", uintptr(syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY), ""); err != nil {
		return fmt.Errorf("remount bind mount %q as read-only: %w", m.Target, err)
	}
	return nil
}

// 保证 target 路径是安全的（必须要是绝对路径，且防止路径逃逸）
func safeTarget(target string) (string, error) {
	cleanTarget := filepath.Clean(target)
	if !filepath.IsAbs(cleanTarget) {
		return "", fmt.Errorf("mount target %q must be an absolute container path", target)
	}
	hostPath := filepath.Clean(filepath.Join("/", cleanTarget))
	if hostPath != cleanTarget {
		return "", fmt.Errorf("mount target %q escapes container root", target)
	}
	return hostPath, nil
}

func prepareTarget(target string, isDir bool) error {
	if isDir {
		// 如果 source 是一个目录文件，那创建以 target 为路径的新目录
		// 比如 /tmp/data:/data，那就先创建 /data
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("create mount target dir %q: %w", target, err)
		}
		return nil
	}

	// 如果 source 是一个普通文件，比如 /tmp/app.conf:/etc/app.conf
	// 那也要保证容器内：1. 父目录 /etc 存在；2. app.conf 文件存在
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create mount target parent for %q: %w", target, err)
	}
	file, err := os.OpenFile(target, os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("create mount target file %q: %w", target, err)
	}
	return file.Close()
}
