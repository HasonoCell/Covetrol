//go:build !linux

package mount

import (
	"fmt"
	"runtime"
)

func Apply(rootfs string, mounts []Mount) error {
	_ = rootfs
	if len(mounts) == 0 {
		return nil
	}
	return fmt.Errorf("bind mounts require Linux mount namespaces; current GOOS=%s", runtime.GOOS)
}
