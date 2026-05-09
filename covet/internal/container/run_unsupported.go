//go:build !linux

package container

import (
	"fmt"
	"runtime"

	"covetrol/covet/internal/meta"
)

func Run(req RunRequest) (meta.Container, error) {
	_ = req
	return meta.Container{}, fmt.Errorf("covet run requires Linux namespaces; current GOOS=%s", runtime.GOOS)
}
