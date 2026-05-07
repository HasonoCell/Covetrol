package container

import "covet/internal/cgroups"

type Config struct {
	Command      []string
	Image        string
	MergedRootFS string
	Detach       bool
	Resources    cgroups.ResourceConfig
}
