// sync 全量状态同步：Container Manager 一次上报全部 Node + Instance，
// Gateway 幂等对账（存在则刷新，不存在则注册），并更新心跳。
package discovery

import (
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/internal/instance"
	"github.com/NeoPlayful/maple-gateway/server/internal/node"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// SyncNode 上报的一个节点及其实例。
type SyncNode struct {
	Name      string           `json:"name" validate:"required"`
	Host      string           `json:"host" validate:"required"`
	Region    string           `json:"region"`
	Labels    map[string]string `json:"labels"`
	Weight    int              `json:"weight"`
	Instances []SyncInstance   `json:"instances"`
}

// SyncInstance 上报的一个实例。
type SyncInstance struct {
	ServiceID    uuid.UUID `json:"service_id" validate:"required"`
	DeploymentID *uuid.UUID `json:"deployment_id"`
	VersionID    *uuid.UUID `json:"version_id"`
	Version      string     `json:"version"`
	Address      string     `json:"address" validate:"required"`
	Port         int        `json:"port" validate:"required,min=1,max=65535"`
	Protocol     string     `json:"protocol"`
	Weight       int        `json:"weight"`
}

// SyncRequest 全量同步请求体。
type SyncRequest struct {
	Nodes []SyncNode `json:"nodes" validate:"required,dive"`
}

// SyncResult 同步结果摘要。
type SyncResult struct {
	Nodes     int `json:"nodes"`     // 处理节点数
	Instances int `json:"instances"` // 处理实例数
	Created   int `json:"created"`   // 新建节点数
	Updated   int `json:"updated"`   // 更新/心跳节点数
}

// Sync POST /api/internal/discovery/sync
// 幂等：按节点名对账——存在则刷新 host/region/weight 并更新心跳，否则新建。
// 实例按 service_id+address:port 对账——已存在则跳过（实例级删除由 DELETE 单独负责），
// 否则注册。失败即返回错误，不部分成功返回。
func (h *Handler) Sync(c fiber.Ctx) error {
	var in SyncRequest
	if err := c.Bind().Body(&in); err != nil {
		return pkg.Err(c, pkg.ErrValidation("请求体格式错误"))
	}
	if err := pkg.ValidateStruct(in); err != nil {
		return pkg.Err(c, err)
	}

	res := &SyncResult{}
	for _, sn := range in.Nodes {
		// Node 对账。
		existing, err := h.nodes.GetByName(c.Context(), sn.Name)
		if err != nil {
			return pkg.Err(c, err)
		}
		var n *node.Node
		if existing == nil {
			n, err = h.nodes.Create(c.Context(), node.New{
				Name:   sn.Name,
				Host:   sn.Host,
				Region: sn.Region,
				Labels: sn.Labels,
				Weight: sn.Weight,
			})
			if err != nil {
				return pkg.Err(c, err)
			}
			res.Created++
		} else {
			upd := node.Update{
				Host:   &sn.Host,
				Labels: sn.Labels,
			}
			if sn.Region != "" {
				upd.Region = &sn.Region
			}
			if sn.Weight > 0 {
				w := sn.Weight
				upd.Weight = &w
			}
			if n, err = h.nodes.Update(c.Context(), existing.ID, upd); err != nil {
				return pkg.Err(c, err)
			}
			res.Updated++
		}
		// 注册即心跳。
		if _, err := h.nodes.Heartbeat(c.Context(), n.ID); err != nil {
			return pkg.Err(c, err)
		}
		res.Nodes++

		// 实例对账：按 service 预取现有实例，避免逐条 ListByService。
		if len(sn.Instances) == 0 {
			continue
		}
		if err := h.syncInstances(c, n.ID, sn.Instances, res); err != nil {
			return err
		}
	}
	return pkg.OK(c, res)
}

// syncInstances 对单个节点下的实例做幂等对账。
func (h *Handler) syncInstances(c fiber.Ctx, nodeID uuid.UUID, items []SyncInstance, res *SyncResult) error {
	ctx := c.Context()
	// 收集涉及的服务，批量预取其现有实例（address:port → instance）。
	seen := map[uuid.UUID]map[string]*instance.Instance{}
	for _, it := range items {
		if _, ok := seen[it.ServiceID]; ok {
			continue
		}
		existing, err := h.instances.ListByService(ctx, it.ServiceID)
		if err != nil {
			return pkg.Err(c, err)
		}
		m := make(map[string]*instance.Instance, len(existing))
		for _, e := range existing {
			key := fmt.Sprintf("%s:%d", e.Address, e.Port)
			m[key] = e
		}
		seen[it.ServiceID] = m
	}

	for _, it := range items {
		key := fmt.Sprintf("%s:%d", it.Address, it.Port)
		if e, ok := seen[it.ServiceID][key]; ok {
			// 已存在：先刷新"最后被 CM 看见"时间（上报即心跳）。
			if _, err := h.instances.Heartbeat(ctx, e.ID); err != nil {
				return pkg.Err(c, err)
			}
			// 仅当上报归属不同 node 时挂载该节点（幂等刷新），不重复建。
			if e.NodeID == nil || *e.NodeID != nodeID {
				_, err := h.instances.Mount(ctx, e.ID, instance.Mount{NodeID: &nodeID})
				if err != nil {
					return pkg.Err(c, err)
				}
			}
			res.Instances++
			continue
		}
		created, err := h.instances.Create(ctx, instance.New{
			ServiceID:    it.ServiceID,
			DeploymentID: it.DeploymentID,
			VersionID:    it.VersionID,
			NodeID:       &nodeID,
			Version:      it.Version,
			Address:      it.Address,
			Port:         it.Port,
			Protocol:     it.Protocol,
			Weight:       it.Weight,
		})
		if err != nil {
			return pkg.Err(c, err)
		}
		// 记入已存在表，防同批重复 key 再次 Create。
		seen[it.ServiceID][key] = created
		res.Instances++
	}
	return nil
}
