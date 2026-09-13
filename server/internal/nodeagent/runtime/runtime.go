// Package runtime 实现 Node Agent 的容器生命周期操作（Docker 之上的薄抽象）。
//
// 所有操作都限定在受管容器范围内（带受管标签），并统一处理镜像允许清单、
// 幂等创建与容器标识解析（容器 ID 或 instance_id 均可）。
package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/compose"
	"github.com/NeoPlayful/maple-gateway/server/internal/nodeagent/docker"
)

// Runtime 是容器运行时操作入口。
type Runtime struct {
	docker        *docker.Client
	compose       *compose.Driver
	allowedImages []string
	stopTimeout   time.Duration
}

// New 构造。
func New(d *docker.Client, allowedImages []string) *Runtime {
	return &Runtime{
		docker:        d,
		compose:       compose.New(""),
		allowedImages: allowedImages,
		stopTimeout:   10 * time.Second,
	}
}

// Compose 返回 Compose 驱动（供执行器驱动应用部署）。
func (r *Runtime) Compose() *compose.Driver { return r.compose }

// Info 返回节点资源与容量。
func (r *Runtime) Info(ctx context.Context) (docker.NodeInfo, error) {
	return r.docker.Info(ctx)
}

// List 列出全部受管容器。
func (r *Runtime) List(ctx context.Context) ([]docker.Container, error) {
	return r.docker.ListManaged(ctx)
}

// Ensure 幂等创建并启动容器，返回容器 ID 与实际映射端口。
func (r *Runtime) Ensure(ctx context.Context, spec docker.CreateSpec) (string, int, error) {
	return r.docker.Ensure(ctx, spec, r.allowedImages)
}

// Start 启动容器（id 可为容器 ID 或 instance_id）。
func (r *Runtime) Start(ctx context.Context, id string) error {
	real, err := r.resolve(ctx, id)
	if err != nil {
		return err
	}
	return r.docker.Start(ctx, real)
}

// Stop 优雅停止容器（id 可为容器 ID 或 instance_id）。
func (r *Runtime) Stop(ctx context.Context, id string) error {
	real, err := r.resolve(ctx, id)
	if err != nil {
		return err
	}
	return r.docker.Stop(ctx, real, r.stopTimeout)
}

// Remove 删除容器（id 可为容器 ID 或 instance_id）。
func (r *Runtime) Remove(ctx context.Context, id string, force bool) error {
	real, err := r.resolve(ctx, id)
	if err != nil {
		return err
	}
	return r.docker.Remove(ctx, real, force)
}

// Restart 重启容器（id 可为容器 ID 或 instance_id）。
func (r *Runtime) Restart(ctx context.Context, id string) error {
	real, err := r.resolve(ctx, id)
	if err != nil {
		return err
	}
	return r.docker.Restart(ctx, real, r.stopTimeout)
}

// Logs 读取容器最近 tail 行日志（id 可为容器 ID 或 instance_id）。
func (r *Runtime) Logs(ctx context.Context, id string, tail int) (string, error) {
	real, err := r.resolve(ctx, id)
	if err != nil {
		return "", err
	}
	return r.docker.Logs(ctx, real, tail)
}

// FollowLogs 持续跟随容器日志，每读到一段即回调 emit（id 可为容器 ID 或 instance_id）。
func (r *Runtime) FollowLogs(ctx context.Context, id string, tail int, emit func(chunk string) error) error {
	real, err := r.resolve(ctx, id)
	if err != nil {
		return err
	}
	return r.docker.FollowLogs(ctx, real, tail, emit)
}

// DiskUsage 返回 Docker 引擎空间占用摘要。
func (r *Runtime) DiskUsage(ctx context.Context) (docker.DiskUsage, error) {
	return r.docker.DiskUsage(ctx)
}

// PullImage 拉取镜像（受允许清单约束）。
func (r *Runtime) PullImage(ctx context.Context, ref string) error {
	return r.docker.Pull(ctx, ref, r.allowedImages)
}

// WatchEvents 订阅本机受管容器的 Docker 事件并逐条回调 emit。
func (r *Runtime) WatchEvents(ctx context.Context, emit func(docker.DockerEvent)) error {
	return r.docker.WatchEvents(ctx, emit)
}

// resolve 把传入标识解析为真实容器 ID：优先按 instance_id 标签匹配受管容器，
// 未命中则回退按容器 ID/名称直查（仍限受管容器）。
func (r *Runtime) resolve(ctx context.Context, id string) (string, error) {
	if looksLikeUUID(id) {
		if c, ok, err := r.docker.FindByInstance(ctx, id); err != nil {
			return "", err
		} else if ok {
			return c.ID, nil
		}
	}
	return id, nil
}

// looksLikeUUID 粗判是否 UUID 形态（36 字符 4 连字符），用于选择解析路径。
func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return strings.Count(s, "-") == 4
}
