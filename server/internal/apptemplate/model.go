// Package template 管理应用模板：容器创建的规格来源。
//
// 一份模板 = 带 {{参数键}} 占位符的 Compose 规格 + 参数定义。用户选模板、填参数即可
// 创建一个项目，渲染出的规格转为 Application 下发。slug 是模板标识（英文），全局唯一，
// 并作为数据目录第二段：<节点 data_dir>/<租户标识>/<模板标识>/<项目标识>。
package apptemplate

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Status 是模板状态。
type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

// ParamType 是模板参数的类型。
type ParamType string

const (
	ParamString ParamType = "string"
	ParamNumber ParamType = "number"
	ParamBool   ParamType = "bool"
	ParamSelect ParamType = "select"
)

// Param 是一个模板参数定义。渲染时用取值替换规格中的 {{key}}。
type Param struct {
	Key      string    `json:"key"`
	Label    string    `json:"label"`
	Type     ParamType `json:"type"`
	Required bool      `json:"required,omitempty"`
	Default  string    `json:"default,omitempty"`
	// Options 是 type=select 的可选值集合。
	Options []string `json:"options,omitempty"`
	Hint    string   `json:"hint,omitempty"`
}

// Template 是一个应用模板。
type Template struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description,omitempty"`
	Spec        string    `json:"spec"`
	Params      []Param   `json:"params,omitempty"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// New 携带创建输入。
type New struct {
	Name        string  `json:"name" validate:"required,min=1,max=100"`
	Slug        string  `json:"slug" validate:"required,min=1,max=64"`
	Description string  `json:"description" validate:"max=500"`
	Spec        string  `json:"spec"`
	Params      []Param `json:"params"`
}

// Update 携带可修改字段。slug 不可修改（它是数据目录第二段）。
//
// Description 为三态语义：非空=设置；description_clear=true=显式清空；二者皆否=保持不变。
type Update struct {
	Name             *string  `json:"name"`
	Description      *string  `json:"description"`
	ClearDescription bool     `json:"description_clear,omitempty"`
	Spec             *string  `json:"spec"`
	Params           *[]Param `json:"params"`
	Status           *Status  `json:"status"`
}

// MarshalParams 把参数定义编码为 JSONB 文本（nil 保持为空）。
func MarshalParams(ps []Param) json.RawMessage {
	if len(ps) == 0 {
		return nil
	}
	raw, err := json.Marshal(ps)
	if err != nil {
		return nil
	}
	return raw
}
