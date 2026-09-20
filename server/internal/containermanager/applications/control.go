package applications

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"go.uber.org/zap"
)

// NetworkProvisioner 为应用准备项目网络：返回可用的 Docker 网络名（子网由 IPAM 统一分配）。
// 实现须幂等：同一应用重复调用返回同一网络；并在首次调用时预留子网、建好 Docker 网络。
type NetworkProvisioner interface {
	// Ensure 确保应用网络就绪，返回网络名。
	Ensure(ctx context.Context, nodeID, appID string) (networkName string, err error)
	// Release 释放应用网络（项目移除时调用）：删 Docker 网络并归还子网。
	Release(ctx context.Context, appID string) error
}

// Controller 通过节点命令通道驱动 Application 的部署/停止/重启/移除。
type Controller struct {
	store    *Store
	registry *agentregistry.Registry
	logger   *zap.Logger
	// net 可空：未接入 IPAM 时为 nil，部署沿用 Compose 默认网络。
	net NetworkProvisioner
}

// NewController 构造。
func NewController(store *Store, registry *agentregistry.Registry, logger *zap.Logger) *Controller {
	return &Controller{store: store, registry: registry, logger: logger}
}

// WithNetworkProvisioner 注入项目网络分配器（部署前分配子网并建网）。
func (c *Controller) WithNetworkProvisioner(p NetworkProvisioner) *Controller {
	c.net = p
	return c
}

// project 是 Compose 项目名的确定规则：应用 ID 的短标识做隔离前缀，节点间不冲突。
// 应用 ID 即 Gateway 项目 ID，故项目网络名与之逐字相同。
func project(a Application) string {
	return "maple-" + pkg.ShortID(a.ID)
}

// spec 组装下发给 Agent 的 Compose 规格。networkName 非空时接入该外部网络。
func spec(a Application, networkName string) agentprotocol.ApplicationSpec {
	return agentprotocol.ApplicationSpec{
		ApplicationID: a.ID,
		Version:       a.Version,
		Project:       project(a),
		NetworkName:   networkName,
		ComposeYAML:   a.Spec,
	}
}

// node 定位应用落点：优先应用已登记的 node_id，否则回退首个在线节点。
func (c *Controller) node(a Application) (*agentregistry.Node, error) {
	if a.NodeID != "" {
		if n, ok := c.registry.GetByID(a.NodeID); ok && n.Online() {
			return n, nil
		}
	}
	for _, n := range c.registry.All() {
		if n.Online() {
			return n, nil
		}
	}
	return nil, fmt.Errorf("无可用节点执行 Application 操作")
}

// Deploy 部署应用：先准备项目网络（分配子网 + 建 Docker 网络），再下发 compose up，
// 成功后回写运行态。首次部署预留网段；重复部署复用原网段（文档 §49/§50/§51）。
func (c *Controller) Deploy(ctx context.Context, id string) (Application, error) {
	a, ok := c.store.Get(id)
	if !ok {
		return Application{}, fmt.Errorf("应用不存在")
	}
	if a.Spec == "" {
		return Application{}, fmt.Errorf("应用无 Compose 规格")
	}
	n, err := c.node(a)
	if err != nil {
		return Application{}, err
	}
	a.NodeID = n.ID()

	// 项目网络：未接入 IPAM 时 networkName 为空，沿用 Compose 默认网络。
	networkName := ""
	if c.net != nil {
		networkName, err = c.net.Ensure(ctx, n.ID(), a.ID)
		if err != nil {
			a.Status = "failed"
			c.store.Put(a)
			return Application{}, fmt.Errorf("准备项目网络失败: %w", err)
		}
	}

	raw, err := n.Call(ctx, agentprotocol.ActionApplicationDeploy, spec(a, networkName))
	if err != nil {
		a.Status = "failed"
		c.store.Put(a)
		return Application{}, err
	}
	var res agentprotocol.ApplicationActionResult
	_ = json.Unmarshal(raw, &res)
	state := res.State
	if state == "" {
		state = "running"
	}
	a.Status = state
	c.store.Put(a)
	c.logger.Info("application deployed", zap.String("app", id), zap.String("node", n.Name))
	out, _ := c.store.Get(id)
	return out, nil
}

// Stop 停止应用（容器保留，可经 Start 恢复）。
func (c *Controller) Stop(ctx context.Context, id string) (Application, error) {
	return c.action(ctx, id, agentprotocol.ActionApplicationStop, "stopped")
}

// Start 启动已停止的应用（复用既有容器）。
func (c *Controller) Start(ctx context.Context, id string) (Application, error) {
	return c.action(ctx, id, agentprotocol.ActionApplicationStart, "running")
}

// Restart 重启应用。
func (c *Controller) Restart(ctx context.Context, id string) (Application, error) {
	return c.action(ctx, id, agentprotocol.ActionApplicationRestart, "running")
}

// Remove 移除应用：先 compose down 清容器，再删项目网络、归还子网，最后删本地记录。
func (c *Controller) Remove(ctx context.Context, id string) error {
	a, ok := c.store.Get(id)
	if !ok {
		return fmt.Errorf("应用不存在")
	}
	n, err := c.node(a)
	if err != nil {
		return err
	}
	networkName := ""
	if c.net != nil {
		networkName = "maple-" + pkg.ShortID(a.ID)
	}
	if _, err := n.Call(ctx, agentprotocol.ActionApplicationRemove, spec(a, networkName)); err != nil {
		return err
	}
	// 容器已清，随后删网络并释放子网。释放失败不阻塞记录删除（记录交由对账收敛）。
	if c.net != nil {
		if err := c.net.Release(ctx, a.ID); err != nil {
			c.logger.Warn("释放项目网络失败", zap.String("app", id), zap.Error(err))
		}
	}
	c.store.Delete(id)
	c.logger.Info("application removed", zap.String("app", id), zap.String("node", n.Name))
	return nil
}

// Ps 查询应用内服务运行态。
func (c *Controller) Ps(ctx context.Context, id string) ([]agentprotocol.ApplicationService, error) {
	a, ok := c.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("应用不存在")
	}
	n, err := c.node(a)
	if err != nil {
		return nil, err
	}
	raw, err := n.Call(ctx, agentprotocol.ActionApplicationPs, map[string]string{"project": project(a)})
	if err != nil {
		return nil, err
	}
	var res agentprotocol.ApplicationPsResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	return res.Services, nil
}

// Validate 校验应用当前规格（不落容器）。
func (c *Controller) Validate(ctx context.Context, id string) (bool, string, error) {
	a, ok := c.store.Get(id)
	if !ok {
		return false, "", fmt.Errorf("应用不存在")
	}
	n, err := c.node(a)
	if err != nil {
		return false, "", err
	}
	raw, err := n.Call(ctx, agentprotocol.ActionApplicationValidate, spec(a, ""))
	if err != nil {
		return false, "", err
	}
	var res agentprotocol.ApplicationValidateResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return false, "", err
	}
	if len(res.Errors) > 0 {
		return res.Valid, res.Errors[0], nil
	}
	return res.Valid, res.Output, nil
}

// action 执行一次返回运行态的应用操作。stop/start/restart 作用于既有容器，
// 不重读规格文件，故不传网络名（网络仅在部署时接入）。
func (c *Controller) action(ctx context.Context, id, act, state string) (Application, error) {
	a, ok := c.store.Get(id)
	if !ok {
		return Application{}, fmt.Errorf("应用不存在")
	}
	n, err := c.node(a)
	if err != nil {
		return Application{}, err
	}
	if _, err := n.Call(ctx, act, spec(a, "")); err != nil {
		return Application{}, err
	}
	c.store.SetStatus(id, state)
	out, _ := c.store.Get(id)
	return out, nil
}
