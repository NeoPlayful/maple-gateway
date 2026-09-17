// Package project 管理租户下的部署项目。
//
// 一个租户 + 一个模板可以有多个项目；项目名即项目标识（必须英文），作为数据目录的
// 第三段区分同一租户同一模板下的多份独立部署：
//
//	<节点 data_dir>/<租户标识>/<模板标识>/<项目标识>
package project

import (
	"time"

	"github.com/google/uuid"
)

// Status 是项目状态。
type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

// Project 是租户下的一个部署项目。
type Project struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	TemplateID  uuid.UUID `json:"template_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Status      Status    `json:"status"`
	NodeID      string    `json:"node_id,omitempty"`
	// ApplicationID 是渲染模板后生成的 CM Application ID（实例化幂等键）。
	ApplicationID string    `json:"application_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// New 携带创建输入。name 为项目标识（英文），必填且受路径段字符集约束。
type New struct {
	TenantID    uuid.UUID `json:"tenant_id" validate:"required"`
	TemplateID  uuid.UUID `json:"template_id"`
	Name        string    `json:"name" validate:"required,min=1,max=64"`
	Description string    `json:"description" validate:"max=500"`
}

// Update 携带可修改字段。
//
// Description 为三态语义：非空=设置；description_clear=true=显式清空；二者皆否=保持不变。
// name 不可修改（它是数据目录第三段与模板变量的组成部分，改名将指向另一份数据）。
type Update struct {
	Description      *string `json:"description"`
	ClearDescription bool    `json:"description_clear,omitempty"`
	Status           *Status `json:"status"`
	NodeID           *string `json:"node_id"`
}
