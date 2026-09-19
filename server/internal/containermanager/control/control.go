// Package control 提供人工触发的实例操作（start/stop/restart、查看日志）。
//
// 与对账器的自动决策区分：对账器按期望副本数维持容器数；本包响应管理端的一次性人工指令。
// 手动 stop 会登记为"人工维护"（desired.Store.Pause），使对账器把它计入实际副本数，
// 避免管理员停掉的实例被下一轮对账立即重建；手动 start 则解除该标记，交回对账器接管。
package control

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/desired"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/gwclient"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/logstream"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/observer"
	"go.uber.org/zap"
)

// ActualReader 提供最近一轮观测快照（由 observer.Observer 实现），用于定位实例所在节点。
type ActualReader interface {
	Snapshot() map[string]observer.ObservedContainer
	// Containers 按容器 ID 索引的全量受管容器，供无 instance_id 的容器（如 Compose 容器）定位。
	Containers() map[string]observer.ObservedContainer
}

// Controller 执行人工实例操作。
type Controller struct {
	registry *agentregistry.Registry
	actual   ActualReader
	store    *desired.Store
	streams  *logstream.Hub
	logger   *zap.Logger
}

// New 构造。
func New(registry *agentregistry.Registry, actual ActualReader, store *desired.Store, streams *logstream.Hub, logger *zap.Logger) *Controller {
	return &Controller{registry: registry, actual: actual, store: store, streams: streams, logger: logger}
}

// locate 依据最近观测快照找到容器所在节点。标识既可为 instance_id（声明式容器），
// 也可为容器 ID（无 instance_id 的 Compose 容器）：先按 instance_id 匹配，未命中再按容器 ID。
func (c *Controller) locate(id string) (*agentregistry.Node, observer.ObservedContainer, error) {
	oc, ok := c.actual.Snapshot()[id]
	if !ok {
		oc, ok = c.actual.Containers()[id]
	}
	if !ok {
		return nil, observer.ObservedContainer{}, fmt.Errorf("容器 %s 不在最近观测范围内", id)
	}
	node, ok := c.registry.Get(oc.NodeName)
	if !ok {
		return nil, observer.ObservedContainer{}, fmt.Errorf("容器所在节点 %s 不可用", oc.NodeName)
	}
	return node, oc, nil
}

// Restart 重启实例容器（不影响期望态，对账器无需干预）。
func (c *Controller) Restart(ctx context.Context, instanceID string) error {
	node, _, err := c.locate(instanceID)
	if err != nil {
		return err
	}
	if _, err := node.Call(ctx, agentprotocol.ActionContainerRestart, agentprotocol.IDParams{ID: instanceID}); err != nil {
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
	if _, err := node.Call(ctx, agentprotocol.ActionContainerStop, agentprotocol.IDParams{ID: instanceID}); err != nil {
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
	if _, err := node.Call(ctx, agentprotocol.ActionContainerStart, agentprotocol.IDParams{ID: instanceID}); err != nil {
		return err
	}
	c.store.Resume(instanceID)
	c.logger.Info("instance started by operator", zap.String("instance_id", instanceID), zap.String("node", node.Name))
	return nil
}

// Remove 删除实例容器（强制）。依据观测快照定位节点，不强依赖 Gateway 实例表：
// 未被纳管（注册失败）的容器同样可经此删除，避免留下无法管理的孤儿容器。
// 删除是否会被对账器补回，取决于该容器是否对应一份期望态——有期望态则下轮重建，
// 无期望态（如手工贴标签的容器）则就此消失。
func (c *Controller) Remove(ctx context.Context, instanceID string, force bool) error {
	node, _, err := c.locate(instanceID)
	if err != nil {
		return err
	}
	if _, err := node.Call(ctx, agentprotocol.ActionContainerRemove, agentprotocol.RemoveParams{ID: instanceID, Force: force}); err != nil {
		return err
	}
	c.store.Resume(instanceID)
	c.logger.Info("instance removed by operator",
		zap.String("instance_id", instanceID), zap.String("node", node.Name), zap.Bool("force", force))
	return nil
}

// RemoveDeployment 强制移除某部署名下的全部受管容器（按 maple.deployment_id 标签匹配）。
// 用于「删除部署/停止部署」时清理残留容器：调用前期望态应已摘除，否则对账器会把删掉的容器补回。
// 返回成功删除的容器数；单个失败仅记警告、不中断其余删除。
func (c *Controller) RemoveDeployment(ctx context.Context, deploymentID string) int {
	if deploymentID == "" {
		return 0
	}
	removed := 0
	for _, oc := range c.actual.Containers() {
		if oc.InstanceID == "" || oc.Container.Labels["maple.deployment_id"] != deploymentID {
			continue
		}
		node, ok := c.registry.Get(oc.NodeName)
		if !ok {
			continue
		}
		if _, err := node.Call(ctx, agentprotocol.ActionContainerRemove, agentprotocol.RemoveParams{ID: oc.InstanceID, Force: true}); err != nil {
			c.logger.Warn("remove deployment container failed",
				zap.String("deployment_id", deploymentID),
				zap.String("instance_id", oc.InstanceID), zap.Error(err))
			continue
		}
		removed++
		c.logger.Info("deployment container removed",
			zap.String("deployment_id", deploymentID), zap.String("instance_id", oc.InstanceID))
	}
	return removed
}

// RemoveVersion 强制移除某部署下指定版本的全部受管容器（按 maple.version_id 匹配，
// 并以 maple.deployment_id 限定范围）。用于「删除版本」时仅回收该版本的容器，
// 不触碰同部署其它版本——避免版本级删除被放大成部署级整删。
// 返回成功删除的容器数；单个失败仅记警告、不中断其余删除。
func (c *Controller) RemoveVersion(ctx context.Context, deploymentID, versionID string) int {
	if versionID == "" {
		return 0
	}
	removed := 0
	for _, oc := range c.actual.Containers() {
		if oc.InstanceID == "" || oc.Container.Labels["maple.version_id"] != versionID {
			continue
		}
		if deploymentID != "" && oc.Container.Labels["maple.deployment_id"] != deploymentID {
			continue
		}
		node, ok := c.registry.Get(oc.NodeName)
		if !ok {
			continue
		}
		if _, err := node.Call(ctx, agentprotocol.ActionContainerRemove, agentprotocol.RemoveParams{ID: oc.InstanceID, Force: true}); err != nil {
			c.logger.Warn("remove version container failed",
				zap.String("deployment_id", deploymentID),
				zap.String("version_id", versionID),
				zap.String("instance_id", oc.InstanceID), zap.Error(err))
			continue
		}
		removed++
		c.logger.Info("version container removed",
			zap.String("deployment_id", deploymentID),
			zap.String("version_id", versionID),
			zap.String("instance_id", oc.InstanceID))
	}
	return removed
}

// Logs 读取实例容器最近 tail 行日志。
func (c *Controller) Logs(ctx context.Context, instanceID string, tail int) (string, error) {
	node, _, err := c.locate(instanceID)
	if err != nil {
		return "", err
	}
	raw, err := node.Call(ctx, agentprotocol.ActionLogsRead, agentprotocol.LogsReadParams{ID: instanceID, Tail: tail})
	if err != nil {
		return "", err
	}
	var out agentprotocol.LogsReadResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	return out.Logs, nil
}

// Stats 采集实例容器的资源用量（CPU/内存/网络/磁盘 IO）。
// 走 Probe 通道（详情面板按需轮询，不产生任务记录）；速率为两次采样差分，首次为 0。
func (c *Controller) Stats(ctx context.Context, instanceID string) (gwclient.ContainerStats, error) {
	node, _, err := c.locate(instanceID)
	if err != nil {
		return gwclient.ContainerStats{}, err
	}
	raw, err := node.Probe(ctx, agentprotocol.ActionContainerStats, agentprotocol.IDParams{ID: instanceID})
	if err != nil {
		return gwclient.ContainerStats{}, err
	}
	var out gwclient.ContainerStats
	if err := json.Unmarshal(raw, &out); err != nil {
		return gwclient.ContainerStats{}, err
	}
	return out, nil
}

// FollowLogs 打开一条实时日志流并返回其句柄。调用方负责在结束时 Close。
// 流经 logs.open 下发到实例所在节点；ctx 取消时向 Agent 下发 logs.close。
func (c *Controller) FollowLogs(ctx context.Context, instanceID string, tail int) (*logstream.Stream, error) {
	node, _, err := c.locate(instanceID)
	if err != nil {
		return nil, err
	}
	stream := c.streams.Open(instanceID)
	sender := node.Sender()
	if sender == nil {
		c.streams.Close(stream.ID)
		return nil, fmt.Errorf("实例所在节点 %s 不在线", node.Name)
	}
	env, _ := agentprotocol.New(agentprotocol.TypeLogsOpen, stream.ID, agentprotocol.LogsOpenPayload{
		StreamID: stream.ID, Target: instanceID, Tail: tail, Follow: true,
	})
	if !sender.Send(env) {
		c.streams.Close(stream.ID)
		return nil, fmt.Errorf("向节点 %s 下发日志流失败", node.Name)
	}
	// 订阅方取消时通知 Agent 停止跟随（发送带同一 request_id 的 logs.close）。
	go func() {
		<-ctx.Done()
		closeEnv, _ := agentprotocol.New(agentprotocol.TypeLogsClose, stream.ID,
			agentprotocol.LogsClosePayload{StreamID: stream.ID, Reason: "client closed"})
		sender.Send(closeEnv)
		c.streams.Close(stream.ID)
	}()
	return stream, nil
}
