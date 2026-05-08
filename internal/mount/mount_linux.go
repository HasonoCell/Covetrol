//go:build linux

package mount

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// 遍历所有挂载项，将宿主机 source 挂到 rootfs 内部的 target
func Apply(rootfs string, mounts []Mount) error {
	for _, mount := range mounts {
		if err := applyOne(rootfs, mount); err != nil {
			return err
		}
	}
	return nil
}

func applyOne(rootfs string, m Mount) error {
	// target 这里注意是容器内路径，如 /tmp/data
	// 需要将 target 转换为宿主机视角下 merged rootfs 内的真实路径
	target, err := safeTarget(rootfs, m.Target)
	if err != nil {
		return err
	}
	upperTarget, err := resolveUpperTarget(rootfs, m.Target)
	if err != nil {
		return err
	}
	if err := prepareTarget(target, upperTarget); err != nil {
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
func safeTarget(rootfs, target string) (string, error) {
	cleanRootFS := filepath.Clean(rootfs)
	if !filepath.IsAbs(cleanRootFS) {
		return "", fmt.Errorf("rootfs %q must be an absolute path", rootfs)
	}
	cleanTarget := filepath.Clean(target)
	if !filepath.IsAbs(cleanTarget) {
		return "", fmt.Errorf("mount target %q must be an absolute container path", target)
	}
	trimmedTarget := strings.TrimPrefix(cleanTarget, string(os.PathSeparator))
	hostPath := filepath.Clean(filepath.Join(cleanRootFS, trimmedTarget))
	rootPrefix := cleanRootFS + string(os.PathSeparator)
	if hostPath != cleanRootFS && !strings.HasPrefix(hostPath, rootPrefix) {
		return "", fmt.Errorf("mount target %q escapes container root", target)
	}
	return hostPath, nil
}

func resolveUpperTarget(rootfs, target string) (string, error) {
	cleanRootFS := filepath.Clean(rootfs)
	if !filepath.IsAbs(cleanRootFS) {
		return "", fmt.Errorf("rootfs %q must be an absolute path", rootfs)
	}
	upperRoot := filepath.Join(filepath.Dir(cleanRootFS), "upper")
	cleanUpperRoot := filepath.Clean(upperRoot)
	trimmedTarget := strings.TrimPrefix(filepath.Clean(target), string(os.PathSeparator))
	return filepath.Join(cleanUpperRoot, trimmedTarget), nil
}

func prepareTarget(target, upperTarget string) error {
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat mount target %q: %w", target, err)
	}

	return fmt.Errorf("mount target %q is missing in merged rootfs after preparing upper target %q", target, upperTarget)
}
