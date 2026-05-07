//go:build !linux

package container

import (
	"fmt"
	"runtime"
)

func Exec(containerID string, command []string) error {
	_ = containerID
	_ = command
	return fmt.Errorf("covet exec requires Linux namespaces; current GOOS=%s", runtime.GOOS)
}
