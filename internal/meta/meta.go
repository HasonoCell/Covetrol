package meta

import (
	"time"

	"covet/internal/mount"
)

type State string

const (
	StateRunning State = "running"
	StateStopped State = "stopped"
)

type Container struct {
	ID          string        `json:"id"`
	PID         int           `json:"pid"`
	Command     []string      `json:"command"`
	Image       string        `json:"image,omitempty"`
	RootFS      string        `json:"rootfs,omitempty"`
	Mounts      []mount.Mount `json:"mounts,omitempty"`
	IPAddress   string        `json:"ip_address,omitempty"`
	Bridge      string        `json:"bridge,omitempty"`
	HostVeth    string        `json:"host_veth,omitempty"`
	GuestVeth   string        `json:"guest_veth,omitempty"`
	PeerVeth    string        `json:"peer_veth,omitempty"`
	MemoryLimit string        `json:"memory_limit,omitempty"`
	CPUWeight   int           `json:"cpu_weight,omitempty"`
	Status      State         `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
}
