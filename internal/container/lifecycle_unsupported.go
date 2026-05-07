//go:build !linux

package container

import (
	"fmt"
	"runtime"
)

func Stop(id string) error {
	_ = id
	return fmt.Errorf("covet stop requires Linux namespaces; current GOOS=%s", runtime.GOOS)
}

func Remove(id string) error {
	_ = id
	return fmt.Errorf("covet rm requires Linux namespaces; current GOOS=%s", runtime.GOOS)
}
