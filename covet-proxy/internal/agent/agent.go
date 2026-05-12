package agent

import corev1 "covetrol/pkg/apis/core/v1"

type NodeAgent interface {
	ListPods() ([]corev1.Pod, error)
}
