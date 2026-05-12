package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	networkingv1 "covetrol/pkg/apis/networking/v1"
)

const baseDir = ".covetrol/proxy/services"

func ServiceDir(name string) string {
	return filepath.Join(baseDir, name)
}

func SpecPath(name string) string {
	return filepath.Join(ServiceDir(name), "spec.json")
}

func StatusPath(name string) string {
	return filepath.Join(ServiceDir(name), "status.json")
}

func SaveService(svc networkingv1.Service) error {
	return writeJSON(SpecPath(svc.Metadata.Name), svc)
}

func SaveStatus(name string, status networkingv1.ServiceStatus) error {
	return writeJSON(StatusPath(name), status)
}

func LoadService(name string) (networkingv1.Service, error) {
	data, err := os.ReadFile(SpecPath(name))
	if err != nil {
		return networkingv1.Service{}, fmt.Errorf("read service spec: %w", err)
	}
	var svc networkingv1.Service
	if err := json.Unmarshal(data, &svc); err != nil {
		return networkingv1.Service{}, fmt.Errorf("unmarshal service spec: %w", err)
	}
	return svc, nil
}

func RemoveService(name string) error {
	if err := os.RemoveAll(ServiceDir(name)); err != nil {
		return fmt.Errorf("remove service dir: %w", err)
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
