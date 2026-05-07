//go:build linux && arm64

package container

import (
	"fmt"
	"syscall"
)

const sysSetns = 268

func sys_setns(fd int, nstype int) error {
	_, _, errno := syscall.RawSyscall(sysSetns, uintptr(fd), uintptr(nstype), 0)
	if errno != 0 {
		return fmt.Errorf("setns fd=%d nstype=%d: %w", fd, nstype, errno)
	}
	return nil
}
