//go:build !linux

package cgroups

type ResourceConfig struct {
	MemoryLimit string
	CPUWeight   int
}

func ParseMemoryLimit(value string) (string, error) {
	return value, nil
}
