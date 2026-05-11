package container

import (
	"fmt"

	"covetrol/covet/internal/meta"
	"covetrol/covet/internal/rootfs"
	"covetrol/covet/internal/store"
)

func ValidateRequest(req RunRequest) error {
	if len(req.Command) == 0 {
		return fmt.Errorf("container command is required")
	}

	if err := rootfs.ValidateImage(req.Image); err != nil {
		return err
	}

	if req.ShareNetWith == "" {
		return nil
	}

	containerMeta, err := store.LoadMetadata(req.ShareNetWith)
	if err != nil {
		return fmt.Errorf("load share-net-with container %q: %w", req.ShareNetWith, err)
	}
	containerMeta, err = store.RefreshMetadata(containerMeta)
	if err != nil {
		return err
	}
	if containerMeta.Status != meta.StateRunning || containerMeta.PID <= 0 {
		return fmt.Errorf("share-net-with container %q is not running", req.ShareNetWith)
	}
	return nil
}
