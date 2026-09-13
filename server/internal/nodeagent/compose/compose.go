// Package compose 在 Node Agent 侧驱动 Docker Compose：把 CM 下发的 Compose 规格
// 落盘为临时文件，并以项目名隔离地调用 compose CLI 完成 validate/up/down/ps 等操作。
//
// 选择 CLI 而非 Go SDK：compose 规格语法与版本演进复杂，官方 CLI 是权威实现；
// 本包不接受任意用户命令，只透传 CM 下发、经 CM 白名单控制的 compose YAML，
// 命令与参数由本包固定拼装（不存在任意 shell 注入面）。
package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Driver 用 compose CLI 管理应用（按 project 名隔离）。
type Driver struct {
	// workDir 是各项目组成文件与覆盖文件的落盘根目录。
	workDir string
	// binary 是 compose 可执行文件（默认 "docker"，子命令 compose）。
	binary string
	// commandTimeout 是单次 compose 命令的执行上限。
	commandTimeout time.Duration
}

// New 构造。workDir 为空则用系统临时目录下的 maple-compose。
func New(workDir string) *Driver {
	if workDir == "" {
		workDir = filepath.Join(os.TempDir(), "maple-compose")
	}
	return &Driver{workDir: workDir, binary: "docker", commandTimeout: 10 * time.Minute}
}

// ErrUnavailable 表示本机未安装 compose 插件。
var ErrUnavailable = fmt.Errorf("docker compose 不可用（未安装 compose 插件或不在 PATH）")

// Available 探测 compose CLI 是否可用（docker compose version）。
func (d *Driver) Available(ctx context.Context) bool {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, d.binary, "compose", "version")
	return cmd.Run() == nil
}

// composeFile 返回某项目的 compose 文件路径。
func (d *Driver) composeFile(project string) string {
	return filepath.Join(d.workDir, project, "compose.yaml")
}

// writeSpec 把 compose YAML 落盘到项目目录，返回文件路径。
func (d *Driver) writeSpec(project, yaml string) (string, error) {
	dir := filepath.Join(d.workDir, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir compose project: %w", err)
	}
	path := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		return "", fmt.Errorf("write compose file: %w", err)
	}
	return path, nil
}

// run 执行一次 compose 命令（自动附加 -f 文件与 -p 项目名），返回合并输出。
func (d *Driver) run(ctx context.Context, project, file string, args ...string) (string, error) {
	full := append([]string{"compose", "-f", file, "-p", project}, args...)
	cctx, cancel := context.WithTimeout(ctx, d.commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, d.binary, full...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	if err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			return out, fmt.Errorf("compose %s 超时", args[0])
		}
		return out, fmt.Errorf("compose %s 失败: %v", args[0], err)
	}
	return out, nil
}

// Validate 校验 compose 规格（compose config -q）。返回是否合法与诊断输出。
func (d *Driver) Validate(ctx context.Context, project, yaml string) (bool, string, error) {
	file, err := d.writeSpec(project, yaml)
	if err != nil {
		return false, "", err
	}
	out, err := d.run(ctx, project, file, "config", "-q")
	if err != nil {
		return false, strings.TrimSpace(out), nil // 语法错误即"不合法"，不视为执行失败
	}
	return true, "", nil
}

// Up 部署（compose up -d），返回项目服务清单与原始输出。
func (d *Driver) Up(ctx context.Context, project, yaml string) ([]Service, string, error) {
	file, err := d.writeSpec(project, yaml)
	if err != nil {
		return nil, "", err
	}
	out, err := d.run(ctx, project, file, "up", "-d", "--remove-orphans")
	if err != nil {
		return nil, out, err
	}
	svcs, _ := d.Ps(ctx, project)
	return svcs, out, nil
}

// Stop 停止并移除项目容器（compose down），保留组成文件。
func (d *Driver) Stop(ctx context.Context, project string) (string, error) {
	file := d.composeFile(project)
	if _, err := os.Stat(file); err != nil {
		return "", fmt.Errorf("项目 %s 无组成文件", project)
	}
	return d.run(ctx, project, file, "down")
}

// Restart 重启项目全部服务（compose restart）。
func (d *Driver) Restart(ctx context.Context, project string) ([]Service, string, error) {
	file := d.composeFile(project)
	if _, err := os.Stat(file); err != nil {
		return nil, "", fmt.Errorf("项目 %s 无组成文件", project)
	}
	out, err := d.run(ctx, project, file, "restart")
	if err != nil {
		return nil, out, err
	}
	svcs, _ := d.Ps(ctx, project)
	return svcs, out, nil
}

// Remove 停止并清理项目（compose down -v --remove-orphans），随后删除组成文件。
func (d *Driver) Remove(ctx context.Context, project string) (string, error) {
	file := d.composeFile(project)
	out := ""
	if _, err := os.Stat(file); err == nil {
		var rerr error
		out, rerr = d.run(ctx, project, file, "down", "-v", "--remove-orphans")
		if rerr != nil {
			return out, rerr
		}
	}
	_ = os.RemoveAll(filepath.Join(d.workDir, project))
	return out, nil
}

// Service 是一个 Compose 服务运行态（compose ps 解析结果）。
type Service struct {
	Name          string      `json:"name"`
	Service       string      `json:"service"`
	State         string      `json:"state"`
	Status        string      `json:"status,omitempty"`
	Health        string      `json:"health,omitempty"`
	Image         string      `json:"image,omitempty"`
	ContainerID   string      `json:"container_id,omitempty"`
	Publishers    []Publisher `json:"publishers,omitempty"`
}

// Publisher 是一个发布端口。
type Publisher struct {
	URL           string `json:"url,omitempty"`
	TargetPort    int    `json:"target_port,omitempty"`
	PublishedPort int    `json:"published_port,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
}

// psItem 是 compose ps --format json 的单条 JSON（字段名对齐 compose v2）。
type psItem struct {
	Name       string `json:"Name"`
	Service    string `json:"Service"`
	State      string `json:"State"`
	Status     string `json:"Status"`
	Health     string `json:"Health"`
	Image      string `json:"Image"`
	ID         string `json:"ID"`
	Publishers []struct {
		URL           string `json:"URL"`
		TargetPort    int    `json:"TargetPort"`
		PublishedPort int    `json:"PublishedPort"`
		Protocol      string `json:"Protocol"`
	} `json:"Publishers"`
}

// Ps 列出项目内服务的运行态（compose ps --format json）。
func (d *Driver) Ps(ctx context.Context, project string) ([]Service, error) {
	file := d.composeFile(project)
	if _, err := os.Stat(file); err != nil {
		return nil, nil // 无组成文件即无服务
	}
	// 加 --all 让非运行容器也可见；compose v2 的 --format json 输出为逐行 JSON 对象。
	out, err := d.run(ctx, project, file, "ps", "--all", "--format", "json")
	if err != nil {
		return nil, err
	}
	return parsePs(out), nil
}

// parsePs 解析 compose ps --format json 输出：逐行一个 JSON 对象，兼容整体数组两种形态。
func parsePs(out string) []Service {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return nil
	}
	// 形态一：整体为 JSON 数组。
	if strings.HasPrefix(trimmed, "[") {
		var items []psItem
		if err := json.Unmarshal([]byte(trimmed), &items); err == nil {
			return toServices(items)
		}
	}
	// 形态二：逐行 JSON 对象。
	var items []psItem
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var it psItem
		if err := json.Unmarshal([]byte(line), &it); err == nil {
			items = append(items, it)
		}
	}
	return toServices(items)
}

func toServices(items []psItem) []Service {
	out := make([]Service, 0, len(items))
	for _, it := range items {
		svc := Service{
			Name:        it.Name,
			Service:     it.Service,
			State:       it.State,
			Status:      it.Status,
			Health:      it.Health,
			Image:       it.Image,
			ContainerID: it.ID,
		}
		for _, p := range it.Publishers {
			svc.Publishers = append(svc.Publishers, Publisher{
				URL: p.URL, TargetPort: p.TargetPort,
				PublishedPort: p.PublishedPort, Protocol: p.Protocol,
			})
		}
		out = append(out, svc)
	}
	return out
}
