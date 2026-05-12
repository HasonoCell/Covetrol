package v1

import corev1 "covetrol/pkg/apis/core/v1"

type Service struct {
	APIVersion string            `json:"apiVersion" yaml:"apiVersion"`
	Kind       string            `json:"kind" yaml:"kind"`
	Metadata   corev1.ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec       ServiceSpec       `json:"spec" yaml:"spec"`
	Status     ServiceStatus     `json:"status" yaml:"status,omitempty"`
}

type ServiceSpec struct {
	Selector   map[string]string `json:"selector" yaml:"selector"`
	Port       int               `json:"port" yaml:"port"`             // 接受请求的 Port
	TargetPort int               `json:"targetPort" yaml:"targetPort"` // 收到请求后转发过去的 Port
}

type ServiceStatus struct {
	Endpoints []string `json:"endpoints,omitempty" yaml:"endpoints,omitempty"`
	Listening bool     `json:"listening,omitempty" yaml:"listening,omitempty"`
}
