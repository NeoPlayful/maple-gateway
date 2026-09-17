package apptemplate

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/pkg"
)

// placeholderRe 匹配规格中的 {{key}} 占位符；key 允许字母、数字、下划线。
var placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_]+)\s*\}\}`)

// paramKeyRe 校验参数键的字符集（与占位符保持一致）。
var paramKeyRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// BuiltinDataPathKey 是内置的数据目录占位符键。它在规格中可被直接引用而无需在
// 参数定义里声明——取值由实例化时按 <租户标识>/<模板标识>/<项目标识> 注入。
const BuiltinDataPathKey = "data_path"

// IsBuiltin 报告某个占位符键是否为内置键（无需用户声明、由系统注入取值）。
func IsBuiltin(key string) bool { return key == BuiltinDataPathKey }

// ValidateParams 校验参数定义：键唯一且仅含字母数字下划线，select 必须有选项。
func ValidateParams(ps []Param) error {
	seen := map[string]bool{}
	for i, p := range ps {
		if !paramKeyRe.MatchString(p.Key) {
			return pkg.ErrValidation(fmt.Sprintf("参数 %d：键只能包含字母、数字与下划线", i+1))
		}
		// 保留键不得由用户声明：其取值由实例化按数据路径注入，自行声明会被静默覆盖。
		if IsBuiltin(p.Key) {
			return pkg.ErrValidation(fmt.Sprintf("参数键 %q 是内置参数，无需声明", p.Key))
		}
		if seen[p.Key] {
			return pkg.ErrValidation(fmt.Sprintf("参数键 %q 重复", p.Key))
		}
		seen[p.Key] = true
		if p.Type == ParamSelect && len(p.Options) == 0 {
			return pkg.ErrValidation(fmt.Sprintf("参数 %q：select 类型必须提供可选值", p.Key))
		}
	}
	return nil
}

// Placeholders 返回规格中出现的占位符键（去重、排序）。
func Placeholders(spec string) []string {
	seen := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(spec, -1) {
		seen[m[1]] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Render 用参数取值渲染 Compose 规格：把每个 {{key}} 替换为对应值。
//
// 校验分两层：
//   - 规格中出现但参数定义未声明的占位符 → 拒绝（防止渲染出字面 {{...}} 或漏配）。
//   - 声明为 required 的参数缺值 → 拒绝。
//
// 替换值按类型做基本校验（number/bool/select），值本身原样写入规格文本。
func Render(tmpl *Template, values map[string]string) (string, error) {
	if tmpl == nil {
		return "", pkg.ErrValidation("模板不存在")
	}
	declared := map[string]Param{}
	for _, p := range tmpl.Params {
		declared[p.Key] = p
	}

	// 规格中出现的占位符必须在参数定义内声明；内置键（data_path）由系统注入，豁免。
	for _, key := range Placeholders(tmpl.Spec) {
		if IsBuiltin(key) {
			continue
		}
		if _, ok := declared[key]; !ok {
			return "", pkg.ErrValidation(fmt.Sprintf("规格引用了未声明的参数 %q", key))
		}
	}

	// 内置键取值由调用方（实例化）注入；若未注入则拒绝，避免渲染出空段。
	if strings.Contains(tmpl.Spec, "{{") {
		for _, key := range Placeholders(tmpl.Spec) {
			if IsBuiltin(key) && strings.TrimSpace(values[key]) == "" {
				return "", pkg.ErrValidation(fmt.Sprintf("内置参数 %q 未能注入取值", key))
			}
		}
	}

	// 逐参数校验取值；required 缺值拒绝。
	for _, p := range tmpl.Params {
		v := strings.TrimSpace(values[p.Key])
		if v == "" {
			if p.Required {
				return "", pkg.ErrValidation(fmt.Sprintf("参数 %q 为必填", p.Label))
			}
			v = p.Default
		}
		if v == "" {
			continue
		}
		if err := validateParamValue(p, v); err != nil {
			return "", err
		}
		values[p.Key] = v
	}

	out := placeholderRe.ReplaceAllStringFunc(tmpl.Spec, func(m string) string {
		key := placeholderRe.FindStringSubmatch(m)[1]
		return values[key]
	})
	return out, nil
}

// validateParamValue 按参数类型校验取值。
func validateParamValue(p Param, v string) error {
	switch p.Type {
	case ParamNumber:
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return pkg.ErrValidation(fmt.Sprintf("参数 %q 必须是数字", p.Label))
		}
	case ParamBool:
		if _, err := strconv.ParseBool(v); err != nil {
			return pkg.ErrValidation(fmt.Sprintf("参数 %q 必须是布尔值", p.Label))
		}
	case ParamSelect:
		for _, o := range p.Options {
			if o == v {
				return nil
			}
		}
		return pkg.ErrValidation(fmt.Sprintf("参数 %q 的取值不在可选范围内", p.Label))
	}
	return nil
}
