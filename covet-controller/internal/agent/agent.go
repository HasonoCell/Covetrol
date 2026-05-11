package agent

import corev1 "covetrol/pkg/apis/core/v1"

// 通过 NodeAgent 接口实现与 covelet 的解耦
type NodeAgent interface {
	ApplyPod(pod corev1.Pod) error
	GetPod(name string) (corev1.Pod, error)
	ListPods() ([]corev1.Pod, error)
	DeletePod(name string) error
}
