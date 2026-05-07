//go:build !linux

package container

import (
	"fmt"
	"runtime"
)

func Start(id string) error {
	_ = id
	return fmt.Errorf("covet start requires Linux namespaces; current GOOS=%s", runtime.GOOS)
}

func Stop(id string) error {
	_ = id
	return fmt.Errorf("covet stop requires Linux namespaces; current GOOS=%s", runtime.GOOS)
}

func Remove(id string) error {
	_ = id
	return fmt.Errorf("covet rm requires Linux namespaces; current GOOS=%s", runtime.GOOS)
}
