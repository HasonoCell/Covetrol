package v1

import corev1 "covetrol/pkg/apis/core/v1"

type ReplicaSet struct {
	APIVersion string            `json:"apiVersion" yaml:"apiVersion"`
	Kind       string            `json:"kind" yaml:"kind"`
	Metadata   corev1.ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec       ReplicaSetSpec    `json:"spec" yaml:"spec"`
	Status     ReplicaSetStatus  `json:"status" yaml:"status,omitempty"`
}

type ReplicaSetSpec struct {
	Replicas int               `json:"replicas" yaml:"replicas"`
	Selector map[string]string `json:"selector" yaml:"selector"`
	Template corev1.Pod        `json:"template" yaml:"template"`
}

type ReplicaSetStatus struct {
	DesiredReplicas int      `json:"desiredReplicas,omitempty" yaml:"desiredReplicas,omitempty"` // 预期 Pod 数量
	ReadyReplicas   int      `json:"readyReplicas,omitempty" yaml:"readyReplicas,omitempty"`     // 实际 Pod 数量
	PodNames        []string `json:"podNames,omitempty" yaml:"podNames,omitempty"`               // 实际 Pods 的名称
}
