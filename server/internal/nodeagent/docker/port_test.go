package docker

import (
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/go-connections/nat"
)

// 未声明容器端口（动态发布）时，应从 Inspect 结果取到引擎分配的宿主端口。
func TestHostPortFromInspectDynamic(t *testing.T) {
	insp := types.ContainerJSON{
		NetworkSettings: &types.NetworkSettings{
			NetworkSettingsBase: types.NetworkSettingsBase{
				Ports: nat.PortMap{
					"80/tcp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "64563"}},
				},
			},
		},
	}
	if got := hostPortFromInspect(insp, 0); got != 64563 {
		t.Errorf("dynamic host port = %d, want 64563", got)
	}
}

// 已声明容器端口时按该端口精确匹配，忽略其它映射。
func TestHostPortFromInspectExact(t *testing.T) {
	insp := types.ContainerJSON{
		NetworkSettings: &types.NetworkSettings{
			NetworkSettingsBase: types.NetworkSettingsBase{
				Ports: nat.PortMap{
					"80/tcp":   []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "22222"}},
					"8080/tcp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "33333"}},
				},
			},
		},
	}
	if got := hostPortFromInspect(insp, 8080); got != 33333 {
		t.Errorf("exact host port = %d, want 33333", got)
	}
}

// 无端口映射时返回 0（尚未就绪或未发布）。
func TestHostPortFromInspectEmpty(t *testing.T) {
	insp := types.ContainerJSON{
		NetworkSettings: &types.NetworkSettings{
			NetworkSettingsBase: types.NetworkSettingsBase{Ports: nat.PortMap{}},
		},
	}
	if got := hostPortFromInspect(insp, 0); got != 0 {
		t.Errorf("empty host port = %d, want 0", got)
	}
}
