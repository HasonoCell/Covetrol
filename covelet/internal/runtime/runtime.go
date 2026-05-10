package runtime

import corev1 "covetrol/pkg/apis/core/v1"

type RunContainerRequest struct {
	PodName   string
	Container corev1.ContainerSpec
}

type ContainerInfo struct {
	ID     string
	Status string
	Image  string
}

type Runtime interface {
	RunContainer(req RunContainerRequest) (string, error)
	StopContainer(id string) error
	RemoveContainer(id string) error
	InspectContainer(id string) (ContainerInfo, error)
}
