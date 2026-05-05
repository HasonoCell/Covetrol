//go:build !linux

package container

import (
	"fmt"
	"runtime"

	"covet/internal/cgroups"
)

func Run(command []string, resources cgroups.ResourceConfig) error {
	_ = command
	_ = resources
	return fmt.Errorf("covet run requires Linux namespaces; current GOOS=%s", runtime.GOOS)
}
