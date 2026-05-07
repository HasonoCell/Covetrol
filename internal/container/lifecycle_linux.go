//go:build linux

package container

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"covet/internal/meta"
	"covet/internal/store"
)

func Stop(id string) error {
	containerMeta, err := store.LoadMetadata(id)
	if err != nil {
		return err
	}

	containerMeta, err = store.RefreshMetadata(containerMeta)
	if err != nil {
		return err
	}
	if containerMeta.Status == meta.StateStopped || containerMeta.PID <= 0 {
		return nil
	}

	// 先发 SIGTERM，给容器主进程一个正常退出的机会
	if err := syscall.Kill(containerMeta.PID, syscall.SIGTERM); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("send SIGTERM to pid %d: %w", containerMeta.PID, err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !isProcessRunning(containerMeta.PID) {
			containerMeta.Status = meta.StateStopped
			return store.SaveMetadata(containerMeta)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 如果进程一直不退，再升级到 SIGKILL
	if err := syscall.Kill(containerMeta.PID, syscall.SIGKILL); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("send SIGKILL to pid %d: %w", containerMeta.PID, err)
	}

	for i := 0; i < 20; i++ {
		if !isProcessRunning(containerMeta.PID) {
			containerMeta.Status = meta.StateStopped
			return store.SaveMetadata(containerMeta)
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("container %s did not stop in time", id)
}

func Remove(id string) error {
	containerMeta, err := store.LoadMetadata(id)
	if err != nil {
		return err
	}

	containerMeta, err = store.RefreshMetadata(containerMeta)
	if err != nil {
		return err
	}
	if containerMeta.Status == meta.StateRunning {
		return fmt.Errorf("container %s is still running; stop it first", id)
	}

	if err := store.RemoveContainer(id); err != nil {
		return err
	}

	return nil
}

// 通过发送为 0 的 signal 来判断进程是否正在运行
func isProcessRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}
