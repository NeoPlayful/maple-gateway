package netpools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/ipam"
)

// NodeChecker 借助 Agent 采集本机网络，判断某网段是否与 Docker 网络/主机路由/接口网段冲突。
type NodeChecker struct {
	nodes NodeResolver
}

// NewNodeChecker 构造。
func NewNodeChecker(nodes NodeResolver) *NodeChecker { return &NodeChecker{nodes: nodes} }

// Check 下发 network.check 并比对：池 CIDR 与任一已存在网段重叠即冲突。
func (c *NodeChecker) Check(ctx context.Context, nodeID string, poolCIDR ipam.CIDR) (bool, string, error) {
	caller, err := c.nodes.Node(nodeID)
	if err != nil {
		return false, "", err
	}
	raw, err := caller.Call(ctx, agentprotocol.ActionNetworkCheck, nil)
	if err != nil {
		return false, "", fmt.Errorf("网络复检失败: %w", err)
	}
	var res agentprotocol.NetworkCheckResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return false, "", fmt.Errorf("解析复检结果: %w", err)
	}
	// Docker 网络子网冲突。
	for _, n := range res.DockerNetworks {
		if n.Subnet == "" {
			continue
		}
		if other, err := ipam.Parse(n.Subnet); err == nil && poolCIDR.Overlaps(other) {
			return true, fmt.Sprintf("与 Docker 网络 %s(%s) 重叠", n.NetworkName, n.Subnet), nil
		}
	}
	// 主机路由冲突。
	for _, r := range res.HostRoutes {
		if other, err := ipam.Parse(r); err == nil && poolCIDR.Overlaps(other) {
			return true, fmt.Sprintf("与主机路由 %s 重叠", r), nil
		}
	}
	// 接口网段冲突。
	for _, r := range res.InterfaceNetworks {
		if other, err := ipam.Parse(r); err == nil && poolCIDR.Overlaps(other) {
			return true, fmt.Sprintf("与本机接口网段 %s 重叠", r), nil
		}
	}
	return false, "", nil
}
