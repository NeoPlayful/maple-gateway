// Package exec 把 CM 下发的 Action 映射到本机容器运行时操作。
//
// 只接受白名单内的 Action（见 agentprotocol），不接受任意 Shell；
// 所有操作经 runtime 完成，并限定在受管容器范围内。
package exec

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/docker"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/runtime"
)

// Executor 基于本机 runtime 执行任务。
type Executor struct {
	rt *runtime.Runtime
}

// New 构造执行器。
func New(rt *runtime.Runtime) *Executor { return &Executor{rt: rt} }

// Execute 按 action 分发到具体操作，返回结果载荷（JSON）。
func (e *Executor) Execute(ctx context.Context, action string, params json.RawMessage) (json.RawMessage, error) {
	if !agentprotocol.IsAllowedAction(action) {
		return nil, fmt.Errorf("action %q not allowed", action)
	}
	switch action {
	case agentprotocol.ActionSystemInfo, agentprotocol.ActionDockerInfo:
		info, err := e.rt.Info(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(info)

	case agentprotocol.ActionContainerList:
		list, err := e.rt.List(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(list)

	case agentprotocol.ActionContainerCreate:
		var spec docker.CreateSpec
		if err := decode(params, &spec); err != nil {
			return nil, err
		}
		id, port, err := e.rt.Ensure(ctx, spec)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"container_id": id, "host_port": port})

	case agentprotocol.ActionContainerStart:
		id, err := idParam(params)
		if err != nil {
			return nil, err
		}
		return nil, e.rt.Start(ctx, id)

	case agentprotocol.ActionContainerStop:
		id, err := idParam(params)
		if err != nil {
			return nil, err
		}
		return nil, e.rt.Stop(ctx, id)

	case agentprotocol.ActionContainerRestart:
		id, err := idParam(params)
		if err != nil {
			return nil, err
		}
		return nil, e.rt.Restart(ctx, id)

	case agentprotocol.ActionContainerRemove:
		var p struct {
			ID    string `json:"id"`
			Force bool   `json:"force"`
		}
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return nil, e.rt.Remove(ctx, p.ID, p.Force)

	case agentprotocol.ActionLogsRead:
		var p struct {
			ID   string `json:"id"`
			Tail int    `json:"tail"`
		}
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		out, err := e.rt.Logs(ctx, p.ID, p.Tail)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"logs": out})

	case agentprotocol.ActionImagePull:
		var p struct {
			Image string `json:"image"`
		}
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return nil, e.rt.PullImage(ctx, p.Image)

	default:
		return nil, fmt.Errorf("action %q not implemented", action)
	}
}

func decode(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		return fmt.Errorf("missing params")
	}
	return json.Unmarshal(raw, out)
}

func idParam(raw json.RawMessage) (string, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &p); err != nil {
		return "", err
	}
	if p.ID == "" {
		return "", fmt.Errorf("missing id")
	}
	return p.ID, nil
}
