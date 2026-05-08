//go:build linux

package network

import (
	"fmt"
	"net"
	"runtime"

	"github.com/vishvananda/netlink"
)

const (
	defaultBridgeName = "covet0" // 默认宿主机上的 linux bridge 名称
	defaultBridgeCIDR = "10.200.0.1/24"
	defaultGuestVeth  = "eth0"
)

func NewConfig(containerID string) (Config, error) {
	ip, cidr, err := allocateContainerAddress(containerID)
	if err != nil {
		return Config{}, err
	}

	return Config{
		BridgeName:    defaultBridgeName,
		BridgeCIDR:    defaultBridgeCIDR,
		BridgeIP:      "10.200.0.1",
		ContainerCIDR: cidr,
		ContainerIP:   ip,
		HostVethName:  "v" + containerID[:11],
		PeerVethName:  "c" + containerID[:11], // peer 这里先给一个临时名字，host 和 peer 都是父进程侧用到的东西
		GuestVethName: defaultGuestVeth,       // guest 是容器内子进程用到的东西
	}, nil
}

// 在宿主机侧完成 bridge + veth + 把 peer 移进容器 netns
func SetupHost(cfg Config, pid int) error {
	if err := ensureBridge(cfg); err != nil {
		return err
	}

	// 准备宿主机上的 bridge
	bridge, err := netlink.LinkByName(cfg.BridgeName)
	if err != nil {
		return fmt.Errorf("find bridge %q: %w", cfg.BridgeName, err)
	}
	if existing, err := netlink.LinkByName(cfg.HostVethName); err == nil {
		_ = netlink.LinkDel(existing)
	}

	// 创建 veth pair
	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:        cfg.HostVethName,     // host 端
			MasterIndex: bridge.Attrs().Index, // 创建出来就将它插到宿主机 bridge 上
		},
		PeerName: cfg.PeerVethName, // peer 端
	}
	if err := netlink.LinkAdd(veth); err != nil {
		return fmt.Errorf("create veth pair %q<->%q: %w", cfg.HostVethName, cfg.PeerVethName, err)
	}

	hostVeth, err := netlink.LinkByName(cfg.HostVethName)
	if err != nil {
		return fmt.Errorf("find host veth %q: %w", cfg.HostVethName, err)
	}
	// 前面只创建了 veth，LinkSetUp 真正把 host veth 这一端拉起来
	if err := netlink.LinkSetUp(hostVeth); err != nil {
		return fmt.Errorf("set host veth %q up: %w", cfg.HostVethName, err)
	}

	guestVeth, err := netlink.LinkByName(cfg.PeerVethName)
	if err != nil {
		return fmt.Errorf("find peer veth %q on host: %w", cfg.PeerVethName, err)
	}
	// 把 peer 这一端移动到容器主进程所在的 net namespace 里
	if err := netlink.LinkSetNsPid(guestVeth, pid); err != nil {
		return fmt.Errorf("move peer veth %q to pid %d netns: %w", cfg.PeerVethName, pid, err)
	}
	return nil
}

// 在容器侧进行网络配置
func SetupContainer(cfg Config) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// 先找到 peer 接口
	link, err := netlink.LinkByName(cfg.PeerVethName)
	if err != nil {
		return fmt.Errorf("find peer veth %q in container netns: %w", cfg.PeerVethName, err)
	}
	// 如果临时 peer 名字不等于最终 guest 名字
	if cfg.PeerVethName != cfg.GuestVethName {
		// 先关闭 peer 接口
		if err := netlink.LinkSetDown(link); err != nil {
			return fmt.Errorf("set peer veth %q down before rename: %w", cfg.PeerVethName, err)
		}
		// 再更改临时 peer 的名字
		if err := netlink.LinkSetName(link, cfg.GuestVethName); err != nil {
			return fmt.Errorf("rename peer veth %q to %q: %w", cfg.PeerVethName, cfg.GuestVethName, err)
		}
		// 然后找到 guest 接口
		link, err = netlink.LinkByName(cfg.GuestVethName)
		if err != nil {
			return fmt.Errorf("find guest veth %q after rename: %w", cfg.GuestVethName, err)
		}
	}

	// 把 guest 接口拉起来
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("set guest veth %q up: %w", cfg.GuestVethName, err)
	}

	// 将 lookback 回换地址也拉起来，保证容器本地网络环境完整
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("find loopback: %w", err)
	}
	if err := netlink.LinkSetUp(lo); err != nil {
		return fmt.Errorf("set loopback up: %w", err)
	}

	// 给 guest 侧配好 ip 地址
	addr, err := netlink.ParseAddr(cfg.ContainerCIDR)
	if err != nil {
		return fmt.Errorf("parse container cidr %q: %w", cfg.ContainerCIDR, err)
	}
	if err := netlink.AddrAdd(link, addr); err != nil && !isAddrExists(err) {
		return fmt.Errorf("assign %q to %q: %w", cfg.ContainerCIDR, cfg.GuestVethName, err)
	}

	// 准备好路由规则
	if err := ensureContainerRoute(link, cfg.BridgeCIDR); err != nil {
		return err
	}
	return nil
}

// 删除 veth pair
func Teardown(cfg Config) error {
	if cfg.HostVethName == "" {
		return nil
	}
	link, err := netlink.LinkByName(cfg.HostVethName)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		return fmt.Errorf("find host veth %q: %w", cfg.HostVethName, err)
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete host veth %q: %w", cfg.HostVethName, err)
	}
	return nil
}

// 确保宿主机上的一个 linux bridge 存在
func ensureBridge(cfg Config) error {
	// 如果已经存在，补齐 bridge 地址，然后拉起 bridge
	if link, err := netlink.LinkByName(cfg.BridgeName); err == nil {
		if err := ensureBridgeAddress(link, cfg.BridgeCIDR); err != nil {
			return err
		}
		if err := netlink.LinkSetUp(link); err != nil {
			return fmt.Errorf("set bridge %q up: %w", cfg.BridgeName, err)
		}
		return nil
	}

	// 如果不存在，创建 bridge 再拉起
	bridge := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{Name: cfg.BridgeName},
	}
	if err := netlink.LinkAdd(bridge); err != nil {
		return fmt.Errorf("create bridge %q: %w", cfg.BridgeName, err)
	}
	if err := ensureBridgeAddress(bridge, cfg.BridgeCIDR); err != nil {
		return err
	}
	if err := netlink.LinkSetUp(bridge); err != nil {
		return fmt.Errorf("set bridge %q up: %w", cfg.BridgeName, err)
	}
	return nil
}

// 为一个 bridge 创建 ip 地址
func ensureBridgeAddress(link netlink.Link, cidr string) error {
	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("parse bridge cidr %q: %w", cidr, err)
	}
	if err := netlink.AddrAdd(link, addr); err != nil && !isAddrExists(err) {
		return fmt.Errorf("assign %q to bridge %q: %w", cidr, link.Attrs().Name, err)
	}
	return nil
}

// 配好路由规则，确保容器 A 能直接通过 brige 和容器 B 沟通
func ensureContainerRoute(link netlink.Link, bridgeCIDR string) error {
	_, subnet, err := net.ParseCIDR(bridgeCIDR)
	if err != nil {
		return fmt.Errorf("parse bridge cidr %q for container route: %w", bridgeCIDR, err)
	}
	route := netlink.Route{
		LinkIndex: link.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst:       subnet,
	}
	if err := netlink.RouteAdd(&route); err != nil && !isRouteExists(err) {
		return fmt.Errorf("add route %q via %q: %w", subnet.String(), link.Attrs().Name, err)
	}
	return nil
}

// 根据容器 ID 分配一个容器 IP 地址
func allocateContainerAddress(containerID string) (string, string, error) {
	if len(containerID) < 2 {
		return "", "", fmt.Errorf("container id %q is too short for network allocation", containerID)
	}
	var value int
	// 把 containerID 每个字符转成字节值
	for i := 0; i < len(containerID); i++ {
		value += int(containerID[i])
	}
	hostOctet := 2 + (value % 250)
	ip := fmt.Sprintf("10.200.0.%d", hostOctet)
	// 返回 ip 和 cidr；cidr 就是 <ip>/24
	return ip, fmt.Sprintf("%s/24", ip), nil
}

func isAddrExists(err error) bool {
	return err != nil && err.Error() == "file exists"
}

func isRouteExists(err error) bool {
	return err != nil && err.Error() == "file exists"
}
