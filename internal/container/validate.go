package container

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultPathEnv = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

func ValidateConfig(cfg Config) error {
	if len(cfg.Command) == 0 {
		return fmt.Errorf("container command is required")
	}

	// 没传 rootfs 就不做其校验
	if cfg.RootFS == "" {
		return nil
	}

	// 先检查 rootfs 是否存在
	info, err := os.Stat(cfg.RootFS)
	if err != nil {
		return fmt.Errorf("stat rootfs %q: %w", cfg.RootFS, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("--rootfs must point to a directory")
	}

	// 在指定 rootfs 里解析命令路径，比如解析 /bin/sh
	if _, err := resolveCommandInRootFS(cfg.RootFS, cfg.Command[0], os.Getenv("PATH")); err != nil {
		return err
	}

	return nil
}

func resolveCommandInRootFS(rootfs, command, pathEnv string) (string, error) {
	rootfs = filepath.Clean(rootfs)

	// 命令本身带 '/' 可以直接根据路径找，所以直接拼接到 rootfs 上
	if strings.Contains(command, "/") {
		candidate := filepath.Clean(filepath.Join(rootfs, command))
		// 进行校验
		return validateResolvedPath(rootfs, candidate, command)
	}

	if pathEnv == "" {
		pathEnv = defaultPathEnv
	}

	// 拆开 PATH
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}

		// 对每个目录拼出候选路径
		trimmed := strings.TrimPrefix(dir, "/")
		candidate := filepath.Join(rootfs, trimmed, command)
		// 校验候选路径是否安全
		if path, err := validateResolvedPath(rootfs, candidate, command); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("find command %q in rootfs %q: executable not found", command, rootfs)
}

func validateResolvedPath(rootfs, candidate, original string) (string, error) {
	cleanRoot := filepath.Clean(rootfs)
	cleanCandidate := filepath.Clean(candidate)
	if cleanRoot == string(os.PathSeparator) {
		cleanRoot = ""
	}
	prefix := cleanRoot + string(os.PathSeparator)
	if cleanRoot != "" && cleanCandidate != cleanRoot && !strings.HasPrefix(cleanCandidate, prefix) {
		return "", fmt.Errorf("command %q escapes rootfs %q", original, rootfs)
	}

	info, err := os.Stat(cleanCandidate)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("command %q resolves to a directory", original)
	}
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("command %q is not executable inside rootfs %q", original, rootfs)
	}

	return cleanCandidate, nil
}
