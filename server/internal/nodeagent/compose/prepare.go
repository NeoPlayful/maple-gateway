package compose

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// DataDirPlaceholder 是数据根占位符：渲染期保留字面量，由节点在此替换为本机数据根，
// 使模板无需写死宿主路径，跨节点可移植。
const DataDirPlaceholder = "{{data_dir}}"

// Prepare 在下发 Compose 前就地加工规格：
//
//   - 把 {{data_dir}} 替换为本节点数据根（dataRoot）；引用而节点未配置数据根时拒绝。
//   - 为每个服务注入受管标签（managedLabel=true 与 maple.application_id=appID），
//     合并既有 labels（兼容 list 与 map 两种写法），使 Compose 容器进入受管容器列表。
//
// 加工失败即拒绝下发，避免落盘一份半成品规格。
func Prepare(spec, dataRoot, managedLabel, appID string) (string, error) {
	if strings.Contains(spec, DataDirPlaceholder) {
		if dataRoot == "" {
			return "", fmt.Errorf("规格引用了数据根但本节点未配置 data_dir")
		}
		spec = strings.ReplaceAll(spec, DataDirPlaceholder, dataRoot)
	}
	if strings.TrimSpace(spec) == "" {
		return spec, nil
	}

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(spec), &doc); err != nil {
		// 规格非法时原样交 compose 校验，由它给出权威报错。
		return spec, nil
	}
	services, ok := doc["services"].(map[string]any)
	if !ok {
		return spec, nil
	}
	for _, svc := range services {
		m, ok := svc.(map[string]any)
		if !ok {
			continue
		}
		m["labels"] = mergeLabels(m["labels"], managedLabel, appID)
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("序列化加工后的规格: %w", err)
	}
	return string(out), nil
}

// mergeLabels 在既有 labels 上补齐受管标签与所属应用，兼容 list 与 map 两种写法。
// 同名标签以平台注入值为准（受管标记须恒为 true，否则容器会逸出受管视野）。
func mergeLabels(existing any, managedLabel, appID string) map[string]string {
	out := map[string]string{}
	switch v := existing.(type) {
	case map[string]any:
		for k, val := range v {
			out[k] = fmt.Sprintf("%v", val)
		}
	case map[string]string:
		for k, val := range v {
			out[k] = val
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				if k, val, found := strings.Cut(s, "="); found {
					out[strings.TrimSpace(k)] = strings.TrimSpace(val)
				}
			}
		}
	}
	if managedLabel != "" {
		out[managedLabel] = "true"
	}
	if appID != "" {
		out["maple.application_id"] = appID
	}
	return out
}
