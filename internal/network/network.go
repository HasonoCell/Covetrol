package network

type Config struct {
	BridgeName    string `json:"bridge_name"`
	BridgeCIDR    string `json:"bridge_cidr"`
	BridgeIP      string `json:"bridge_ip"`
	ContainerCIDR string `json:"container_cidr"`
	ContainerIP   string `json:"container_ip"`
	HostVethName  string `json:"host_veth_name"`
	GuestVethName string `json:"guest_veth_name"`
	PeerVethName  string `json:"peer_veth_name"`
}
