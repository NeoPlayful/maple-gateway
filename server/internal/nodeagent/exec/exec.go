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
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/compose"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/docker"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/hostmetrics"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/runtime"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/wsclient"
)

// Executor 基于本机 runtime 执行任务。
type Executor struct {
	rt *runtime.Runtime
}

// New 构造执行器。
func New(rt *runtime.Runtime) *Executor { return &Executor{rt: rt} }

// FollowLogs 实现 wsclient.LogStreamer：把一次日志跟随映射到本机 runtime。
func (e *Executor) FollowLogs(ctx context.Context, id string, tail int, emit func(chunk string) error) error {
	return e.rt.FollowLogs(ctx, id, tail, emit)
}

// WatchEvents 实现 wsclient.EventSource：把本机 Docker 事件映射为协议事件后回调。
func (e *Executor) WatchEvents(ctx context.Context, emit func(wsclient.DockerEvent)) error {
	return e.rt.WatchEvents(ctx, func(ev docker.DockerEvent) {
		emit(wsclient.DockerEvent{
			Action:      ev.Action,
			ContainerID: ev.ContainerID,
			InstanceID:  ev.InstanceID,
			Image:       ev.Image,
			ExitCode:    ev.ExitCode,
			Time:        ev.Time,
		})
	})
}

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

	case agentprotocol.ActionNodeMetrics:
		host, err := hostmetrics.Collect()
		if err != nil {
			host = hostmetrics.Metrics{Available: false}
		}
		du, _ := e.rt.DiskUsage(ctx)
		return json.Marshal(map[string]any{"host": host, "docker": du})

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
		return json.Marshal(agentprotocol.CreateResult{ContainerID: id, HostPort: port})

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
		var p agentprotocol.RemoveParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return nil, e.rt.Remove(ctx, p.ID, p.Force)

	case agentprotocol.ActionLogsRead:
		var p agentprotocol.LogsReadParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		out, err := e.rt.Logs(ctx, p.ID, p.Tail)
		if err != nil {
			return nil, err
		}
		return json.Marshal(agentprotocol.LogsReadResult{Logs: out})

	case agentprotocol.ActionImagePull:
		var p agentprotocol.ImagePullParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return nil, e.rt.PullImage(ctx, p.Image)

	case agentprotocol.ActionApplicationValidate:
		return e.appValidate(ctx, params)
	case agentprotocol.ActionApplicationDeploy:
		return e.appDeploy(ctx, params)
	case agentprotocol.ActionApplicationStop:
		return e.appStop(ctx, params)
	case agentprotocol.ActionApplicationRestart:
		return e.appRestart(ctx, params)
	case agentprotocol.ActionApplicationRemove:
		return e.appRemove(ctx, params)
	case agentprotocol.ActionApplicationPs:
		return e.appPs(ctx, params)

	default:
		return nil, fmt.Errorf("action %q not implemented", action)
	}
}

// specParam 解出 Compose 规格入参。
func specParam(raw json.RawMessage) (agentprotocol.ApplicationSpec, error) {
	var p agentprotocol.ApplicationSpec
	if err := decode(raw, &p); err != nil {
		return p, err
	}
	if p.Project == "" || p.ComposeYAML == "" {
		return p, fmt.Errorf("missing project or compose_yaml")
	}
	return p, nil
}

// appValidate 校验 Compose 规格。
func (e *Executor) appValidate(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	p, err := specParam(raw)
	if err != nil {
		return nil, err
	}
	cp := e.rt.Compose()
	if !cp.Available(ctx) {
		return nil, compose.ErrUnavailable
	}
	valid, out, err := cp.Validate(ctx, p.Project, p.ComposeYAML)
	if err != nil {
		return nil, err
	}
	res := agentprotocol.ApplicationValidateResult{Valid: valid}
	if valid {
		res.Output = out
	} else {
		res.Errors = []string{out}
	}
	return json.Marshal(res)
}

// appDeploy 部署 Application（compose up -d）。
func (e *Executor) appDeploy(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	p, err := specParam(raw)
	if err != nil {
		return nil, err
	}
	cp := e.rt.Compose()
	if !cp.Available(ctx) {
		return nil, compose.ErrUnavailable
	}
	svcs, out, err := cp.Up(ctx, p.Project, p.ComposeYAML)
	if err != nil {
		return nil, err
	}
	return json.Marshal(agentprotocol.ApplicationActionResult{
		Project: p.Project, State: "running", Services: toProtoServices(svcs), Output: out,
	})
}

// appStop 停止 Application（compose down）。
func (e *Executor) appStop(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	p, err := specParam(raw)
	if err != nil {
		return nil, err
	}
	out, err := e.rt.Compose().Stop(ctx, p.Project)
	if err != nil {
		return nil, err
	}
	return json.Marshal(agentprotocol.ApplicationActionResult{Project: p.Project, State: "stopped", Output: out})
}

// appRestart 重启 Application（compose restart）。
func (e *Executor) appRestart(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	p, err := specParam(raw)
	if err != nil {
		return nil, err
	}
	svcs, out, err := e.rt.Compose().Restart(ctx, p.Project)
	if err != nil {
		return nil, err
	}
	return json.Marshal(agentprotocol.ApplicationActionResult{
		Project: p.Project, State: "running", Services: toProtoServices(svcs), Output: out,
	})
}

// appRemove 移除 Application（compose down -v + 清理文件）。
func (e *Executor) appRemove(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	p, err := specParam(raw)
	if err != nil {
		return nil, err
	}
	out, err := e.rt.Compose().Remove(ctx, p.Project)
	if err != nil {
		return nil, err
	}
	return json.Marshal(agentprotocol.ApplicationActionResult{Project: p.Project, State: "removed", Output: out})
}

// appPs 列出 Application 内服务运行态。
func (e *Executor) appPs(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var p struct {
		Project string `json:"project"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	svcs, err := e.rt.Compose().Ps(ctx, p.Project)
	if err != nil {
		return nil, err
	}
	return json.Marshal(agentprotocol.ApplicationPsResult{Project: p.Project, Services: toProtoServices(svcs)})
}

// toProtoServices 把 compose 驱动视图映射为协议视图。
func toProtoServices(svcs []compose.Service) []agentprotocol.ApplicationService {
	out := make([]agentprotocol.ApplicationService, 0, len(svcs))
	for _, s := range svcs {
		ps := agentprotocol.ApplicationService{
			Name: s.Name, Service: s.Service, State: s.State, Status: s.Status,
			Health: s.Health, Image: s.Image, ContainerID: s.ContainerID,
		}
		for _, p := range s.Publishers {
			ps.Publishers = append(ps.Publishers, agentprotocol.ApplicationPortPublisher{
				URL: p.URL, TargetPort: p.TargetPort, PublishedPort: p.PublishedPort, Protocol: p.Protocol,
			})
		}
		out = append(out, ps)
	}
	return out
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
