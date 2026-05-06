package container

import "covet/internal/cgroups"

type Config struct {
	Command   []string
	RootFS    string
	Detach    bool
	Resources cgroups.ResourceConfig
}
