//go:build !linux

package rootfs

import (
	"fmt"
	"runtime"

	"covetrol/covet/internal/meta"
	"covetrol/covet/internal/mount"
)

func PrepareOverlay(containerID, imageName string, mounts []mount.Mount) (string, error) {
	_ = containerID
	_ = imageName
	_ = mounts
	return "", fmt.Errorf("rootfs overlay requires Linux; current GOOS=%s", runtime.GOOS)
}

func Cleanup(containerMeta meta.Container) error {
	_ = containerMeta
	return nil
}

func Pivot(rootfs string) error {
	_ = rootfs
	return fmt.Errorf("pivot_root requires Linux; current GOOS=%s", runtime.GOOS)
}

func ResolveCommandPath(command string) (string, error) {
	return command, nil
}

func ValidateImage(imageName string) error {
	_ = imageName
	return nil
}
