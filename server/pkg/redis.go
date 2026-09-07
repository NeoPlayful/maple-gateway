package pkg

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis 包装 go-redis 客户端。
type Redis struct {
	Client *redis.Client
}

// NewRedis 建立 Redis 连接并探测。enabled=false 时返回 nil（组件可选）。
func NewRedis(ctx context.Context, url string) (*Redis, error) {
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
	return &Redis{Client: client}, nil
}

// Close 关闭连接。
func (r *Redis) Close() {
	if r != nil && r.Client != nil {
		_ = r.Client.Close()
	}
}
