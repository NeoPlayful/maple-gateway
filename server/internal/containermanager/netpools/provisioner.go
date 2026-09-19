package netpools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"go.uber.org/zap"
)

// AgentCaller 向落点节点下发一次 Agent 动作并等待结果。
type AgentCaller interface {
	Call(ctx context.Context, action string, params any) (json.RawMessage, error)
}

// NodeResolver 按节点 ID 定位可下发的节点（离线或不存在返回错误）。
type NodeResolver interface {
	// Node 返回指定节点 ID 对应的下发通道。
	Node(nodeID string) (AgentCaller, error)
}

// Provisioner 把 IPAM 分配与 Docker 网络创建串起来，实现应用网络的就绪与释放。
type Provisioner struct {
	alloc *Allocator
	nodes NodeResolver
	log   *zap.Logger
}

// NewProvisioner 构造。nodes 为节点定位器（通常包一层 agentregistry.Registry）。
func NewProvisioner(alloc *Allocator, nodes NodeResolver, log *zap.Logger) *Provisioner {
	return &Provisioner{alloc: alloc, nodes: nodes, log: log}
}

// Ensure 确保应用网络就绪：分配子网（幂等）→ 通知 Agent 幂等创建 Docker 网络 →
// 记录状态 active。返回网络名供 Compose 接入（文档 §47/§49）。
func (p *Provisioner) Ensure(ctx context.Context, nodeID, appID string) (string, error) {
	networkName := "maple-" + pkg.ShortID(appID)

	rec, err := p.alloc.EnsureSubnet(ctx, nodeID, appID, networkName)
	if err != nil {
		return "", err
	}

	caller, err := p.nodes.Node(nodeID)
	if err != nil {
		// 网络未建成功即标记 reserved（保留子网，等下次重试），不释放（文档 §51）。
		_ = p.alloc.nets.SetStatus(ctx, appID, NetReserved, func(n *ProjectNetwork) { n.LastError = err.Error() })
		return "", err
	}
	params := agentprotocol.NetworkCreateParams{
		NetworkName: networkName, Driver: "bridge", Subnet: rec.Subnet, Gateway: rec.Gateway,
	}
	raw, err := caller.Call(ctx, agentprotocol.ActionNetworkCreate, params)
	if err != nil {
		_ = p.alloc.nets.SetStatus(ctx, appID, NetReserved, func(n *ProjectNetwork) { n.LastError = err.Error() })
		return "", fmt.Errorf("创建项目网络失败: %w", err)
	}
	var res agentprotocol.NetworkCreateResult
	_ = json.Unmarshal(raw, &res)

	now := nowFn()
	if err := p.alloc.nets.SetStatus(ctx, appID, NetActive, func(n *ProjectNetwork) {
		n.AllocatedAt = now
		n.LastError = ""
	}); err != nil {
		return "", err
	}
	p.log.Info("project network ready",
		zap.String("app", appID), zap.String("network", networkName), zap.String("subnet", rec.Subnet))
	return networkName, nil
}

// Release 释放应用网络：通知 Agent 删除 Docker 网络，随后置 released 并写冷却期。
// 网络删除失败则标记 failed 且不释放子网（文档 §52）。
func (p *Provisioner) Release(ctx context.Context, appID string) error {
	rec, ok := p.alloc.nets.GetByProject(appID)
	if !ok {
		return nil
	}
	caller, err := p.nodes.Node(rec.NodeID)
	if err != nil {
		return err
	}
	if _, err := caller.Call(ctx, agentprotocol.ActionNetworkDelete,
		agentprotocol.NetworkRefParams{NetworkName: rec.DockerNetworkName}); err != nil {
		_ = p.alloc.nets.SetStatus(ctx, appID, NetFailed, func(n *ProjectNetwork) { n.LastError = err.Error() })
		return fmt.Errorf("删除项目网络失败: %w", err)
	}
	pool, _ := p.alloc.pools.Get(rec.PoolID)
	return p.alloc.Release(ctx, appID, pool.ReuseDelaySeconds)
}
