package v1

type Pod struct {
	APIVersion string     `json:"apiVersion" yaml:"apiVersion"`
	Kind       string     `json:"kind" yaml:"kind"`
	Metadata   ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec       PodSpec    `json:"spec" yaml:"spec"`
	Status     PodStatus  `json:"status" yaml:"status,omitempty"`
}

type ObjectMeta struct {
	Name string `json:"name" yaml:"name"`
}

type PodSpec struct {
	Containers []ContainerSpec `json:"containers" yaml:"containers"`
}

type ContainerSpec struct {
	Name    string        `json:"name" yaml:"name"`
	Image   string        `json:"image" yaml:"image"`
	Command []string      `json:"command,omitempty" yaml:"command,omitempty"`
	Args    []string      `json:"args,omitempty" yaml:"args,omitempty"`
	Volumes []VolumeMount `json:"volumes,omitempty" yaml:"volumes,omitempty"`
}

type VolumeMount struct {
	Name      string `json:"name" yaml:"name"`
	MountPath string `json:"mountPath" yaml:"mountPath"`
	ReadOnly  bool   `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
}

type PodStatus struct {
	Phase             string            `json:"phase,omitempty" yaml:"phase,omitempty"`
	PodIP             string            `json:"podIP,omitempty" yaml:"podIP,omitempty"`
	InfraContainerID  string            `json:"infraContainerID,omitempty" yaml:"infraContainerID,omitempty"`
	ContainerStatuses []ContainerStatus `json:"containerStatuses,omitempty" yaml:"containerStatuses,omitempty"`
}

type ContainerStatus struct {
	Name        string `json:"name" yaml:"name"`
	ContainerID string `json:"containerID,omitempty" yaml:"containerID,omitempty"`
	Phase       string `json:"phase,omitempty" yaml:"phase,omitempty"`
}
