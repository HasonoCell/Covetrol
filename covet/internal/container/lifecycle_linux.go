//go:build linux

package container

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"covetrol/covet/internal/cgroups"
	"covetrol/covet/internal/meta"
	"covetrol/covet/internal/network"
	"covetrol/covet/internal/rootfs"
	"covetrol/covet/internal/store"
)

func Start(id string) error {
	// start 逻辑主要就是复用。先读容器元信息
	containerMeta, err := store.LoadMetadata(id)
	if err != nil {
		return err
	}

	containerMeta, err = store.RefreshMetadata(containerMeta)
	if err != nil {
		return err
	}
	if containerMeta.Status == meta.StateRunning {
		return nil
	}
	if containerMeta.Image == "" {
		return fmt.Errorf("container %s has no image metadata to restart from", id)
	}
	if len(containerMeta.Command) == 0 {
		return fmt.Errorf("container %s has no command metadata to restart from", id)
	}

	// 容器已经停了，但上一次运行留下的 overlay 挂载可能还在。
	// start 之前先尽力清一遍，避免 PrepareOverlay 重新创建目录时撞上旧 mount。
	if err := rootfs.Cleanup(containerMeta); err != nil {
		return err
	}

	ctx := RuntimeContext{
		Request: RunRequest{
			Command: containerMeta.Command,
			Image:   containerMeta.Image,
			Detach:  true,
			Resources: cgroups.ResourceConfig{
				MemoryLimit: containerMeta.MemoryLimit,
				CPUWeight:   containerMeta.CPUWeight,
			},
		},
		ContainerID: containerMeta.ID,
		Network: network.Config{
			BridgeName:    containerMeta.Bridge,
			BridgeIP:      "10.200.0.1",
			BridgeCIDR:    "10.200.0.1/24",
			ContainerIP:   containerMeta.IPAddress,
			ContainerCIDR: containerMeta.IPAddress + "/24",
			HostVethName:  containerMeta.HostVeth,
			GuestVethName: containerMeta.GuestVeth,
			PeerVethName:  containerMeta.PeerVeth,
		},
	}

	containerMeta.PID = 0
	containerMeta.Status = meta.StateRunning
	containerMeta.RootFS = ""
	_, err = startContainer(ctx, containerMeta)
	return err
}

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
		// 进程已经退出时，仍然要做一次 rootfs 清理。
		// 这类情况通常出现在后台容器自行退出，但 overlay merged mount 还留在宿主机上。
		if err := rootfs.Cleanup(containerMeta); err != nil {
			return err
		}
		if err := network.Teardown(network.Config{HostVethName: containerMeta.HostVeth}); err != nil {
			return err
		}
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
			if err := rootfs.Cleanup(containerMeta); err != nil {
				return err
			}
			if err := network.Teardown(network.Config{HostVethName: containerMeta.HostVeth}); err != nil {
				return err
			}
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
			if err := rootfs.Cleanup(containerMeta); err != nil {
				return err
			}
			if err := network.Teardown(network.Config{HostVethName: containerMeta.HostVeth}); err != nil {
				return err
			}
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
	if err := network.Teardown(network.Config{HostVethName: containerMeta.HostVeth}); err != nil {
		return err
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
