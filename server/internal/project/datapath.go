package project

import (
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
)

// DataPath 是一份项目数据的目录归属：三段标识 + 由它们拼出的相对路径。
//
// 相对路径形如 <租户标识>/<模板标识>/<项目标识>，相对节点数据目录：节点侧用自身的
// data_dir 拼出宿主绝对路径并做越界校验，控制面因此无需、也无法指定任意的宿主目录。
type DataPath struct {
	Tenant   string `json:"tenant"`
	Template string `json:"template"`
	Project  string `json:"project"`
	// Rel 是相对节点数据目录的路径（三段用 / 连接）。
	Rel string `json:"rel"`
}

// BuildDataPath 由三段标识拼出项目数据路径，并逐段做路径段校验。
// 任一段非法（含分隔符、上跳段、非安全字符）即拒绝——三道防线中的第一道。
func BuildDataPath(tenantSlug, templateSlug, projectName string) (DataPath, error) {
	tenant := strings.TrimSpace(tenantSlug)
	template := strings.TrimSpace(templateSlug)
	project := strings.TrimSpace(projectName)
	if err := pkg.ValidatePathSegment("租户标识", tenant); err != nil {
		return DataPath{}, err
	}
	if err := pkg.ValidatePathSegment("模板标识", template); err != nil {
		return DataPath{}, err
	}
	if err := pkg.ValidatePathSegment("项目标识", project); err != nil {
		return DataPath{}, err
	}
	return DataPath{
		Tenant:   tenant,
		Template: template,
		Project:  project,
		Rel:      tenant + "/" + template + "/" + project,
	}, nil
}

// Sub 返回该数据路径下的一段子目录（如 "db"、"conf"），仍相对数据根。
// sub 可为多段（如 "postgres/data"），逐段校验。
func (d DataPath) Sub(sub string) (string, error) {
	sub = strings.TrimSpace(strings.ReplaceAll(sub, "\\", "/"))
	if sub == "" {
		return d.Rel, nil
	}
	if err := pkg.ValidateRelSubpath("子路径", sub); err != nil {
		return "", err
	}
	return d.Rel + "/" + sub, nil
}
