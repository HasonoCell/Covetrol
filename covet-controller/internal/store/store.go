package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	appsv1 "covetrol/pkg/apis/apps/v1"
)

// controller 本地文件包含两类：
//	- spec.json
//    原始 ReplicaSet 规格
//	- status.json
//    计算后的状态

const baseDir = ".covetrol/controllers/replicasets"

func ReplicaSetDir(name string) string {
	return filepath.Join(baseDir, name)
}

func SpecPath(name string) string {
	return filepath.Join(ReplicaSetDir(name), "spec.json")
}

func StatusPath(name string) string {
	return filepath.Join(ReplicaSetDir(name), "status.json")
}

func SaveReplicaSet(rs appsv1.ReplicaSet) error {
	return writeJSON(SpecPath(rs.Metadata.Name), rs)
}

func SaveStatus(name string, status appsv1.ReplicaSetStatus) error {
	return writeJSON(StatusPath(name), status)
}

func LoadReplicaSet(name string) (appsv1.ReplicaSet, error) {
	data, err := os.ReadFile(SpecPath(name))
	if err != nil {
		return appsv1.ReplicaSet{}, fmt.Errorf("read replica set spec: %w", err)
	}
	var rs appsv1.ReplicaSet
	if err := json.Unmarshal(data, &rs); err != nil {
		return appsv1.ReplicaSet{}, fmt.Errorf("unmarshal replica set spec: %w", err)
	}
	return rs, nil
}

func LoadStatus(name string) (appsv1.ReplicaSetStatus, error) {
	data, err := os.ReadFile(StatusPath(name))
	if err != nil {
		return appsv1.ReplicaSetStatus{}, fmt.Errorf("read replica set status: %w", err)
	}
	var status appsv1.ReplicaSetStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return appsv1.ReplicaSetStatus{}, fmt.Errorf("unmarshal replica set status: %w", err)
	}
	return status, nil
}

func RemoveReplicaSet(name string) error {
	if err := os.RemoveAll(ReplicaSetDir(name)); err != nil {
		return fmt.Errorf("remove replica set dir: %w", err)
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
