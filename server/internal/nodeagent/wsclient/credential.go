// Package wsclient 实现 Node Agent 反向 WebSocket 客户端：
// Agent 主动连接 Container Manager，经一条持久连接完成握手、心跳、
// 状态上报与任务接收执行。节点无需对外暴露管理端口。
package wsclient

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Credential 是 Agent 本地持久化的节点身份。
type Credential struct {
	NodeID     string `json:"node_id"`
	NodeSecret string `json:"node_secret"`
	ServerURL  string `json:"server_url,omitempty"`
}

// LoadCredential 从文件读取凭证；不存在时返回 (nil, nil) 表示未注册。
func LoadCredential(path string) (*Credential, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read credential: %w", err)
	}
	var c Credential
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse credential: %w", err)
	}
	if c.NodeID == "" || c.NodeSecret == "" {
		return nil, fmt.Errorf("credential incomplete")
	}
	return &c, nil
}

// SaveCredential 原子写出凭证并把权限收紧到 0600（尽力而为）。
func SaveCredential(path string, c *Credential) error {
	if path == "" {
		return fmt.Errorf("credential path empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create credential dir: %w", err)
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write credential: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace credential: %w", err)
	}
	_ = os.Chmod(path, 0o600)
	return nil
}
