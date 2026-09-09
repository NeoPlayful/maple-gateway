package pkg

import (
	"context"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis 包装 go-redis 客户端。
type Redis struct {
	Client *redis.Client
	Prefix string // key 命名空间前缀（如 "maple"）；空等价不设
}

// NewRedis 建立 Redis 连接并探测。enabled=false 时返回 nil（组件可选）。
// prefix 为业务 key 的统一前缀；空则回退 "maple"（历史硬编码默认，保持默认行为不变）。
func NewRedis(ctx context.Context, url, prefix string) (*Redis, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opt)
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		client.Close()
		return nil, err
	}
	if strings.TrimSpace(prefix) == "" {
		prefix = "maple"
	}
	return &Redis{Client: client, Prefix: prefix}, nil
}

// Key 返回带命名空间前缀的完整 Redis 键：prefix:name。prefix 为空时直接用 name。
func (r *Redis) Key(name string) string {
	if r == nil || strings.TrimSpace(r.Prefix) == "" {
		return name
	}
	return r.Prefix + ":" + name
}

// Close 关闭连接。
func (r *Redis) Close() {
	if r != nil && r.Client != nil {
		_ = r.Client.Close()
	}
}
