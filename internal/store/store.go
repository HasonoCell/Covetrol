package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"covet/internal/meta"
)

const (
	stateDirName     = ".covet/containers"
	metadataFileName = "metadata.json"
	logFileName      = "container.log"
)

// 返回整个元信息存储的路径
func BaseDir() string {
	return filepath.Join(".", stateDirName)
}

// 单个容器存储路径
func ContainerDir(id string) string {
	return filepath.Join(BaseDir(), id)
}

func MetadataPath(id string) string {
	return filepath.Join(ContainerDir(id), metadataFileName)
}

func LogPath(id string) string {
	return filepath.Join(ContainerDir(id), logFileName)
}

// 保存元信息
func SaveMetadata(containerMeta meta.Container) error {
	if err := os.MkdirAll(ContainerDir(containerMeta.ID), 0o755); err != nil {
		return fmt.Errorf("create container dir: %w", err)
	}

	// meta struct -> meta json
	data, err := json.MarshalIndent(containerMeta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	if err := os.WriteFile(MetadataPath(containerMeta.ID), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	return nil
}

// 加载元信息
func LoadMetadata(id string) (meta.Container, error) {
	data, err := os.ReadFile(MetadataPath(id))
	if err != nil {
		return meta.Container{}, fmt.Errorf("read metadata: %w", err)
	}

	var containerMeta meta.Container
	if err := json.Unmarshal(data, &containerMeta); err != nil {
		return meta.Container{}, fmt.Errorf("unmarshal metadata: %w", err)
	}

	return containerMeta, nil
}

func ReadLog(id string) ([]byte, error) {
	data, err := os.ReadFile(LogPath(id))
	if err != nil {
		return nil, fmt.Errorf("read log: %w", err)
	}
	return data, nil
}

// 打印元信息
func ListMetadata() ([]meta.Container, error) {
	entries, err := os.ReadDir(BaseDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read container state dir: %w", err)
	}

	metas := make([]meta.Container, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		containerMeta, err := LoadMetadata(entry.Name())
		if err != nil {
			return nil, err
		}

		// 每次打印元信息前刷新一遍状态
		containerMeta, err = RefreshMetadata(containerMeta)
		if err != nil {
			return nil, err
		}
		metas = append(metas, containerMeta)
	}

	sort.Slice(metas, func(i, j int) bool {
		// 时间升序排列
		return metas[i].CreatedAt.Before(metas[j].CreatedAt)
	})

	return metas, nil
}

func RefreshMetadata(containerMeta meta.Container) (meta.Container, error) {
	if containerMeta.PID <= 0 || containerMeta.Status == meta.StateStopped {
		return containerMeta, nil
	}

	if isProcessRunning(containerMeta.PID) {
		return containerMeta, nil
	}

	containerMeta.Status = meta.StateStopped
	if err := SaveMetadata(containerMeta); err != nil {
		return meta.Container{}, err
	}

	return containerMeta, nil
}

func isProcessRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}
