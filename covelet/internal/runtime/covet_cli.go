package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// 因为目前 covet 的 meta 属于 internal package，所以再手动定义一下容器元数据
type covetContainerMeta struct {
	ID     string `json:"id"`
	Image  string `json:"image,omitempty"`
	Status string `json:"status"`
	// 一个 pod 暴露出去的 ip，本质上是 pod 中 infra container 的 ip
	IP string `json:"ip_address,omitempty"`
}

// 容器引擎 covet 的 struct 抽象
type CovetCLI struct {
	BinaryPath string
	WorkingDir string
}

func NewCovetCLI(binaryPath, workingDir string) *CovetCLI {
	return &CovetCLI{
		BinaryPath: binaryPath,
		WorkingDir: workingDir,
	}
}

// 将配置文件（ContainerSpec）转换为容器引擎 covet 对应的命令从而启动容器
func (r *CovetCLI) RunContainer(req RunContainerRequest) (string, error) {
	// 基本参数
	args := []string{"run", "-d"}

	// 共享 infra container 的 netns
	if req.ShareNetWith != "" {
		args = append(args, "--share-net-with", req.ShareNetWith)
	}

	// 加上 volume 挂载有关参数
	for _, volume := range req.Container.Volumes {
		spec := volume.Name + ":" + volume.MountPath
		if volume.ReadOnly {
			spec += ":ro"
		}
		args = append(args, "-v", spec)
	}

	// 加上要启动的容器的 image
	args = append(args, req.Container.Image)
	if len(req.Container.Command) > 0 {
		args = append(args, req.Container.Command...)
	}
	if len(req.Container.Args) > 0 {
		args = append(args, req.Container.Args...)
	}

	// 调用实际 run 函数
	output, err := r.run(args...)
	if err != nil {
		return "", err
	}
	containerID := strings.TrimSpace(output)
	if containerID == "" {
		return "", fmt.Errorf("covet run did not return a container id")
	}
	return containerID, nil
}

// 调用容器引擎的 stop command
func (r *CovetCLI) StopContainer(id string) error {
	_, err := r.run("stop", id)
	return err
}

// 调用容器引擎的 rm command
func (r *CovetCLI) RemoveContainer(id string) error {
	_, err := r.run("rm", id)
	return err
}

// 调用容器引擎的 inspect command
func (r *CovetCLI) InspectContainer(id string) (ContainerInfo, error) {
	metadataPath := filepath.Join(r.WorkingDir, ".covet", "containers", id, "metadata.json")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return ContainerInfo{}, fmt.Errorf("read covet metadata for %q: %w", id, err)
	}

	var containerMeta covetContainerMeta
	if err := json.Unmarshal(data, &containerMeta); err != nil {
		return ContainerInfo{}, fmt.Errorf("unmarshal covet metadata for %q: %w", id, err)
	}

	return ContainerInfo{
		ID:     containerMeta.ID,
		Status: containerMeta.Status,
		Image:  containerMeta.Image,
		IP:     containerMeta.IP,
	}, nil
}

// 实际的 run 函数，本质上就是去找 covet 的可执行二进制文件，然后传入参数并执行
func (r *CovetCLI) run(args ...string) (string, error) {
	cmd := exec.Command(r.BinaryPath, args...)
	cmd.Dir = r.WorkingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run %s %s: %w: %s", r.BinaryPath, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
