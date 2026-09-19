package docker

import (
	"testing"

	"github.com/docker/docker/api/types/network"
)

func TestValidateSubnetGateway(t *testing.T) {
	if err := validateSubnetGateway("10.128.15.0/24", "10.128.15.1"); err != nil {
		t.Errorf("valid subnet/gateway rejected: %v", err)
	}
	// 网关不在子网内。
	if err := validateSubnetGateway("10.128.15.0/24", "10.128.16.1"); err == nil {
		t.Error("expected gateway-out-of-subnet error")
	}
	// 非网络地址。
	if err := validateSubnetGateway("10.128.15.5/24", ""); err == nil {
		t.Error("expected non-network-address error")
	}
	// 非法 CIDR。
	if err := validateSubnetGateway("not-a-cidr", ""); err == nil {
		t.Error("expected invalid CIDR error")
	}
}

func TestSameNetwork(t *testing.T) {
	insp := network.Inspect{
		Driver: "bridge",
		IPAM:   network.IPAM{Config: []network.IPAMConfig{{Subnet: "10.128.15.0/24", Gateway: "10.128.15.1"}}},
	}
	// 子网与网关一致 → 幂等命中（文档 §48）。
	if !sameNetwork(insp, NetworkSpec{Name: "maple-x", Subnet: "10.128.15.0/24", Gateway: "10.128.15.1"}) {
		t.Error("identical spec should match")
	}
	// 子网不同 → 不匹配（触发 conflict）。
	if sameNetwork(insp, NetworkSpec{Name: "maple-x", Subnet: "10.128.16.0/24", Gateway: "10.128.16.1"}) {
		t.Error("different subnet must not match")
	}
	// 仅子网一致、未指定网关 → 视为匹配。
	if !sameNetwork(insp, NetworkSpec{Name: "maple-x", Subnet: "10.128.15.0/24"}) {
		t.Error("subnet-only match should succeed")
	}
}

func TestToNetworkInfoManagedDetection(t *testing.T) {
	// 带受管标签。
	withLabel := network.Inspect{Name: "whatever", Labels: map[string]string{ManagedNetworkLabel: "true"}}
	if !toNetworkInfo(withLabel).Managed {
		t.Error("label-marked network should be managed")
	}
	// 无标签但名字形如 maple-*。
	byName := network.Inspect{Name: "maple-31c6635dffc5", Labels: map[string]string{}}
	if !toNetworkInfo(byName).Managed {
		t.Error("maple- prefixed network should be managed")
	}
	// 第三方网络不算受管。
	third := network.Inspect{Name: "bridge", Labels: map[string]string{}}
	if toNetworkInfo(third).Managed {
		t.Error("third-party network should not be managed")
	}
}
