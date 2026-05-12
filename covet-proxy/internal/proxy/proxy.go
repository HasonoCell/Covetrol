package proxy

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"covetrol/covet-proxy/internal/agent"
	corev1 "covetrol/pkg/apis/core/v1"
	networkingv1 "covetrol/pkg/apis/networking/v1"
)

type Proxy struct {
	agent agent.NodeAgent
	next  int // 下一个处理请求的 Pod
}

func New(agent agent.NodeAgent) *Proxy {
	return &Proxy{agent: agent}
}

// 返回每个 pod 最终暴露的 endpoint
func (p *Proxy) ResolveEndpoints(svc networkingv1.Service) ([]string, error) {
	// 先得到当前节点上运行着的所有 pods 信息
	pods, err := p.agent.ListPods()
	if err != nil {
		return nil, err
	}

	endpoints := make([]string, 0)
	for _, pod := range pods {
		// 选取和 spec selector 相同的 pod
		if !matchesSelector(svc.Spec.Selector, pod) {
			continue
		}
		if !strings.EqualFold(pod.Status.Phase, "Running") {
			continue
		}
		if pod.Status.PodIP == "" {
			continue
		}
		// 将 pod 暴露的 ip 地址和 spec 中期望的 port 信息拼接为 endpoint
		endpoints = append(endpoints, net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(svc.Spec.TargetPort)))
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no ready endpoints for service %q", svc.Metadata.Name)
	}
	return endpoints, nil
}

// 启动网络服务
func (p *Proxy) Serve(svc networkingv1.Service) error {
	// 启动 listener，监听本机上的 port 节点（通过 spec 配置）
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(svc.Spec.Port)))
	if err != nil {
		return fmt.Errorf("listen for service %q on port %d: %w", svc.Metadata.Name, svc.Spec.Port, err)
	}
	defer listener.Close()

	for {
		// 如果收到请求
		conn, err := listener.Accept()
		if err != nil {
			return fmt.Errorf("accept service connection: %w", err)
		}
		endpoints, err := p.ResolveEndpoints(svc)
		if err != nil {
			_ = conn.Close()
			return err
		}

		// ! 轮询调度，挑选出来一个 pod 的 endpoint
		target := endpoints[p.next%len(endpoints)]
		p.next++
		// 让 pod 处理请求，启动 goroutine
		go p.proxyConnection(conn, target)
	}
}

func (p *Proxy) proxyConnection(src net.Conn, target string) {
	// 先与 pod endpoint 建立连接
	defer src.Close()
	dst, err := net.Dial("tcp", target)
	if err != nil {
		return
	}
	defer dst.Close()

	// 实际如何通信？现将 src 的请求数据拷贝到 dst
	// 再将 dst 处理完后的响应数据拷贝回 src
	// 但是此处的实现本质上只是 user space 中的程序对数据的转发，即 client -> covet-proxy process -> pod
	// 数据包要多走一遍 user space，会有性能损耗
	// iptables 就是通过 netfilter 帮助数据包直接在 kernel 中进行转发的（DNAT），这也是更正式的实现，这里就不做了
	go io.Copy(dst, src)
	_, _ = io.Copy(src, dst)
}

func matchesSelector(selector map[string]string, pod corev1.Pod) bool {
	for key, value := range selector {
		if pod.Metadata.Labels == nil || pod.Metadata.Labels[key] != value {
			return false
		}
	}
	return true
}
