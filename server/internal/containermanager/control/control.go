// Package control 提供人工触发的实例操作（start/stop/restart、查看日志）。
//
// 与对账器的自动决策区分：对账器按期望副本数维持容器数；本包响应管理端的一次性人工指令。
// 手动 stop 会登记为"人工维护"（desired.Store.Pause），使对账器把它计入实际副本数，
// 避免管理员停掉的实例被下一轮对账立即重建；手动 start 则解除该标记，交回对账器接管。
package control

import (
	"context"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/observer"
	"go.uber.org/zap"
)

// ActualReader 提供最近一轮观测快照（由 observer.Observer 实现），用于定位实例所在节点。
type ActualReader interface {
	Snapshot() map[string]observer.ObservedContainer
}

// Controller 执行人工实例操作。
type Controller struct {
	registry *agentregistry.Registry
	actual   ActualReader
	store    *desired.Store
	logger   *zap.Logger
}

// New 构造。
func New(registry *agentregistry.Registry, actual ActualReader, store *desired.Store, logger *zap.Logger) *Controller {
	return &Controller{registry: registry, actual: actual, store: store, logger: logger}
}

// locate 依据最近观测快照找到实例所在节点。
func (c *Controller) locate(instanceID string) (*agentregistry.Node, observer.ObservedContainer, error) {
	oc, ok := c.actual.Snapshot()[instanceID]
	if !ok {
		return nil, observer.ObservedContainer{}, fmt.Errorf("实例 %s 不在最近观测范围内", instanceID)
	}
	node, ok := c.registry.Get(oc.NodeName)
	if !ok {
		return nil, observer.ObservedContainer{}, fmt.Errorf("实例所在节点 %s 不可用", oc.NodeName)
	}
	return node, oc, nil
}

// Restart 重启实例容器（不影响期望态，对账器无需干预）。
func (c *Controller) Restart(ctx context.Context, instanceID string) error {
	node, _, err := c.locate(instanceID)
	if err != nil {
		return err
	}
	if err := node.Agent.Restart(ctx, instanceID); err != nil {
		return err
	}
	c.store.Resume(instanceID)
	c.logger.Info("instance restarted by operator", zap.String("instance_id", instanceID), zap.String("node", node.Name))
	return nil
}

// Stop 停止实例容器并登记为人工维护，避免被对账器立即重建。
func (c *Controller) Stop(ctx context.Context, instanceID string) error {
	node, oc, err := c.locate(instanceID)
	if err != nil {
		return err
	}
	if err := node.Agent.Stop(ctx, instanceID); err != nil {
		return err
	}
	c.store.Pause(instanceID, oc.Container.Labels["maple.version_id"])
	c.logger.Info("instance stopped by operator (paused)",
		zap.String("instance_id", instanceID), zap.String("node", node.Name))
	return nil
}

// Start 启动实例容器并解除人工维护标记，交回对账器接管。
func (c *Controller) Start(ctx context.Context, instanceID string) error {
	node, _, err := c.locate(instanceID)
	if err != nil {
		return err
	}
	if err := node.Agent.Start(ctx, instanceID); err != nil {
		return err
	}
	c.store.Resume(instanceID)
	c.logger.Info("instance started by operator", zap.String("instance_id", instanceID), zap.String("node", node.Name))
	return nil
}

// Logs 读取实例容器最近 tail 行日志。
func (c *Controller) Logs(ctx context.Context, instanceID string, tail int) (string, error) {
	node, _, err := c.locate(instanceID)
	if err != nil {
		return "", err
	}
	return node.Agent.Logs(ctx, instanceID, tail)
}
