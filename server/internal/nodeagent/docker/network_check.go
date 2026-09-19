package docker

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sort"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
)

// NetworkCheck 采集本机网络占用情况，供 CM 做池冲突检测（文档 §35/§38/§96）：
// Docker 现有网络（含非受管，仅参与冲突检测）、主要接口网段（含 VPN/overlay）、
// 本机路由网段（平台相关，见 hostRoutes）。
func (c *Client) NetworkCheck(ctx context.Context) (NetworkCheck, error) {
	out := NetworkCheck{}
	// Docker 全部网络（不加受管过滤：第三方网络同样参与冲突检测）。
	raw, err := c.cli.NetworkList(ctx, network.ListOptions{Filters: filters.NewArgs()})
	if err != nil {
		return out, fmt.Errorf("列出 Docker 网络: %w", err)
	}
	for _, n := range raw {
		info := NetworkInfo{ID: n.ID, Name: n.Name, Driver: n.Driver,
			Managed: n.Labels[ManagedNetworkLabel] == "true"}
		if len(n.IPAM.Config) > 0 {
			info.Subnet = n.IPAM.Config[0].Subnet
			info.Gateway = n.IPAM.Config[0].Gateway
		}
		out.DockerNetworks = append(out.DockerNetworks, info)
	}
	sort.Slice(out.DockerNetworks, func(i, j int) bool { return out.DockerNetworks[i].Name < out.DockerNetworks[j].Name })

	// 主要接口网段：从本机地址表取 IPv4 私有/链路网段。
	out.InterfaceNetworks = interfaceCIDRs()
	out.HostRoutes = hostRoutes()
	return out, nil
}

// NetworkCheck 汇总网络占用情况。
type NetworkCheck struct {
	DockerNetworks    []NetworkInfo `json:"docker_networks"`
	InterfaceNetworks []string      `json:"interface_networks"`
	HostRoutes        []string      `json:"host_routes"`
}

// interfaceCIDRs 返回本机各接口的 IPv4 网段（跳过回环与未指定地址）。
func interfaceCIDRs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		p, err := netip.ParsePrefix(ipnet.String())
		if err != nil || !p.Addr().Is4() || p.Addr().IsLoopback() {
			continue
		}
		masked := p.Masked().String()
		if seen[masked] {
			continue
		}
		seen[masked] = true
		out = append(out, masked)
	}
	sort.Strings(out)
	return out
}
