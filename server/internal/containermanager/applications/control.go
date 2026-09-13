package applications

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/NeoPlayful/maple-gateway/server/internal/containermanager/agentregistry"
	"go.uber.org/zap"
)

// Controller 通过节点命令通道驱动 Application 的部署/停止/重启/移除。
type Controller struct {
	store    *Store
	registry *agentregistry.Registry
	logger   *zap.Logger
}

// NewController 构造。
func NewController(store *Store, registry *agentregistry.Registry, logger *zap.Logger) *Controller {
	return &Controller{store: store, registry: registry, logger: logger}
}

// project 是 Compose 项目名的确定规则：应用 ID（去连字符）做隔离前缀，节点间不冲突。
func project(a Application) string {
	s := ""
	for _, r := range a.ID {
		if r != '-' {
			s += string(r)
		}
	}
	if len(s) > 24 {
		s = s[:24]
	}
	return "maple-app-" + s
}

// spec 组装下发给 Agent 的 Compose 规格。
func spec(a Application) agentprotocol.ApplicationSpec {
	return agentprotocol.ApplicationSpec{
		ApplicationID: a.ID,
		Version:       a.Version,
		Project:       project(a),
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

// Deploy 部署应用：下发 compose up 到落点节点，成功后回写运行态。
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
	raw, err := n.Call(ctx, agentprotocol.ActionApplicationDeploy, spec(a))
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

// Remove 移除应用并删除本地记录。
func (c *Controller) Remove(ctx context.Context, id string) error {
	a, ok := c.store.Get(id)
	if !ok {
		return fmt.Errorf("应用不存在")
	}
	n, err := c.node(a)
	if err != nil {
		return err
	}
	if _, err := n.Call(ctx, agentprotocol.ActionApplicationRemove, spec(a)); err != nil {
		return err
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
	raw, err := n.Call(ctx, agentprotocol.ActionApplicationValidate, spec(a))
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

// action 执行一次返回运行态的应用操作。
func (c *Controller) action(ctx context.Context, id, act, state string) (Application, error) {
	a, ok := c.store.Get(id)
	if !ok {
		return Application{}, fmt.Errorf("应用不存在")
	}
	n, err := c.node(a)
	if err != nil {
		return Application{}, err
	}
	if _, err := n.Call(ctx, act, spec(a)); err != nil {
		return Application{}, err
	}
	c.store.SetStatus(id, state)
	out, _ := c.store.Get(id)
	return out, nil
}
