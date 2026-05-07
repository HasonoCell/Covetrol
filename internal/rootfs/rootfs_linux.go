//go:build linux

package rootfs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"covet/internal/image"
	"covet/internal/meta"
	"covet/internal/store"
)

const defaultPathEnv = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

func PrepareOverlay(containerID, imageName string) (string, error) {
	lowerDir := store.ContainerLowerDir(containerID)
	upperDir := store.ContainerUpperDir(containerID)
	workDir := store.ContainerWorkDir(containerID)
	mergedDir := store.ContainerMergedDir(containerID)
	overlayDirs := []string{lowerDir, upperDir, workDir, mergedDir}

	// 先清理旧的 rootfs
	if err := os.RemoveAll(store.ContainerRootFSDir(containerID)); err != nil {
		return "", fmt.Errorf("reset container rootfs dir: %w", err)
	}

	// 准备好 overlay 所需要的四个 dirs
	for _, dir := range overlayDirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create overlay dir %q: %w", dir, err)
		}
	}

	// 提前在 upper 层里放好 .pivot_root，避免后面在 merged 里临时创建目录时触发只读限制
	if err := os.MkdirAll(filepath.Join(upperDir, ".pivot_root"), 0o700); err != nil {
		return "", fmt.Errorf("create overlay pivot_root dir: %w", err)
	}

	// lowerDir 作为容器的 rootfs 路径
	if err := image.Unpack(imageName, lowerDir); err != nil {
		return "", err
	}

	// 前面只创建了 dirs，这里正式以 overlay 的方式挂载
	data := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lowerDir, upperDir, workDir)
	// merge 作为容器内进程的根挂载点
	if err := syscall.Mount("overlay", mergedDir, "overlay", 0, data); err != nil {
		return "", fmt.Errorf("mount overlay rootfs for image %q: %w", imageName, err)
	}

	return mergedDir, nil
}

func Cleanup(containerMeta meta.Container) error {
	if containerMeta.Image == "" || containerMeta.RootFS == "" {
		return nil
	}
	if err := syscall.Unmount(containerMeta.RootFS, syscall.MNT_DETACH); err != nil && err != syscall.EINVAL && err != syscall.ENOENT {
		return fmt.Errorf("unmount container rootfs %q: %w", containerMeta.RootFS, err)
	}
	return nil
}

func Pivot(rootfs string) error {
	absRootFS, err := filepath.Abs(rootfs)
	if err != nil {
		return fmt.Errorf("resolve rootfs path %q: %w", rootfs, err)
	}

	// pivot_root 要求新的根目录必须是一个挂载点，所以这里确保 rootfs 是一个挂载点（MS_BIND mount 一遍）
	if err := syscall.Mount(absRootFS, absRootFS, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("bind mount rootfs %q: %w", absRootFS, err)
	}

	// 存放旧根目录
	oldRoot := filepath.Join(absRootFS, ".pivot_root")
	if err := os.MkdirAll(oldRoot, 0o700); err != nil {
		return fmt.Errorf("create old root dir %q: %w", oldRoot, err)
	}

	// 绑定挂载宿主机的 /dev 到容器 rootfs 的 /dev 中，确保容器至少有一个最小可用的 /dev
	// 绑定挂载本质就是让 vfs 拦截对 target 的操作（读写），重定向到 source 中去
	if err := os.MkdirAll(filepath.Join(absRootFS, "dev"), 0o755); err != nil {
		return fmt.Errorf("create /dev in rootfs %q: %w", absRootFS, err)
	}
	if err := syscall.Mount("/dev", filepath.Join(absRootFS, "dev"), "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("bind mount host /dev into rootfs %q: %w", absRootFS, err)
	}

	// 开始 pivot
	if err := syscall.PivotRoot(absRootFS, oldRoot); err != nil {
		return fmt.Errorf("pivot root to %q: %w", absRootFS, err)
	}
	// 确保程序的工作目录是在根目录下
	if err := syscall.Chdir("/"); err != nil {
		return fmt.Errorf("chdir to new root: %w", err)
	}

	oldRoot = "/.pivot_root"
	// 卸载旧根目录
	if err := syscall.Unmount(oldRoot, syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("unmount old root %q: %w", oldRoot, err)
	}
	// 移除旧根所在文件夹
	if err := os.Remove(oldRoot); err != nil && !os.IsPermission(err) && !isReadOnlyFS(err) {
		return fmt.Errorf("remove old root dir %q: %w", oldRoot, err)
	}
	return nil
}

func ResolveCommandPath(command string) (string, error) {
	if strings.Contains(command, "/") {
		return command, nil
	}
	return resolveCommandInRootFS("/", command, os.Getenv("PATH"))
}

func ValidateImage(imageName string) error {
	if imageName == "" {
		return fmt.Errorf("image name is required")
	}
	if _, err := os.Stat(store.ImagePath(imageName)); err != nil {
		return fmt.Errorf("stat image %q: %w", imageName, err)
	}
	return nil
}

func resolveCommandInRootFS(rootfs, command, pathEnv string) (string, error) {
	rootfs = filepath.Clean(rootfs)
	if strings.Contains(command, "/") {
		candidate := filepath.Clean(filepath.Join(rootfs, command))
		return validateResolvedPath(rootfs, candidate, command)
	}
	if pathEnv == "" {
		pathEnv = defaultPathEnv
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		trimmed := strings.TrimPrefix(dir, "/")
		candidate := filepath.Join(rootfs, trimmed, command)
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

func isReadOnlyFS(err error) bool {
	pathErr, ok := err.(*os.PathError)
	if !ok {
		return false
	}
	return pathErr.Err == syscall.EROFS
}
