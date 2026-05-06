//go:build !linux

package container

import (
	"fmt"
	"runtime"

	"covet/internal/meta"
)

func Run(cfg Config) (meta.Container, error) {
	_ = cfg
	return meta.Container{}, fmt.Errorf("covet run requires Linux namespaces; current GOOS=%s", runtime.GOOS)
}
