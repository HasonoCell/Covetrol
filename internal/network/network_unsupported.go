//go:build !linux

package network

import (
	"fmt"
	"runtime"
)

func NewConfig(containerID string) (Config, error) {
	_ = containerID
	return Config{}, fmt.Errorf("container networking requires Linux; current GOOS=%s", runtime.GOOS)
}

func SetupHost(cfg Config, pid int) error {
	_ = cfg
	_ = pid
	return fmt.Errorf("container networking requires Linux; current GOOS=%s", runtime.GOOS)
}

func SetupContainer(cfg Config) error {
	_ = cfg
	return fmt.Errorf("container networking requires Linux; current GOOS=%s", runtime.GOOS)
}

func Teardown(cfg Config) error {
	_ = cfg
	return nil
}
