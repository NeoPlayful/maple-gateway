package project

import (
	"context"
	"encoding/json"
	"fmt"
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

// Instantiator 把「租户 + 模板 + 项目 + 参数」渲染为一份 Compose 规格并落成 Application。
type Instantiator struct {
	projects  *Repository
	tenants   SlugResolver
	templates TemplateSource
	apps      AppSaver
}

// NewInstantiator 构造。
func NewInstantiator(projects *Repository, tenants SlugResolver, templates TemplateSource, apps AppSaver) *Instantiator {
	return &Instantiator{projects: projects, tenants: tenants, templates: templates, apps: apps}
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
	rendered, err := apptemplate.Render(tmpl, values)
	if err != nil {
		return nil, err
	}

	appID := p.ApplicationID
	body := map[string]any{
		"id":          appID,
		"tenant_id":   p.TenantID.String(),
		"name":        p.Name,
		"description": fmt.Sprintf("%s / %s", tmpl.Name, p.Name),
		"version":     "v1",
		"spec":        rendered,
		"node_id":     in.NodeID,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, pkg.ErrSystem("序列化应用请求失败")
	}
	out, err := i.apps.SaveApplication(ctx, json.RawMessage(raw))
	if err != nil {
		return nil, pkg.ErrSystem("创建应用失败: " + err.Error())
	}
	// 回读 CM 生成的 Application ID（首次创建时由 CM 分配）。
	if id := extractID(out); id != "" && id != appID {
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
