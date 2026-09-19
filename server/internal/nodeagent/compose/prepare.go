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
//   - 当 networkName 非空时，把各服务接入该外部网络并追加顶层 external 网络定义，
//     不再依赖 <compose-project>_default 自动网络——网段由 CM 统一分配（见 IPAM），
//     绕开 Docker 默认地址池。
//
// 加工失败即拒绝下发，避免落盘一份半成品规格。
func Prepare(spec, dataRoot, managedLabel, appID, networkName string) (string, error) {
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
	if networkName != "" {
		injectExternalNetwork(doc, services, networkName)
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("序列化加工后的规格: %w", err)
	}
	return string(out), nil
}

// projectNetworkKey 是规格里外部网络的键名（服务经它接入）。
const projectNetworkKey = "project_network"

// injectExternalNetwork 把各服务接入外部网络，并声明顶层 external 网络。
// 既有 networks 写法（list）予以保留并去重；顶层 networks 里同名定义以平台注入为准。
func injectExternalNetwork(doc, services map[string]any, networkName string) {
	for _, svc := range services {
		m, ok := svc.(map[string]any)
		if !ok {
			continue
		}
		m["networks"] = mergeNetworks(m["networks"])
	}
	nets, _ := doc["networks"].(map[string]any)
	if nets == nil {
		nets = map[string]any{}
	}
	nets[projectNetworkKey] = map[string]any{"external": true, "name": networkName}
	doc["networks"] = nets
}

// mergeNetworks 在既有 networks 上补齐项目网络键，尽量保留原有写法与配置：
// map 写法补键（保留各网络的别名等配置），list 写法追加去重，无则用单元素 list。
func mergeNetworks(existing any) any {
	if m, ok := existing.(map[string]any); ok {
		if _, has := m[projectNetworkKey]; !has {
			m[projectNetworkKey] = map[string]any{}
		}
		return m
	}
	out := []string{projectNetworkKey}
	switch v := existing.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s != projectNetworkKey {
				out = append(out, s)
			}
		}
	case []string:
		for _, s := range v {
			if s != projectNetworkKey {
				out = append(out, s)
			}
		}
	}
	return out
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
