package docker

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
)

// 网络操作错误码（对齐文档 §48/§76）。
const (
	CodeNetworkConflict = "NETWORK_CONFLICT"
	CodeNetworkInUse    = "NETWORK_IN_USE"
	CodeNetworkNotFound = "NETWORK_NOT_FOUND"
)

// ManagedNetworkLabel 标记由 Maple Gateway 创建的网络。
const ManagedNetworkLabel = "maple.managed"

// NetworkSpec 是一次网络创建的规格。
type NetworkSpec struct {
	Name    string `json:"name"`
	Driver  string `json:"driver,omitempty"`
	Subnet  string `json:"subnet"`
	Gateway string `json:"gateway,omitempty"`
}

// CreateNetworkResult 是网络创建的结果。
type CreateNetworkResult struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	AlreadyExists bool   `json:"already_exists"`
}

// NetworkInfo 是网络对外的视图。
type NetworkInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Driver     string   `json:"driver"`
	Subnet     string   `json:"subnet,omitempty"`
	Gateway    string   `json:"gateway,omitempty"`
	Managed    bool     `json:"managed"`
	Containers []string `json:"containers,omitempty"`
}

// NetworkCreate 幂等创建 bridge 网络（文档 §47/§48）：
//   - 同名网络不存在 → 创建；
//   - 已存在且 subnet/gateway 完全一致 → 幂等成功（already_exists=true）；
//   - 已存在但参数不同 → NETWORK_CONFLICT。
func (c *Client) NetworkCreate(ctx context.Context, spec NetworkSpec) (CreateNetworkResult, error) {
	if spec.Name == "" || spec.Subnet == "" {
		return CreateNetworkResult{}, fmt.Errorf("缺少网络名或子网")
	}
	if err := validateSubnetGateway(spec.Subnet, spec.Gateway); err != nil {
		return CreateNetworkResult{}, err
	}
	if existing, ok, err := c.findNetworkByName(ctx, spec.Name); err != nil {
		return CreateNetworkResult{}, err
	} else if ok {
		if sameNetwork(existing, spec) {
			return CreateNetworkResult{ID: existing.ID, Name: existing.Name, AlreadyExists: true}, nil
		}
		return CreateNetworkResult{}, &NetError{Code: CodeNetworkConflict,
			Message: fmt.Sprintf("同名网络 %s 已存在但子网/网关不一致", spec.Name)}
	}

	driver := spec.Driver
	if driver == "" {
		driver = "bridge"
	}
	options := network.CreateOptions{
		Driver: driver,
		Labels: map[string]string{ManagedNetworkLabel: "true"},
		IPAM: &network.IPAM{
			Driver: "default",
			Config: []network.IPAMConfig{{Subnet: spec.Subnet, Gateway: spec.Gateway}},
		},
	}
	resp, err := c.cli.NetworkCreate(ctx, spec.Name, options)
	if err != nil {
		return CreateNetworkResult{}, fmt.Errorf("创建网络: %w", err)
	}
	return CreateNetworkResult{ID: resp.ID, Name: spec.Name}, nil
}

// NetworkInspect 返回网络详情（不存在时 ok=false）。
func (c *Client) NetworkInspect(ctx context.Context, name string) (NetworkInfo, bool, error) {
	insp, ok, err := c.findNetworkByName(ctx, name)
	if err != nil || !ok {
		return NetworkInfo{}, false, err
	}
	return toNetworkInfo(insp), true, nil
}

// NetworkRemove 删除网络；仍有关联容器时拒绝（文档 §52 安全检查）。
func (c *Client) NetworkRemove(ctx context.Context, name string) error {
	insp, ok, err := c.findNetworkByName(ctx, name)
	if err != nil {
		return err
	}
	if !ok {
		return &NetError{Code: CodeNetworkNotFound, Message: fmt.Sprintf("网络 %s 不存在", name)}
	}
	if len(insp.Containers) > 0 {
		return &NetError{Code: CodeNetworkInUse,
			Message: fmt.Sprintf("网络 %s 仍有关联容器 %d 个", name, len(insp.Containers))}
	}
	if err := c.cli.NetworkRemove(ctx, insp.ID); err != nil {
		return fmt.Errorf("删除网络 %s: %w", name, err)
	}
	return nil
}

// NetworkListManaged 列出受管网络（带 maple.managed 标签，或名形如 maple-*）。
func (c *Client) NetworkListManaged(ctx context.Context) ([]NetworkInfo, error) {
	raw, err := c.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", ManagedNetworkLabel+"=true")),
	})
	if err != nil {
		return nil, fmt.Errorf("列出受管网络: %w", err)
	}
	out := make([]NetworkInfo, 0, len(raw))
	for _, n := range raw {
		info := NetworkInfo{ID: n.ID, Name: n.Name, Driver: n.Driver, Managed: true}
		if len(n.IPAM.Config) > 0 {
			info.Subnet = n.IPAM.Config[0].Subnet
			info.Gateway = n.IPAM.Config[0].Gateway
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// NetError 是携带错误码的网络操作错误。
type NetError struct {
	Code    string
	Message string
}

func (e *NetError) Error() string { return e.Message }

// findNetworkByName 按名精确查找网络（NetworkList 的 name 过滤为子串匹配，故再精确比对）。
func (c *Client) findNetworkByName(ctx context.Context, name string) (network.Inspect, bool, error) {
	list, err := c.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return network.Inspect{}, false, fmt.Errorf("查找网络 %s: %w", name, err)
	}
	for _, n := range list {
		if n.Name != name {
			continue
		}
		insp, err := c.cli.NetworkInspect(ctx, n.ID, network.InspectOptions{})
		if err != nil {
			return network.Inspect{}, false, fmt.Errorf("检查网络 %s: %w", name, err)
		}
		return insp, true, nil
	}
	return network.Inspect{}, false, nil
}

// sameNetwork 报告既有网络与目标规格是否完全一致（子网与网关）。
func sameNetwork(insp network.Inspect, spec NetworkSpec) bool {
	if insp.Driver != "" && spec.Driver != "" && insp.Driver != spec.Driver {
		return false
	}
	for _, cfg := range insp.IPAM.Config {
		if cfg.Subnet == spec.Subnet {
			if spec.Gateway == "" || cfg.Gateway == spec.Gateway {
				return true
			}
		}
	}
	return false
}

// toNetworkInfo 把 inspect 结果映射为对外视图。
func toNetworkInfo(insp network.Inspect) NetworkInfo {
	info := NetworkInfo{
		ID: insp.ID, Name: insp.Name, Driver: insp.Driver,
		Managed: insp.Labels[ManagedNetworkLabel] == "true" || strings.HasPrefix(insp.Name, "maple-"),
	}
	if len(insp.IPAM.Config) > 0 {
		info.Subnet = insp.IPAM.Config[0].Subnet
		info.Gateway = insp.IPAM.Config[0].Gateway
	}
	for _, ep := range insp.Containers {
		info.Containers = append(info.Containers, ep.Name)
	}
	sort.Strings(info.Containers)
	return info
}

// validateSubnetGateway 校验子网 CIDR 与（可选的）网关落在子网内。
func validateSubnetGateway(subnet, gateway string) error {
	p, err := netip.ParsePrefix(subnet)
	if err != nil {
		return fmt.Errorf("非法子网 %q: %w", subnet, err)
	}
	if p.Addr() != p.Masked().Addr() {
		return fmt.Errorf("子网 %q 不是网络地址", subnet)
	}
	if !p.Addr().Is4() {
		return fmt.Errorf("仅支持 IPv4 子网，得到 %q", subnet)
	}
	if gateway != "" {
		gw, err := netip.ParseAddr(gateway)
		if err != nil {
			return fmt.Errorf("非法网关 %q: %w", gateway, err)
		}
		if !p.Contains(gw) {
			return fmt.Errorf("网关 %s 不在子网 %s 内", gateway, subnet)
		}
	}
	return nil
}
