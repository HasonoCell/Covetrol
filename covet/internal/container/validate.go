package container

import (
	"fmt"

	"covetrol/covet/internal/rootfs"
)

func ValidateRequest(req RunRequest) error {
	if len(req.Command) == 0 {
		return fmt.Errorf("container command is required")
	}

	return rootfs.ValidateImage(req.Image)
}
