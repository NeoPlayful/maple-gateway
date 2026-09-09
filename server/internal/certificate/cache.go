package certificate

import (
	"sync"

	"github.com/NeoPlayful/maple-gateway/server/internal/router"
)

// Cache 是 hostname → *tls.Certificate 的内存缓存（Direct TLS 握手专用）。
// 热路径读多写少：读取用 RWMutex 保护，写入原子替换；严禁在 GetCertificate 里查库。
type Cache struct {
	mu   sync.RWMutex
	bySN map[string]*Loaded // key: normalize 后小写 hostname
}

// NewCache 构造空缓存。
func NewCache() *Cache {
	return &Cache{bySN: make(map[string]*Loaded)}
}

// Get 按 SNI（ServerName）取证书。未命中返回 nil。
func (c *Cache) Get(serverName string) *Loaded {
	key := normalizeSN(serverName)
	if key == "" {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.bySN[key]
}

// Set 写入/替换某 hostname 的证书（原子替换，热加载入口）。
func (c *Cache) Set(hostname string, cert *Loaded) {
	key := normalizeSN(hostname)
	if key == "" || cert == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bySN[key] = cert
}

// Delete 移除某 hostname 证书。
func (c *Cache) Delete(hostname string) {
	key := normalizeSN(hostname)
	if key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.bySN, key)
}

// ReplaceAll 原子整体替换缓存内容（全量重载用，避免装载过程读不一致）。
func (c *Cache) ReplaceAll(next map[string]*Loaded) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bySN = next
}

// Len 证书条目数量（指标/统计用）。
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.bySN)
}

// Snapshot 返回当前全部条目（装载/对账用）。
func (c *Cache) Snapshot() map[string]*Loaded {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]*Loaded, len(c.bySN))
	for k, v := range c.bySN {
		out[k] = v
	}
	return out
}

// normalizeSN 规范化 SNI/ServerName：去端口、小写；非法/空返回 ""。
func normalizeSN(serverName string) string {
	norm, err := router.NormalizeHost(serverName)
	if err != nil {
		return ""
	}
	return norm
}
