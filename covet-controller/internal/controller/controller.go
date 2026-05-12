package controller

import (
	"maps"
	"fmt"
	"slices"
	"sort"
	"strings"

	"covetrol/covet-controller/internal/agent"
	"covetrol/covet-controller/internal/store"
	appsv1 "covetrol/pkg/apis/apps/v1"
	corev1 "covetrol/pkg/apis/core/v1"
)

type Controller struct {
	agent agent.NodeAgent
}

func New(agent agent.NodeAgent) *Controller {
	return &Controller{agent: agent}
}

// 检验参数并保存 spec.json，然后马上触发一次 reconcile
func (c *Controller) Apply(rs appsv1.ReplicaSet) error {
	if err := validateReplicaSet(rs); err != nil {
		return err
	}
	if err := store.SaveReplicaSet(rs); err != nil {
		return err
	}
	// 立马协调一次，所以每次 covelet apply <yaml> 后都会 controller 都会立刻生效
	_, err := c.Reconcile(rs.Metadata.Name)
	return err
}

// 读 spec.json 和 status.json，返回 ReplicaSet 结构体
func (c *Controller) Get(name string) (appsv1.ReplicaSet, error) {
	rs, err := store.LoadReplicaSet(name)
	if err != nil {
		return appsv1.ReplicaSet{}, err
	}
	status, err := store.LoadStatus(name)
	if err != nil {
		return appsv1.ReplicaSet{}, err
	}
	rs.Status = status
	return rs, nil
}

// 删除该 ReplicaSet 中的所有 Pod，再删除该 Controller 的本地信息
func (c *Controller) Delete(name string) error {
	rs, err := store.LoadReplicaSet(name)
	if err != nil {
		return err
	}
	actualPods, err := c.agent.ListPods()
	if err != nil {
		return err
	}
	for _, pod := range actualPods {
		if !matchesReplicaSet(rs, pod) {
			continue
		}
		_ = c.agent.DeletePod(pod.Metadata.Name)
	}
	return store.RemoveReplicaSet(name)
}

// ! 关键的协调函数
func (c *Controller) Reconcile(name string) (appsv1.ReplicaSetStatus, error) {
	rs, err := store.LoadReplicaSet(name) // 加载配置结构体
	if err != nil {
		return appsv1.ReplicaSetStatus{}, err
	}
	desiredNames := desiredPodNames(rs)   // 得到期望 Pod names
	actualPods, err := c.agent.ListPods() // 得到当前运行着的 Pods 信息
	if err != nil {
		return appsv1.ReplicaSetStatus{}, err
	}

	// 创建一个运行中的 Pods 的 name -> pod struct 的映射
	actualByName := make(map[string]corev1.Pod, len(actualPods))
	for _, pod := range actualPods {
		if !matchesReplicaSet(rs, pod) {
			continue
		}
		actualByName[pod.Metadata.Name] = pod
	}

	// 	遍历 desiredNames，如果某个期望 Pod 还不存在于运行中 Pods
	//  就那找配置创建出一个具体 pod spec 并应用
	for _, podName := range desiredNames {
		if _, ok := actualByName[podName]; ok {
			continue
		}
		// template 对应着 yaml 文件中的 pod spec
		podSpec := rs.Spec.Template
		podSpec.APIVersion = "covetrol/v1"
		podSpec.Kind = "Pod"
		podSpec.Metadata.Name = podName
		podSpec.Metadata.Labels = mergeLabels(rs.Spec.Template.Metadata.Labels, rs.Spec.Selector)
		// 应用配置创建 Pod
		if err := c.agent.ApplyPod(podSpec); err != nil {
			return appsv1.ReplicaSetStatus{}, fmt.Errorf("apply pod %q for replica set %q: %w", podName, name, err)
		}
	}

	// 再遍历运行中的 Pods，如果一个 Pod 不在 desiredNames 里就删除多余的 Pod
	// 这就是缩容，主要是针对一份配置文件的不同版本作调整，确保运行 Pod 和 desires 状态的一致性
	for actualName := range actualByName {
		if !slices.Contains(desiredNames, actualName) {
			if err := c.agent.DeletePod(actualName); err != nil {
				return appsv1.ReplicaSetStatus{}, fmt.Errorf("delete pod %q for replica set %q: %w", actualName, name, err)
			}
		}
	}

	// reconcile 主要步骤结束后重新统计状态
	refreshedPods, err := c.agent.ListPods()
	if err != nil {
		return appsv1.ReplicaSetStatus{}, err
	}

	status := appsv1.ReplicaSetStatus{
		DesiredReplicas: rs.Spec.Replicas,
		PodNames:        make([]string, 0, len(desiredNames)),
	}
	// 检查此时 pod 状态是否和预期一致，准备写入 status.json
	for _, pod := range refreshedPods {
		if !matchesReplicaSet(rs, pod) || !slices.Contains(desiredNames, pod.Metadata.Name) {
			continue
		}
		status.PodNames = append(status.PodNames, pod.Metadata.Name)
		if strings.EqualFold(pod.Status.Phase, "Running") {
			status.ReadyReplicas++ // 统计运行中 Pod 数量
		}
	}
	sort.Strings(status.PodNames)
	if err := store.SaveStatus(name, status); err != nil {
		return appsv1.ReplicaSetStatus{}, err
	}
	return status, nil
}

func validateReplicaSet(rs appsv1.ReplicaSet) error {
	if rs.Kind != "" && rs.Kind != "ReplicaSet" {
		return fmt.Errorf("unsupported kind %q", rs.Kind)
	}
	if rs.Metadata.Name == "" {
		return fmt.Errorf("replica set metadata.name is required")
	}
	if rs.Spec.Replicas < 0 {
		return fmt.Errorf("replica set replicas must be >= 0")
	}
	if len(rs.Spec.Selector) == 0 {
		return fmt.Errorf("replica set spec.selector is required")
	}
	if len(rs.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("replica set template.spec.containers is required")
	}
	return nil
}

// 生成期望 Pods 的名字
func desiredPodNames(rs appsv1.ReplicaSet) []string {
	// 实现比较简单，比如 busybox-rs + replicas=2 就是 busybox-rs-0，busybox-rs-1
	out := make([]string, 0, rs.Spec.Replicas)
	for i := 0; i < rs.Spec.Replicas; i++ {
		out = append(out, fmt.Sprintf("%s-%d", rs.Metadata.Name, i))
	}
	return out
}

func matchesReplicaSet(rs appsv1.ReplicaSet, pod corev1.Pod) bool {
	for key, value := range rs.Spec.Selector {
		if pod.Metadata.Labels == nil || pod.Metadata.Labels[key] != value {
			return false
		}
	}
	return true
}

func mergeLabels(base, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(extra))
	maps.Copy(out, base)
	maps.Copy(out, extra)
	return out
}
