package pod

import (
	"fmt"
	"os"

	"covetrol/covelet/internal/runtime"
	"covetrol/covelet/internal/store"
	corev1 "covetrol/pkg/apis/core/v1"
)

type Service struct {
	runtime runtime.Runtime // 底层容器引擎（比如 covet）
}

func NewService(runtime runtime.Runtime) *Service {
	return &Service{runtime: runtime}
}

// 启动 pod 服务
func (s *Service) Run(pod corev1.Pod) error {
	// 先检查配置
	if err := validatePod(pod); err != nil {
		return err
	}

	status := corev1.PodStatus{Phase: "Pending"}
	//  保存 pod 元信息
	if err := store.SavePodSpec(pod); err != nil {
		return err
	}
	// 保存 pod 状态
	if err := store.SavePodStatus(pod.Metadata.Name, status); err != nil {
		return err
	}

	// 准备好 infra 容器配置
	infraSpec := infraContainerSpec(pod.Metadata.Name)
	// ! 先启动 infra 容器
	infraID, err := s.runtime.RunContainer(runtime.RunContainerRequest{
		PodName:   pod.Metadata.Name,
		Container: infraSpec,
		Infra:     true,
	})
	if err != nil {
		status.Phase = "Failed"
		_ = store.SavePodStatus(pod.Metadata.Name, status)
		return fmt.Errorf("run infra container for pod %q: %w", pod.Metadata.Name, err)
	}
	infraInfo, err := s.runtime.InspectContainer(infraID)
	if err != nil {
		status.Phase = "Failed"
		_ = store.SavePodStatus(pod.Metadata.Name, status)
		return fmt.Errorf("inspect infra container %q for pod %q: %w", infraID, pod.Metadata.Name, err)
	}

	// ! 再准备好业务容器配置并逐个启动业务容器
	records := make([]store.ContainerRecord, 0, len(pod.Spec.Containers)+1)
	records = append(records, store.ContainerRecord{
		Name:        infraSpec.Name,
		ContainerID: infraID,
		Infra:       true,
	})
	containerStatuses := make([]corev1.ContainerStatus, 0, len(pod.Spec.Containers))
	// 遍历每一个 container spec，创建对应的容器，并收集 record 和 status 信息
	for _, containerSpec := range pod.Spec.Containers {
		containerID, err := s.runtime.RunContainer(runtime.RunContainerRequest{
			PodName:      pod.Metadata.Name,
			Container:    containerSpec,
			ShareNetWith: infraID, // 共享 netns
		})

		// 如果中途某个 container 启动失败，Pod phase 会被写成 Failed，然后直接返回错误
		if err != nil {
			status.Phase = "Failed"
			_ = store.SavePodStatus(pod.Metadata.Name, status)
			return fmt.Errorf("run container %q for pod %q: %w", containerSpec.Name, pod.Metadata.Name, err)
		}
		records = append(records, store.ContainerRecord{
			Name:        containerSpec.Name,
			ContainerID: containerID,
		})
		containerStatuses = append(containerStatuses, corev1.ContainerStatus{
			Name:        containerSpec.Name,
			ContainerID: containerID,
			Phase:       "Running",
		})
	}

	status.Phase = "Running"
	status.PodIP = infraInfo.IP
	status.InfraContainerID = infraID
	status.ContainerStatuses = containerStatuses
	if err := store.SaveContainerRecords(pod.Metadata.Name, records); err != nil {
		return err
	}
	if err := store.SavePodStatus(pod.Metadata.Name, status); err != nil {
		return err
	}
	return nil
}

// 读 json 元信息，返回完整 Pod 结构体
func (s *Service) Get(name string) (corev1.Pod, error) {
	pod, err := store.LoadPodSpec(name)
	if err != nil {
		return corev1.Pod{}, err
	}
	status, err := store.LoadPodStatus(name)
	if err != nil {
		return corev1.Pod{}, err
	}
	pod.Status = status
	return pod, nil
}

// 删除 Pod 中的每一个容器，最后删除 Pod 的有关信息
func (s *Service) Delete(name string) error {
	records, err := store.LoadContainerRecords(name)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var infraRecords []store.ContainerRecord
	// 删业务容器 records
	for _, record := range records {
		if record.Infra {
			infraRecords = append(infraRecords, record)
			continue
		}
		if err := s.runtime.StopContainer(record.ContainerID); err != nil {
			return fmt.Errorf("stop container %q for pod %q: %w", record.ContainerID, name, err)
		}
		if err := s.runtime.RemoveContainer(record.ContainerID); err != nil {
			return fmt.Errorf("remove container %q for pod %q: %w", record.ContainerID, name, err)
		}
	}
	// 删 infra 容器 records
	for _, record := range infraRecords {
		if err := s.runtime.StopContainer(record.ContainerID); err != nil {
			return fmt.Errorf("stop infra container %q for pod %q: %w", record.ContainerID, name, err)
		}
		if err := s.runtime.RemoveContainer(record.ContainerID); err != nil {
			return fmt.Errorf("remove infra container %q for pod %q: %w", record.ContainerID, name, err)
		}
	}
	return store.RemovePod(name)
}

// 列出 pod 有关信息
func (s *Service) List() ([]corev1.Pod, error) {
	names, err := store.ListPods()
	if err != nil {
		return nil, err
	}
	out := make([]corev1.Pod, 0, len(names))
	for _, name := range names {
		pod, err := s.Get(name)
		if err != nil {
			return nil, err
		}
		out = append(out, pod)
	}
	return out, nil
}

// 检查 pod 配置文件
func validatePod(pod corev1.Pod) error {
	if pod.Kind != "" && pod.Kind != "Pod" {
		return fmt.Errorf("unsupported kind %q", pod.Kind)
	}
	if pod.Metadata.Name == "" {
		return fmt.Errorf("pod metadata.name is required")
	}
	if len(pod.Spec.Containers) == 0 {
		return fmt.Errorf("pod spec.containers is required")
	}
	for _, containerSpec := range pod.Spec.Containers {
		if containerSpec.Name == "" {
			return fmt.Errorf("container name is required")
		}
		if containerSpec.Image == "" {
			return fmt.Errorf("container image is required")
		}
		for _, volume := range containerSpec.Volumes {
			if volume.Name == "" || volume.MountPath == "" {
				return fmt.Errorf("volume name and mountPath are required")
			}
		}
	}
	return nil
}

// 自动生成 infra 容器 配置信息（理所当然不读 yaml，目前统一采用 busybox 中的 sleep 实现）
func infraContainerSpec(podName string) corev1.ContainerSpec {
	return corev1.ContainerSpec{
		Name:  podName + "-infra",
		Image: "busybox",
		Command: []string{
			"/bin/busybox",
			"sleep",
		},
		Args: []string{"3600"},
	}
}
