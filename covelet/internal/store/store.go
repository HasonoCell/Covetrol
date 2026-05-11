package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	corev1 "covetrol/pkg/apis/core/v1"
)

// Pod 本地状态存储，结构是：
//  .covetrol/pods/<podName>/
//     spec.json
//     status.json
//     containers.json

const baseDir = ".covetrol/pods"

type ContainerRecord struct {
	Name        string `json:"name"`
	ContainerID string `json:"container_id"`
	Infra       bool   `json:"infra,omitempty"`
}

func PodDir(name string) string {
	return filepath.Join(baseDir, name)
}

func SpecPath(name string) string {
	return filepath.Join(PodDir(name), "spec.json")
}

func StatusPath(name string) string {
	return filepath.Join(PodDir(name), "status.json")
}

func ContainersPath(name string) string {
	return filepath.Join(PodDir(name), "containers.json")
}

func SavePodSpec(pod corev1.Pod) error {
	return writeJSON(SpecPath(pod.Metadata.Name), pod)
}

func SavePodStatus(name string, status corev1.PodStatus) error {
	return writeJSON(StatusPath(name), status)
}

func SaveContainerRecords(name string, records []ContainerRecord) error {
	return writeJSON(ContainersPath(name), records)
}

func LoadPodSpec(name string) (corev1.Pod, error) {
	data, err := os.ReadFile(SpecPath(name))
	if err != nil {
		return corev1.Pod{}, fmt.Errorf("read pod spec: %w", err)
	}
	var pod corev1.Pod
	if err := json.Unmarshal(data, &pod); err != nil {
		return corev1.Pod{}, fmt.Errorf("unmarshal pod spec: %w", err)
	}
	return pod, nil
}

func LoadPodStatus(name string) (corev1.PodStatus, error) {
	data, err := os.ReadFile(StatusPath(name))
	if err != nil {
		return corev1.PodStatus{}, fmt.Errorf("read pod status: %w", err)
	}
	var status corev1.PodStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return corev1.PodStatus{}, fmt.Errorf("unmarshal pod status: %w", err)
	}
	return status, nil
}

func LoadContainerRecords(name string) ([]ContainerRecord, error) {
	data, err := os.ReadFile(ContainersPath(name))
	if err != nil {
		return nil, fmt.Errorf("read container records: %w", err)
	}
	var records []ContainerRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("unmarshal container records: %w", err)
	}
	return records, nil
}

func ListPods() ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pod dir: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func RemovePod(name string) error {
	if err := os.RemoveAll(PodDir(name)); err != nil {
		return fmt.Errorf("remove pod dir: %w", err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dir for %q: %w", path, err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %q: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}
