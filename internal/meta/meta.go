package meta

import "time"

type State string

const (
	StateRunning State = "running"
	StateStopped State = "stopped"
)

type Container struct {
	ID          string    `json:"id"`
	PID         int       `json:"pid"`
	Command     []string  `json:"command"`
	Image       string    `json:"image,omitempty"`
	RootFS      string    `json:"rootfs,omitempty"`
	MemoryLimit string    `json:"memory_limit,omitempty"`
	CPUWeight   int       `json:"cpu_weight,omitempty"`
	Status      State     `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}
