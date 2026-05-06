//go:build !linux

package container

import (
	"fmt"
	"runtime"
)

func Run(cfg Config) error {
	_ = cfg
	return fmt.Errorf("covet run requires Linux namespaces; current GOOS=%s", runtime.GOOS)
}
