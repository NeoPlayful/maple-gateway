package project

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/internal/apptemplate"
	"github.com/NeoPlayful/maple-gateway/server/pkg"
	"github.com/google/uuid"
)

// SlugResolver 按租户 ID 取其标识（Gateway tenants.slug），作为数据目录第一段。
type SlugResolver interface {
	TenantSlug(ctx context.Context, tenantID uuid.UUID) (string, error)
}

// TemplateSource 读取模板（slug 为数据目录第二段；spec/params 供渲染）。
type TemplateSource interface {
	Get(ctx context.Context, id uuid.UUID) (*apptemplate.Template, error)
}

// AppSaver 把渲染出的 Compose 规格写成 CM Application，返回其 ID。
// body 为 CM /api/mgmt/applications 的请求体（原样透传）。
type AppSaver interface {
	SaveApplication(ctx context.Context, body json.RawMessage) (json.RawMessage, error)
}

// PortAllocator 从控制面集中端口池分配/归还本机端口（模板未钉死端口时用）。
type PortAllocator interface {
	AllocatePort(ctx context.Context, resourceID, kind, nodeID string) (int, error)
	ReleasePort(ctx context.Context, resourceID string) error
}

// ServiceBinder 返回项目已绑定的服务 ID（一项目一服务）。供实例化把服务归属
// 写进 CM 应用，使观测器据此把该应用的容器归入对应服务的路由池。
type ServiceBinder interface {
	ServiceForProject(ctx context.Context, projectID uuid.UUID) (uuid.UUID, bool, error)
}

// Instantiator 把「租户 + 模板 + 项目 + 参数」渲染为一份 Compose 规格并落成 Application。
type Instantiator struct {
	projects  *Repository
	tenants   SlugResolver
	templates TemplateSource
	apps      AppSaver
	// ports 可空；未接入 CM 时为 nil，模板引用内置 {{port}} 且未声明为参数时拒绝实例化。
	ports PortAllocator
	// svc 可空；注入后实例化会把项目服务 ID 写进 CM 应用。
	svc ServiceBinder
}

// NewInstantiator 构造。
func NewInstantiator(projects *Repository, tenants SlugResolver, templates TemplateSource, apps AppSaver) *Instantiator {
	return &Instantiator{projects: projects, tenants: tenants, templates: templates, apps: apps}
}

// WithPortAllocator 注入端口池（模板未钉死宿主端口时由系统分配）。
func (i *Instantiator) WithPortAllocator(p PortAllocator) *Instantiator {
	i.ports = p
	return i
}

// WithServiceBinder 注入项目→服务解析器（实例化把服务归属写进 CM 应用）。
func (i *Instantiator) WithServiceBinder(b ServiceBinder) *Instantiator {
	i.svc = b
	return i
}

// InstantiateInput 是一次实例化请求。
type InstantiateInput struct {
	// Values 是模板参数取值；键须与模板参数定义一致。
	Values map[string]string `json:"values"`
	// NodeID 是部署落点（可空，由 CM 侧调度兜底）。
	NodeID string `json:"node_id"`
}

// Instantiate 渲染项目模板并生成 Application，成功后在项目上记录 application_id。
//
// 数据目录三段（<租户标识>/<模板标识>/<项目标识>）在此确定并作为 Compose 规格的
// 渲染上下文一并提供（模板可经 %DATA_DIR% 占位引用，落成绑定挂载的相对路径）。
// 重复调用是幂等的：项目已有 application_id 时按同名覆盖同一 Application。
func (i *Instantiator) Instantiate(ctx context.Context, projectID uuid.UUID, in InstantiateInput) (*Project, error) {
	p, err := i.projects.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	tmpl, err := i.templates.Get(ctx, p.TemplateID)
	if err != nil {
		return nil, err
	}
	slug, err := i.tenants.TenantSlug(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}

	// 三段路径是模板资源的落点，先校验再渲染：任一段非法即拒绝，避免越界路径进入规格。
	dataPath, err := BuildDataPath(slug, tmpl.Slug, p.Name)
	if err != nil {
		return nil, err
	}

	values := in.Values
	if values == nil {
		values = map[string]string{}
	}
	// 数据目录相对路径是内置参数，规格可直接用 {{data_path}} 引用而无需声明；
	// 此处无条件注入，渲染器会校验规格确实引用了它时取值非空。
	values[apptemplate.BuiltinDataPathKey] = dataPath.Rel
	if err := i.injectPort(ctx, projectID, tmpl, values); err != nil {
		return nil, err
	}
	rendered, err := apptemplate.Render(tmpl, values)
	if err != nil {
		return nil, err
	}

	// Application ID 统一取 Gateway 项目 ID：这样 Compose 项目名与项目网络名
	// 都落在 maple-<shortID(projectID)> 上，二者逐字相同，避免两套标识漂移。
	appID := p.ID.String()
	body := map[string]any{
		"id":          appID,
		"tenant_id":   p.TenantID.String(),
		"name":        p.Name,
		"description": fmt.Sprintf("%s / %s", tmpl.Name, p.Name),
		"version":     "v1",
		"spec":        rendered,
		"node_id":     in.NodeID,
	}
	// 把项目服务归属写进 CM 应用：观测器据应用的 service_id 将容器归入正确路由池。
	if i.svc != nil {
		if sid, ok, err := i.svc.ServiceForProject(ctx, projectID); err != nil {
			return nil, err
		} else if ok {
			body["service_id"] = sid.String()
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, pkg.ErrSystem("序列化应用请求失败")
	}
	out, err := i.apps.SaveApplication(ctx, json.RawMessage(raw))
	if err != nil {
		return nil, pkg.ErrSystem("创建应用失败: " + err.Error())
	}
	// Application ID 即项目 ID；若与库中记录不一致（如历史遗留的 CM 生成 ID），回写对齐。
	if p.ApplicationID != appID {
		id := extractID(out)
		if id == "" {
			id = appID
		}
		if err := i.projects.SetApplicationID(ctx, projectID, id); err != nil {
			return nil, err
		}
	}
	out2, err := i.projects.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return out2, nil
}

// injectPort 处理模板对宿主端口的引用：规格用了 {{port}} 且未将它声明为参数时，
// 从控制面端口池分配一个空闲端口注入渲染（占位落在项目 ID 上），避免多项目抢占同一端口。
// 已声明为参数的 port 视作用户钉死的固定端口，不参与分配。
func (i *Instantiator) injectPort(ctx context.Context, projectID uuid.UUID, tmpl *apptemplate.Template, values map[string]string) error {
	if !referencesPort(tmpl) {
		return nil
	}
	if i.ports == nil {
		return pkg.ErrSystem("模板引用了宿主端口但未接入端口分配")
	}
	port, err := i.ports.AllocatePort(ctx, projectID.String(), "project", "")
	if err != nil {
		return pkg.ErrSystem("分配端口失败: " + err.Error())
	}
	values[apptemplate.BuiltinPortKey] = strconv.Itoa(port)
	return nil
}

// referencesPort 报告模板规格是否引用了内置 port 占位符，且未将其声明为参数。
func referencesPort(tmpl *apptemplate.Template) bool {
	if !strings.Contains(tmpl.Spec, "{{") {
		return false
	}
	declared := map[string]bool{}
	for _, p := range tmpl.Params {
		declared[p.Key] = true
	}
	if declared[apptemplate.BuiltinPortKey] {
		return false
	}
	for _, key := range apptemplate.Placeholders(tmpl.Spec) {
		if key == apptemplate.BuiltinPortKey {
			return true
		}
	}
	return false
}

// ReleasePort 归还项目占用的全部端口（项目删除时调用）。
func (i *Instantiator) ReleasePort(ctx context.Context, projectID uuid.UUID) error {
	if i.ports == nil {
		return nil
	}
	return i.ports.ReleasePort(ctx, projectID.String())
}

// extractID 从 CM 的响应体里取出应用 id（保持对包装结构不敏感的宽松解析）。
func extractID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	if v, ok := m["id"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
